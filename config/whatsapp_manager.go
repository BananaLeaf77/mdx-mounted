package config

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

type WAStatus string

const (
	WAStatusDisconnected WAStatus = "disconnected"
	WAStatusWaitingQR    WAStatus = "waiting_qr"
	WAStatusConnected    WAStatus = "connected"
	WAStatusConnecting   WAStatus = "connecting"

	waQRTimeout            = 3 * time.Minute
	waReconnectDelay       = 5 * time.Second
	waMaxReconnectAttempts = 3
	waDBTimeout            = 10 * time.Second
)

type WAManager struct {
	mu        sync.RWMutex
	connectMu sync.Mutex

	client          *whatsmeow.Client
	dbAddress       string
	status          WAStatus
	qrCode          string
	qrCancel        context.CancelFunc // Track QR context for cleanup
	reconnectCancel context.CancelFunc
	reconnectCount  int
	eventHandlerID  uint32
	connectionID    uint64 // Monotonic ID to track connection attempts
}

func NewWAManager(dbAddress string) *WAManager {
	return &WAManager{
		dbAddress: dbAddress,
		status:    WAStatusDisconnected,
	}
}

func (m *WAManager) getNextConnectionID() uint64 {
	return atomic.AddUint64(&m.connectionID, 1)
}

func (m *WAManager) Connect() error {
	m.connectMu.Lock()
	defer m.connectMu.Unlock()

	connID := m.getNextConnectionID()
	log.Printf("📱 WhatsApp Connect() started [connID: %d]", connID)

	// Cancel any pending reconnect and QR channels
	m.cancelOngoingOperations()

	// Create new database context - separate from QR context
	dbCtx, dbCancel := context.WithTimeout(context.Background(), waDBTimeout)
	defer dbCancel()

	dbLog := waLog.Stdout("Database", "WARN", true)

	container, err := sqlstore.New(dbCtx, "pgx", m.dbAddress, dbLog)
	if err != nil {
		return fmt.Errorf("whatsapp sqlstore: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(dbCtx)
	if err != nil {
		return fmt.Errorf("whatsapp get device: %w", err)
	}

	m.mu.Lock()
	// Clean up existing client properly
	m.cleanupClientLocked()

	m.client = whatsmeow.NewClient(deviceStore, nil)
	m.client.AutomaticMessageRerequestFromPhone = true
	m.client.AutoTrustIdentity = true
	m.client.UseRetryMessageStore = true
	m.client.Store.Platform = "macos"

	// Add event handler with connection ID tracking
	m.eventHandlerID = m.client.AddEventHandler(func(evt interface{}) {
		m.handleEvent(evt, connID)
	})

	client := m.client
	hasSession := client.Store.ID != nil
	m.status = WAStatusConnecting
	m.qrCode = ""
	m.mu.Unlock()

	if !hasSession {
		// No existing session - need QR pairing
		m.setStatus(WAStatusWaitingQR)

		// CRITICAL: Use cancellable context for QR channel
		qrCtx, qrCancel := context.WithTimeout(context.Background(), waQRTimeout)

		m.mu.Lock()
		m.qrCancel = qrCancel // Store for cleanup on Disconnect/Logout
		m.mu.Unlock()

		qrChan, err := client.GetQRChannel(qrCtx)
		if err != nil {
			qrCancel()
			m.setStatus(WAStatusDisconnected)
			return fmt.Errorf("failed to get QR channel: %w", err)
		}

		if err := client.Connect(); err != nil {
			qrCancel()
			m.setStatus(WAStatusDisconnected)
			return fmt.Errorf("whatsapp connect: %w", err)
		}

		// Handle QR events in goroutine with connection ID tracking
		go m.handleQRChannel(qrChan, qrCancel, connID)
	} else {
		// Existing session - try to connect directly
		if err := client.Connect(); err != nil {
			m.setStatus(WAStatusDisconnected)
			return fmt.Errorf("whatsapp reconnect: %w", err)
		}
	}

	return nil
}

// cancelOngoingOperations stops any pending QR channels and reconnect attempts
func (m *WAManager) cancelOngoingOperations() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.reconnectCancel != nil {
		m.reconnectCancel()
		m.reconnectCancel = nil
	}

	if m.qrCancel != nil {
		m.qrCancel()
		m.qrCancel = nil
	}
}

// cleanupClientLocked properly cleans up the existing client
// Must be called with m.mu held
func (m *WAManager) cleanupClientLocked() {
	if m.client == nil {
		return
	}

	// Remove event handler first to prevent events during cleanup
	if m.eventHandlerID != 0 {
		m.client.RemoveEventHandler(m.eventHandlerID)
		m.eventHandlerID = 0
	}

	// Disconnect if connected
	if m.client.IsConnected() {
		m.client.Disconnect()
	}

	m.client = nil
}

func (m *WAManager) handleQRChannel(qrChan <-chan whatsmeow.QRChannelItem, cancel context.CancelFunc, connID uint64) {
	defer cancel()

	for evt := range qrChan {
		// Check if this is still the current connection
		currentConnID := atomic.LoadUint64(&m.connectionID)
		if currentConnID != connID {
			log.Printf("🔄 QR channel [connID: %d] superseded by newer connection [connID: %d], exiting", connID, currentConnID)
			return
		}

		switch evt.Event {
		case "code":
			m.mu.Lock()
			// Only update if still waiting for QR
			if m.status == WAStatusWaitingQR {
				m.qrCode = evt.Code
			}
			m.mu.Unlock()
			log.Printf("📱 WhatsApp QR code generated [connID: %d]", connID)

		case "success":
			m.mu.Lock()
			m.qrCode = ""
			m.qrCancel = nil // Clear the cancel func on success
			// Status will be set by Connected event, but we can pre-set it
			if m.status == WAStatusWaitingQR {
				m.status = WAStatusConnected
			}
			m.mu.Unlock()
			log.Printf("✅ WhatsApp QR scanned — pairing successful [connID: %d]", connID)
			return

		case "timeout":
			log.Printf("⏰ WhatsApp QR code timed out [connID: %d]", connID)
			m.setStatus(WAStatusDisconnected)
			return

		case "err-client-outdated":
			log.Printf("❌ WhatsApp client outdated [connID: %d]", connID)
			m.setStatus(WAStatusDisconnected)
			return

		case "err-unexpected-state":
			log.Printf("❌ WhatsApp unexpected state error [connID: %d]", connID)
			m.setStatus(WAStatusDisconnected)
			return

		default:
			log.Printf("⚠️ WhatsApp QR channel event [connID: %d]: %s", connID, evt.Event)
			if evt.Error != nil {
				log.Printf("⚠️ WhatsApp QR channel error [connID: %d]: %v", connID, evt.Error)
			}
		}
	}
}

func (m *WAManager) EnsureConnected() error {
	st := m.GetStatus()
	if st == WAStatusConnected {
		// Additional health check
		m.mu.RLock()
		client := m.client
		m.mu.RUnlock()

		if client != nil && client.IsConnected() && client.IsLoggedIn() {
			return nil
		}
		// Status says connected but client is not healthy, force reconnect
		log.Println("⚠️ Status connected but client unhealthy, forcing reconnect")
	}
	if st == WAStatusWaitingQR || st == WAStatusConnecting {
		return errors.New("connection already in progress")
	}
	return m.Connect()
}

func (m *WAManager) Disconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Cancel any ongoing operations
	if m.reconnectCancel != nil {
		m.reconnectCancel()
		m.reconnectCancel = nil
	}

	if m.qrCancel != nil {
		m.qrCancel()
		m.qrCancel = nil
	}

	// Cleanup client and CLEAR SESSION
	if m.client != nil {
		if m.eventHandlerID != 0 {
			m.client.RemoveEventHandler(m.eventHandlerID)
			m.eventHandlerID = 0
		}

		// Disconnect first
		if m.client.IsConnected() {
			m.client.Disconnect()
		}

		// IMPORTANT: Clear the device ID to force new QR on next connect
		if m.client.Store != nil {
			m.client.Store.ID = nil
		}

		m.client = nil
	}

	m.status = WAStatusDisconnected
	m.qrCode = ""
	m.reconnectCount = 0
	log.Println("🔌 WhatsApp disconnected and session cleared")
}

// cancelOngoingOperationsLocked must be called with m.mu held
func (m *WAManager) cancelOngoingOperationsLocked() {
	if m.reconnectCancel != nil {
		m.reconnectCancel()
		m.reconnectCancel = nil
	}

	if m.qrCancel != nil {
		m.qrCancel() // This stops the QR channel goroutine immediately
		m.qrCancel = nil
	}
}

func (m *WAManager) Logout(ctx context.Context) error {
	// Cancel ongoing operations first (without holding lock to avoid deadlock)
	m.cancelOngoingOperations()

	m.mu.Lock()
	client := m.client
	m.mu.Unlock()

	if client == nil {
		return errors.New("whatsapp client not initialised")
	}

	// Perform logout - this clears session from database
	err := client.Logout(ctx)

	// Cleanup
	m.mu.Lock()
	if m.eventHandlerID != 0 {
		client.RemoveEventHandler(m.eventHandlerID)
	}
	m.eventHandlerID = 0
	m.status = WAStatusDisconnected
	m.qrCode = ""
	m.reconnectCount = 0
	m.client = nil
	m.mu.Unlock()

	if err != nil {
		log.Printf("⚠️ WhatsApp logout error: %v", err)
		return err
	}

	log.Println("🔓 WhatsApp session cleared")
	return nil
}

func (m *WAManager) SendMessage(phone, text string) error {
	m.mu.RLock()
	client := m.client
	status := m.status
	m.mu.RUnlock()

	if client == nil {
		return errors.New("whatsapp client not initialised")
	}

	// Heal stale state before giving up: status may lag behind reality.
	if status == WAStatusConnected && !client.IsConnected() {
		log.Printf("⚠️ WhatsApp stale state detected in SendMessage — reconnecting before send to %s", phone)
		m.setStatus(WAStatusDisconnected)

		if err := m.Connect(); err != nil {
			return fmt.Errorf("whatsapp reconnect failed: %w", err)
		}

		// Wait up to 5 s for connection to establish.
		for i := 0; i < 25; i++ {
			time.Sleep(200 * time.Millisecond)
			m.mu.RLock()
			client = m.client
			status = m.status
			m.mu.RUnlock()
			if status == WAStatusConnected && client != nil && client.IsConnected() {
				break
			}
		}
	}

	if status != WAStatusConnected {
		return errors.New("whatsapp not connected")
	}

	// Re-read under lock after potential reconnect.
	m.mu.RLock()
	client = m.client
	m.mu.RUnlock()

	if client == nil || !client.IsConnected() {
		return errors.New("whatsapp client disconnected")
	}

	jid := types.NewJID(phone, types.DefaultUserServer)

	sendCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Check registration and get canonical JID.
	res, err := client.IsOnWhatsApp(sendCtx, []string{phone})
	if err != nil {
		log.Printf("⚠️ IsOnWhatsApp check failed for %s: %v — attempting send anyway", phone, err)
	} else if len(res) > 0 && !res[0].IsIn {
		return fmt.Errorf("phone %s is not registered on WhatsApp", phone)
	} else if len(res) > 0 && res[0].JID.User != "" {
		jid = res[0].JID
	}

	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(text),
		},
	}

	_, sendErr := client.SendMessage(sendCtx, jid, msg)
	if sendErr != nil {
		// One retry after device refresh.
		if _, refreshErr := client.GetUserDevicesContext(sendCtx, []types.JID{jid}); refreshErr == nil {
			_, sendErr = client.SendMessage(sendCtx, jid, msg)
		}
	}

	return sendErr
}

func (m *WAManager) GetStatus() WAStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *WAManager) GetQRCode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Only return QR code if we're actually waiting for pairing
	if m.status == WAStatusWaitingQR && m.qrCode != "" {
		return m.qrCode
	}
	return ""
}

func (m *WAManager) GetJID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.client == nil || m.client.Store.ID == nil {
		return ""
	}
	return m.client.Store.ID.String()
}

func (m *WAManager) IsLoggedIn() bool {
	m.mu.RLock()
	client := m.client
	status := m.status
	m.mu.RUnlock()

	if client == nil || status != WAStatusConnected {
		return false
	}

	// Detect stale state: status says connected but TCP is dead.
	if !client.IsConnected() || !client.IsLoggedIn() {
		log.Println("⚠️ WhatsApp stale state detected in IsLoggedIn — triggering reconnect")
		// Fix the status so callers get accurate info immediately.
		m.setStatus(WAStatusDisconnected)
		// Schedule reconnect (non-blocking, same as the Disconnected event handler).
		go func() {
			time.Sleep(2 * time.Second)
			if err := m.Connect(); err != nil {
				log.Printf("⚠️ Background reconnect after stale detection failed: %v", err)
			}
		}()
		return false
	}

	return true
}

func (m *WAManager) RawClient() *whatsmeow.Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.client
}

// handleEvent processes WhatsApp events with connection tracking
func (m *WAManager) handleEvent(evt interface{}, connID uint64) {
	// Verify this event is for the current connection
	currentConnID := atomic.LoadUint64(&m.connectionID)
	if currentConnID != connID {
		// Old connection event, ignore
		return
	}

	switch v := evt.(type) {
	case *events.Connected:
		m.mu.Lock()
		m.status = WAStatusConnected
		m.qrCode = ""
		m.reconnectCount = 0
		m.mu.Unlock()
		log.Printf("✅ WhatsApp connected [connID: %d]", connID)

	case *events.Disconnected:
		wasConnected := m.GetStatus() == WAStatusConnected
		m.setStatus(WAStatusDisconnected)
		log.Printf("❌ WhatsApp disconnected [connID: %d]", connID)

		// Auto-reconnect only if we were previously connected and have a session
		if wasConnected {
			m.mu.RLock()
			hasSession := m.client != nil && m.client.Store.ID != nil
			m.mu.RUnlock()

			if hasSession {
				m.scheduleReconnect()
			}
		}

	case *events.LoggedOut:
		m.mu.Lock()
		m.status = WAStatusDisconnected
		m.qrCode = ""
		m.reconnectCount = 0
		m.qrCancel = nil
		m.mu.Unlock()
		log.Printf("⚠️ WhatsApp logged out [connID: %d]", connID)

		// Clear any scheduled reconnects
		m.cancelOngoingOperations()

	case *events.PairSuccess:
		log.Printf("✅ WhatsApp pairing successful [connID: %d]", connID)
		// Status will be updated by Connected event

	case *events.ConnectFailure:
		m.setStatus(WAStatusDisconnected)
		log.Printf("❌ WhatsApp connect failure [connID: %d]: %v", connID, v)

		// Schedule reconnect if we have a session
		m.mu.RLock()
		hasSession := m.client != nil && m.client.Store.ID != nil
		m.mu.RUnlock()

		if hasSession && m.reconnectCount < waMaxReconnectAttempts {
			m.scheduleReconnect()
		}

	case *events.StreamReplaced:
		log.Printf("🔄 WhatsApp stream replaced [connID: %d]", connID)
		// Connection is still valid, just the stream was replaced
	}
}

func (m *WAManager) scheduleReconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.reconnectCount >= waMaxReconnectAttempts {
		log.Printf("⚠️ Max reconnect attempts (%d) reached, manual intervention required", waMaxReconnectAttempts)
		return
	}

	// Cancel existing reconnect
	if m.reconnectCancel != nil {
		m.reconnectCancel()
	}

	m.reconnectCount++

	ctx, cancel := context.WithCancel(context.Background())
	m.reconnectCancel = cancel

	go func(count int) {
		delay := time.Duration(count) * waReconnectDelay
		log.Printf("🔄 Scheduling WhatsApp reconnect attempt %d in %v", count, delay)

		select {
		case <-time.After(delay):
			log.Printf("🔄 Attempting WhatsApp reconnect (attempt %d/%d)", count, waMaxReconnectAttempts)
			if err := m.Connect(); err != nil {
				log.Printf("⚠️ WhatsApp reconnect attempt %d failed: %v", count, err)
			} else {
				log.Printf("✅ WhatsApp reconnect attempt %d successful", count)
			}
		case <-ctx.Done():
			log.Printf("🔄 WhatsApp reconnect attempt %d cancelled", count)
		}
	}(m.reconnectCount)
}

func (m *WAManager) setStatus(s WAStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.status != s {
		log.Printf("📱 WhatsApp status: %s -> %s", m.status, s)
		m.status = s
	}

	if s != WAStatusWaitingQR {
		m.qrCode = ""
	}
}
