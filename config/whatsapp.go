package config

import (
	"errors"
	"log"
	"time"
)

// InitWA creates a WAManager with proper initialization
func InitWA(dbAddress string) (*WAManager, error) {
	if dbAddress == "" {
		return nil, errors.New("database address is required for WhatsApp manager")
	}
	
	mgr := NewWAManager(dbAddress)
	
	// Don't block on connection - it will happen asynchronously
	go func() {
		// Give the system a moment to settle
		time.Sleep(1 * time.Second)
		
		if err := mgr.Connect(); err != nil {
			log.Printf("⚠️ WhatsApp initial connection failed: %v", err)
			log.Println("📱 WhatsApp will be available via admin dashboard for manual connection")
		} else {
			log.Println("✅ WhatsApp manager initialized and connecting...")
		}
	}()
	
	return mgr, nil
}