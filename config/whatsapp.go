// config/initwa.go
package config

import (
	"chronosphere/utils"
	"context"
	"fmt"
	"log"
	"go.mau.fi/whatsmeow"
	_ "github.com/lib/pq"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		fmt.Println("Received a message!", v.Message.GetConversation())
	case *events.Connected:
		log.Println("✅ WhatsApp Client Connected")
	case *events.Disconnected:
		log.Println("❌ WhatsApp Client Disconnected")
	case *events.LoggedOut:
		log.Println("⚠️ WhatsApp Client Logged Out - Session Invalidated")
	case *events.IdentityChange:
		log.Printf("👤 WhatsApp Identity Change for %s (Implicit: %v). Store will be updated automatically.", v.JID.String(), v.Implicit)
	}
}

func InitWA(dbAddress string) (*whatsmeow.Client, context.Context, error) {
	dbLog := waLog.Stdout("Database", "DEBUG", true)
	ctx := context.Background()
	container, err := sqlstore.New(ctx, "postgres", dbAddress, dbLog)
	if err != nil {
		log.Fatal("Failed to initialize ", utils.ColorText("Whatsapp ", utils.Red), "client, error: ", err)
		return nil, nil, fmt.Errorf("failed to create sqlstore: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		log.Fatal("Failed to get ", utils.ColorText("Device first ", utils.Yellow), ", error: ", err)
		return nil, nil, fmt.Errorf("failed to get device: %w", err)
	}

	// clientLog := waLog.Stdout("Client", "WARN", true)
	client := whatsmeow.NewClient(deviceStore, nil)

	// Set the companion platform to macOS to improve reputation and link clickability on iOS recipients.
	// This helps WhatsApp treat the bot as a "Desktop" client rather than a suspicious generic web client.
	client.Store.Platform = "macos"

	// Improve E2EE recovery on linked devices (notably iOS) when session keys rotate.
	client.AutomaticMessageRerequestFromPhone = true
	client.AutoTrustIdentity = true
	client.UseRetryMessageStore = true
	client.AddEventHandler(eventHandler)

	if client.Store.ID != nil {
		if err := client.Connect(); err != nil {
			log.Println("⚠️ Failed to connect WhatsApp client silently: ", err)
			// Return it anyway so we can fix it from the GUI and re-connect!
		} else {
			log.Print("✅ Connected to ", utils.ColorText("Whatsapp", utils.Green), " successfully")
		}
	} else {
		log.Println("ℹ️ WhatsApp not linked. Scanning must be done manually via the Admin Dashboard.")
	}

	return client, ctx, nil
}
