// config/whatsapp.go
package config

import (
	"log"
)

// InitWA creates a WAManager using the same Postgres DSN used by the main DB,
// then immediately attempts to connect (either resumes session or starts QR).
// Returns the manager; callers use it instead of the raw whatsmeow.Client.
func InitWA(dbAddress string) (*WAManager, error) {
	mgr := NewWAManager(dbAddress)

	if err := mgr.Connect(); err != nil {
		log.Printf("⚠️  WhatsApp initial connect failed: %v", err)
		// Return the manager anyway — admin can trigger reconnect from dashboard.
		return mgr, nil
	}

	log.Println("✅ WhatsApp manager initialised")
	return mgr, nil
}