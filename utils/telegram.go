package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// NotifyTelegram sends a plain-text message to a Telegram chat via the Bot API.
// Uses TELEGRAM_BOOKING_BOT_TOKEN / TELEGRAM_BOOKING_CHAT_ID so it can point at
// a different chat than your error-log forwarder if you want to keep the two
// feeds separate.
func NotifyTelegram(message string) error {
	token := os.Getenv("TELE_BOT_TOKEN")
	chatID := os.Getenv("TELE_CHAT_ID")
	if token == "" || chatID == "" {
		return nil // not configured — silently no-op, don't break the caller
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload, err := json.Marshal(map[string]string{
		"chat_id": chatID,
		"text":    message,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal telegram payload: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to send telegram notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}
	return nil
}