// config/initwa.go
package config

import (
	"chronosphere/utils"
	"context"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
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
		log.Print("⚠️ Whatsapp not connected. Generating QR code in terminal for testing...")
		qrChan, _ := client.GetQRChannel(ctx)
		if err := client.Connect(); err != nil {
			log.Println("⚠️ Failed to start WhatsApp QR channel: ", err)
		} else {
			go func() {
				for evt := range qrChan {
					if evt.Event == "code" {
						qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
						
						emailReceiver := os.Getenv("QR_CODE_EMAIL_RECEIVER")
						if emailReceiver != "" {
							errEmail := utils.SendQRCodeEmail(emailReceiver, "MadeU System - WhatsApp Authentication", "Harap scan QR code terlampir untuk mengautentikasi bot WhatsApp MadeU Anda. Kode ini kedaluwarsa dengan cepat.", evt.Code)
							if errEmail != nil {
								log.Println("⚠️ Failed to email QR code:", errEmail)
							} else {
								log.Println("✉️ Successfully emailed QR code attachment to:", emailReceiver)
							}
						}
					} else if evt.Event == "success" {
						log.Print("✅ Whatsapp authenticated successfully from terminal!")
						break
					} else if evt.Event == "timeout" {
						log.Print("⚠️ Whatsapp QR code timeout. Use admin panel to re-generate.")
						break
					}
				}
			}()
		}
	}

	return client, ctx, nil
}
