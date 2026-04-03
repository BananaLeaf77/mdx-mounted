package config

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// WAStatus represents the current connection state of the WhatsApp client.
type WAStatus string

const (
	WAStatusDisconnected WAStatus = "disconnected"
	WAStatusWaitingQR    WAStatus = "waiting_qr"
	WAStatusConnected    WAStatus = "connected"
	WAStatusNotLinked    WAStatus = "not_linked"

	waQRTimeout = 3 * time.Minute
)

// WAManager manages a single whatsmeow client lifecycle.
// It is safe for concurrent use.
type WAManager struct {
	mu        sync.RWMutex
	connectMu sync.Mutex // serialises Connect calls

	client    *whatsmeow.Client
	dbAddress string // postgres DSN used by sqlstore

	status WAStatus
	qrCode string // latest QR code string (empty when not waiting)
}

// NewWAManager creates a WAManager. dbAddress is the Postgres DSN.
// Call Connect() when you are ready to start the session.
func NewWAManager(dbAddress string) *WAManager {
	return &WAManager{
		dbAddress: dbAddress,
		status:    WAStatusDisconnected,
	}
}

// Connect initialises or re-initialises the whatsmeow client.
// If a session already exists in the store it reconnects silently.
// If no session exists it starts QR pairing.
// Safe to call multiple times — concurrent calls are serialised.
func (m *WAManager) Connect() error {
	m.connectMu.Lock()
	defer m.connectMu.Unlock()

	ctx := context.Background()
	dbLog := waLog.Stdout("Database", "WARN", true)

	container, err := sqlstore.New(ctx, "postgres", m.dbAddress, dbLog)
	if err != nil {
		return fmt.Errorf("whatsapp sqlstore: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("whatsapp get device: %w", err)
	}

	m.mu.Lock()
	// Disconnect any existing client before building a new one.
	if m.client != nil && m.client.IsConnected() {
		m.client.Disconnect()
	}

	m.client = whatsmeow.NewClient(deviceStore, nil)
	m.client.AutomaticMessageRerequestFromPhone = true
	m.client.AutoTrustIdentity = true
	m.client.UseRetryMessageStore = true
	m.client.Store.Platform = "macos"
	m.client.AddEventHandler(m.handleEvent)
	client := m.client
	m.mu.Unlock()

	if client.Store.ID == nil {
		// No session — start QR pairing.
		qrCtx, qrCancel := context.WithTimeout(ctx, waQRTimeout)

		qrChan, _ := client.GetQRChannel(qrCtx)
		if err := client.Connect(); err != nil {
			qrCancel()
			return fmt.Errorf("whatsapp connect: %w", err)
		}
		m.setStatus(WAStatusWaitingQR)

		go func() {
			defer qrCancel()
			for evt := range qrChan {
				switch evt.Event {
				case "code":
					m.mu.Lock()
					m.qrCode = evt.Code
					m.mu.Unlock()
					log.Println("📱 WhatsApp QR code updated")
				case "success":
					m.mu.Lock()
					m.qrCode = ""
					m.mu.Unlock()
					log.Println("✅ WhatsApp QR scanned — pairing succeeded")
					// handleEvent Connected will set status to connected.
				case "timeout":
					log.Println("⏰ WhatsApp QR code timed out")
					m.setStatus(WAStatusDisconnected)
				}
			}
		}()
	} else {
		// Session exists — just reconnect.
		if err := client.Connect(); err != nil {
			return fmt.Errorf("whatsapp reconnect: %w", err)
		}
		// handleEvent will update status once Connected fires.
	}

	return nil
}

// EnsureConnected connects if not already connected or waiting for QR.
func (m *WAManager) EnsureConnected() error {
	st := m.GetStatus()
	if st == WAStatusConnected || st == WAStatusWaitingQR {
		return nil
	}
	return m.Connect()
}

// Disconnect terminates the connection without clearing the stored session.
// The device remains linked; call Connect() to resume.
func (m *WAManager) Disconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != nil && m.client.IsConnected() {
		m.client.Disconnect()
	}
	m.status = WAStatusDisconnected
	log.Println("🔌 WhatsApp disconnected")
}

// Logout terminates the connection AND deletes the stored session.
// After logout the device is unlinked and Connect() will show a new QR.
func (m *WAManager) Logout(ctx context.Context) error {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()

	if client == nil {
		return errors.New("whatsapp client not initialised")
	}

	err := client.Logout(ctx)
	m.mu.Lock()
	m.status = WAStatusDisconnected
	m.qrCode = ""
	m.mu.Unlock()

	if err != nil {
		log.Printf("⚠️  WhatsApp logout error (session may already be gone): %v", err)
		// Don't return — the important thing is the local state is cleared.
	}

	log.Println("🔓 WhatsApp session cleared")
	return nil
}

// SendMessage sends a text message to the given normalised phone number
// (e.g. "6281234567890").
func (m *WAManager) SendMessage(phone, text string) error {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()

	if client == nil || !client.IsConnected() {
		return errors.New("whatsapp not connected")
	}

	jid := types.NewJID(phone, types.DefaultUserServer)

	sendCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Verify the number is on WhatsApp and get canonical JID.
	res, err := client.IsOnWhatsApp(sendCtx, []string{phone})
	if err != nil {
		log.Printf("⚠️  IsOnWhatsApp check failed for %s: %v — attempting send anyway", phone, err)
	} else if len(res) > 0 {
		if !res[0].IsIn {
			return fmt.Errorf("phone %s is not registered on WhatsApp", phone)
		}
		jid = res[0].JID
	}

	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(text),
		},
	}

	_, sendErr := client.SendMessage(sendCtx, jid, msg)
	if sendErr != nil {
		// One retry after refreshing device list.
		if _, refreshErr := client.GetUserDevicesContext(sendCtx, []types.JID{jid}); refreshErr != nil {
			log.Printf("⚠️  Device refresh failed for %s: %v", jid, refreshErr)
		}
		_, sendErr = client.SendMessage(sendCtx, jid, msg)
	}
	return sendErr
}

// GetStatus returns the current connection status.
func (m *WAManager) GetStatus() WAStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

// GetQRCode returns the current QR code string (empty when not pairing).
func (m *WAManager) GetQRCode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.qrCode
}

// GetJID returns the linked JID string, or "" if not linked.
func (m *WAManager) GetJID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.client == nil || m.client.Store.ID == nil {
		return ""
	}
	return m.client.Store.ID.String()
}

// IsLoggedIn returns true when the client is connected and authenticated.
func (m *WAManager) IsLoggedIn() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.client != nil && m.client.IsLoggedIn()
}

// RawClient exposes the underlying whatsmeow client for callers that need it
// (e.g. the existing utils.SendWhatsAppMessage helper).
// Returns nil if not yet initialised.
func (m *WAManager) RawClient() *whatsmeow.Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.client
}

// ─── internal ────────────────────────────────────────────────────────────────

func (m *WAManager) handleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Connected:
		m.mu.Lock()
		m.status = WAStatusConnected
		m.qrCode = ""
		m.mu.Unlock()
		log.Println("✅ WhatsApp connected")

	case *events.Disconnected:
		m.setStatus(WAStatusDisconnected)
		log.Println("❌ WhatsApp disconnected")

		// Auto-reconnect if session still exists (e.g. network blip).
		m.mu.RLock()
		hasSession := m.client != nil && m.client.Store.ID != nil
		m.mu.RUnlock()
		if hasSession {
			go func() {
				time.Sleep(5 * time.Second)
				log.Println("🔄 WhatsApp auto-reconnecting...")
				if err := m.Connect(); err != nil {
					log.Printf("⚠️  WhatsApp auto-reconnect failed: %v", err)
				}
			}()
		}

	case *events.LoggedOut:
		m.mu.Lock()
		m.status = WAStatusDisconnected
		m.qrCode = ""
		m.mu.Unlock()
		log.Printf("⚠️  WhatsApp logged out (reason: %v)", v.Reason)

	case *events.PairSuccess:
		log.Println("✅ WhatsApp pairing successful")

	case *events.ConnectFailure:
		m.setStatus(WAStatusDisconnected)
		log.Printf("❌ WhatsApp connect failure: %v", v.Reason)
	}
}

func (m *WAManager) setStatus(s WAStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = s
}
