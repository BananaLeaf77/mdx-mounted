package utils

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
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

	// Multi-Message Split Strategy for iOS:
	// iOS often disables links embedded in long messages from unknown senders.
	// Sending the link as a separate, subsequent message triggers the "Link Preview" 
	// or "Hyperlink Bubble" UI, which is much more consistently clickable.
	
	urlPattern := `https?://[^\s\n]+`
	re := regexp.MustCompile(urlPattern)
	urls := re.FindAllString(msgText, -1)
	
	// Create clean text with URLs removed for the primary bubble
	cleanText := strings.TrimSpace(re.ReplaceAllString(msgText, ""))
	
	// Message 1: The info text
	if cleanText != "" {
		waMsg := &waE2E.Message{Conversation: &cleanText}
		if _, err := client.SendMessage(sendCtx, jid, waMsg); err != nil {
			return fmt.Errorf("failed to send text part: %w", err)
		}
		// Brief pause for natural bubble delivery order
		time.Sleep(500 * time.Millisecond)
	}
	
	// Subsequent Messages: The URLs
	for _, rawURL := range urls {
		trimmedURL := strings.TrimSpace(rawURL)
		if trimmedURL == "" {
			continue
		}
		
		// For the link bubble, use ExtendedTextMessage with MatchedText for "promotion"
		linkMsg := &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        &trimmedURL,
				MatchedText: &trimmedURL,
			},
		}
		if _, err := client.SendMessage(sendCtx, jid, linkMsg); err != nil {
			log.Println("⚠️ Failed to send URL part", trimmedURL, ":", err)
		}
		time.Sleep(300 * time.Millisecond)
	}

	return nil
}
