package utils

import (
	"context"
	"fmt"
	"log"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// SendWhatsAppMessage safely sends a WhatsApp message by first checking if the user is on WhatsApp
// and forcing a device sync to ensure they actually receive it (fixes the 1-checkmark iOS issue).
func SendWhatsAppMessage(client *whatsmeow.Client, phone string, msgText string) error {
	if client == nil || !client.IsConnected() {
		return fmt.Errorf("whatsapp client is not connected")
	}

	jid := types.NewJID(phone, types.DefaultUserServer)

	// 🔥 CRITICAL FIX: Always query the server for their latest devices BEFORE sending.
	// This forces whatsmeow to download their current encryption keys, which fixes
	// the issue where iOS devices drop the message and it gets stuck on 1 checkmark.
	res, err := client.IsOnWhatsApp(context.Background(), []string{phone})
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

	// It's safer to use ExtendedTextMessage instead of basic Conversation for modern iOS clients
	waMessage := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: &msgText,
		},
	}

	_, err = client.SendMessage(context.Background(), jid, waMessage)
	if err != nil {
		return fmt.Errorf("failed to send whatsapp message to %s: %w", phone, err)
	}

	return nil
}
