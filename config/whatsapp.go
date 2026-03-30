// config/initwa.go
package config

import (
	"chronosphere/utils"
	"context"
	"fmt"
	"log"

	_ "github.com/lib/pq"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		fmt.Println("Received a message!", v.Message.GetConversation())
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

	clientLog := waLog.Stdout("Client", "INFO", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)
	client.AddEventHandler(eventHandler)

	if client.Store.ID != nil {
		if err := client.Connect(); err != nil {
			log.Println("⚠️ Failed to connect WhatsApp client silently: ", err)
			// Return it anyway so we can fix it from the GUI and re-connect!
		} else {
			log.Print("✅ Connected to ", utils.ColorText("Whatsapp", utils.Green), " successfully")
		}
	} else {
		log.Print("⚠️ Whatsapp not connected. Go to admin panel to link device.")
	}

	return client, ctx, nil
}
