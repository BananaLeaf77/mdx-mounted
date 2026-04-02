package utils

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

func SendWhatsAppMessage(client *whatsmeow.Client, phone string, msgText string) error {
	if client == nil {
		return fmt.Errorf("whatsapp client is not initialized")
	}

	// 🔥 RESILIENCE FIX: If disconnected but session exists, try to reconnect before failing.
	if !client.IsConnected() && client.Store.ID != nil {
		log.Printf("🔌 WhatsApp disconnected; attempting to reconnect before sending message to %s...", phone)
		_ = client.Connect() // Fire reconnection attempt

		// Wait briefly for connection (max 5 seconds)
		for i := 0; i < 25; i++ {
			time.Sleep(200 * time.Millisecond)
			if client.IsConnected() {
				log.Println("✅ WhatsApp reconnected successfully!")
				break
			}
		}
	}

	if !client.IsConnected() {
		return fmt.Errorf("whatsapp client is not connected")
	}

	sendCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	jid := types.NewJID(phone, types.DefaultUserServer)

	// 🔥 CRITICAL FIX: Always query the server for their latest devices BEFORE sending.
	// This forces whatsmeow to download their current encryption keys, which fixes
	// the issue where iOS devices drop the message and it gets stuck on 1 checkmark.
	res, err := client.IsOnWhatsApp(sendCtx, []string{phone})
	if err != nil {
		log.Println("⚠️ Failed to check IsOnWhatsApp for", phone, ":", err)
		// Try to send anyway as a fallback using the constructed JID
	} else if len(res) > 0 {
		if !res[0].IsIn {
			return fmt.Errorf("phone number %s is not registered on whatsapp", phone)
		}
		// Overwrite JID with the true server-returned JID
		jid = res[0].JID
	}

	// Force device list/session refresh before encrypting outgoing payload.
	if _, err := client.GetUserDevicesContext(sendCtx, []types.JID{jid}); err != nil {
		log.Println("⚠️ Failed to refresh user devices for", jid.String(), ":", err)
	}

	// For best compatibility with iOS link-parsing, use the simplest text message type (Conversation)
	// when sending basic text with links. Basic formatting (*bold*, _italics_) still works.
	waMessage := &waE2E.Message{
		Conversation: &msgText,
	}

	_, err = client.SendMessage(sendCtx, jid, waMessage)
	if err != nil {
		// Retry once after a fresh device sync; helps when iOS sessions rotate.
		if _, refreshErr := client.GetUserDevicesContext(sendCtx, []types.JID{jid}); refreshErr != nil {
			log.Println("⚠️ Retry device refresh failed for", jid.String(), ":", refreshErr)
		}
		_, retryErr := client.SendMessage(sendCtx, jid, waMessage)
		if retryErr != nil {
			return fmt.Errorf("failed to send whatsapp message to %s: %w", phone, retryErr)
		}
	}

	return nil
}
