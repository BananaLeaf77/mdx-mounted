package config

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
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
)

type WAManager struct {
	mu        sync.RWMutex
	connectMu sync.Mutex

	client          *whatsmeow.Client
	dbAddress       string
	status          WAStatus
	qrCode          string
	reconnectCancel context.CancelFunc
	reconnectCount  int
	eventHandlerID  uint32
}

func NewWAManager(dbAddress string) *WAManager {
	return &WAManager{
		dbAddress: dbAddress,
		status:    WAStatusDisconnected,
	}
}

func (m *WAManager) Connect() error {
	m.connectMu.Lock()
	defer m.connectMu.Unlock()

	// Cancel any pending reconnect
	if m.reconnectCancel != nil {
		m.reconnectCancel()
		m.reconnectCancel = nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbLog := waLog.Stdout("Database", "WARN", true)

	container, err := sqlstore.New(ctx, "pgx", m.dbAddress, dbLog)
	if err != nil {
		return fmt.Errorf("whatsapp sqlstore: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp get device: %w", err)
	}

	m.mu.Lock()
	// Clean up existing client
	if m.client != nil {
		if m.client.IsConnected() {
			m.client.Disconnect()
		}
		// Remove event handler if it was added
		if m.eventHandlerID != 0 {
			m.client.RemoveEventHandler(m.eventHandlerID)
		}
	}

	m.client = whatsmeow.NewClient(deviceStore, nil)
	m.client.AutomaticMessageRerequestFromPhone = true
	m.client.AutoTrustIdentity = true
	m.client.UseRetryMessageStore = true
	m.client.Store.Platform = "macos"

	// Add event handler and store the ID
	m.eventHandlerID = m.client.AddEventHandler(m.handleEvent)

	client := m.client
	hasSession := client.Store.ID != nil
	m.status = WAStatusConnecting
	m.qrCode = ""
	m.mu.Unlock()

	if !hasSession {
		// No existing session - need QR pairing
		m.setStatus(WAStatusWaitingQR)

		qrCtx, qrCancel := context.WithTimeout(context.Background(), waQRTimeout)
		defer qrCancel()

		qrChan, err := client.GetQRChannel(qrCtx)
		if err != nil {
			m.setStatus(WAStatusDisconnected)
			return fmt.Errorf("failed to get QR channel: %w", err)
		}

		if err := client.Connect(); err != nil {
			m.setStatus(WAStatusDisconnected)
			return fmt.Errorf("whatsapp connect: %w", err)
		}

		// Handle QR events in goroutine
		go m.handleQRChannel(qrChan, qrCancel)
	} else {
		// Existing session - try to connect directly
		if err := client.Connect(); err != nil {
			m.setStatus(WAStatusDisconnected)
			return fmt.Errorf("whatsapp reconnect: %w", err)
		}
		// Connection status will be updated via Connected event
	}

	return nil
}

func (m *WAManager) handleQRChannel(qrChan <-chan whatsmeow.QRChannelItem, cancel context.CancelFunc) {
	defer cancel()

	for evt := range qrChan {
		switch evt.Event {
		case "code":
			m.mu.Lock()
			m.qrCode = evt.Code
			m.status = WAStatusWaitingQR
			m.mu.Unlock()
			log.Println("📱 WhatsApp QR code generated and ready")

		case "success":
			m.mu.Lock()
			m.qrCode = ""
			m.status = WAStatusConnected
			m.mu.Unlock()
			log.Println("✅ WhatsApp QR scanned — pairing successful")
			return

		case "timeout":
			log.Println("⏰ WhatsApp QR code timed out")
			m.setStatus(WAStatusDisconnected)
			return
		}
	}
}

func (m *WAManager) EnsureConnected() error {
	st := m.GetStatus()
	if st == WAStatusConnected {
		return nil
	}
	if st == WAStatusWaitingQR || st == WAStatusConnecting {
		return errors.New("connection already in progress")
	}
	return m.Connect()
}

func (m *WAManager) Disconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Cancel any reconnect attempts
	if m.reconnectCancel != nil {
		m.reconnectCancel()
		m.reconnectCancel = nil
	}

	if m.client != nil && m.client.IsConnected() {
		m.client.Disconnect()
	}
	m.status = WAStatusDisconnected
	m.reconnectCount = 0
	log.Println("🔌 WhatsApp disconnected")
}

func (m *WAManager) Logout(ctx context.Context) error {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()

	if client == nil {
		return errors.New("whatsapp client not initialised")
	}

	// Cancel reconnect attempts
	if m.reconnectCancel != nil {
		m.reconnectCancel()
		m.reconnectCancel = nil
	}

	err := client.Logout(ctx)

	m.mu.Lock()
	m.status = WAStatusDisconnected
	m.qrCode = ""
	m.reconnectCount = 0
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

	if status != WAStatusConnected {
		return errors.New("whatsapp not connected")
	}

	if !client.IsConnected() {
		return errors.New("whatsapp client disconnected")
	}

	jid := types.NewJID(phone, types.DefaultUserServer)

	sendCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Check if number is on WhatsApp
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
		// One retry with device refresh
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
	defer m.mu.RUnlock()
	return m.client != nil && m.client.IsLoggedIn() && m.status == WAStatusConnected
}

func (m *WAManager) RawClient() *whatsmeow.Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.client
}

// handleEvent processes WhatsApp events
func (m *WAManager) handleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Connected:
		m.mu.Lock()
		m.status = WAStatusConnected
		m.qrCode = ""
		m.reconnectCount = 0
		m.mu.Unlock()
		log.Printf("✅ WhatsApp connected")

	case *events.Disconnected:
		wasConnected := m.GetStatus() == WAStatusConnected
		m.setStatus(WAStatusDisconnected)
		log.Printf("❌ WhatsApp disconnected")

		// Auto-reconnect only if we were previously connected and have a session
		if wasConnected {
			m.scheduleReconnect()
		}

	case *events.LoggedOut:
		m.mu.Lock()
		m.status = WAStatusDisconnected
		m.qrCode = ""
		m.reconnectCount = 0
		m.mu.Unlock()
		log.Printf("⚠️ WhatsApp logged out")

		// Clear any scheduled reconnects
		if m.reconnectCancel != nil {
			m.reconnectCancel()
			m.reconnectCancel = nil
		}

	case *events.PairSuccess:
		log.Printf("✅ WhatsApp pairing successful")
		// Status will be updated by Connected event

	case *events.ConnectFailure:
		m.setStatus(WAStatusDisconnected)
		log.Printf("❌ WhatsApp connect failure: %v", v)

		// Schedule reconnect if we have a session
		m.mu.RLock()
		hasSession := m.client != nil && m.client.Store.ID != nil
		m.mu.RUnlock()

		if hasSession && m.reconnectCount < waMaxReconnectAttempts {
			m.scheduleReconnect()
		}
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