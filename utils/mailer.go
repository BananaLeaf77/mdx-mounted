// utils/email.go
package utils

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"os"
	"time"

	"github.com/skip2/go-qrcode"
)

// SendEmail sends a plain text email
func SendEmail(to, subject, body string) error {
	from := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")

	addr := fmt.Sprintf("%s:%s", host, port)
	msg := []byte(
		"From: " + os.Getenv("SMTP_FROM") + "\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"Content-Type: text/plain; charset=\"utf-8\"\r\n" +
			"\r\n" +
			body + "\r\n")

	auth := smtp.PlainAuth("", from, pass, host)
	return smtp.SendMail(addr, auth, from, []string{to}, msg)
}

// SendQRCodeEmail sends an email with QR code attachment
func SendQRCodeEmail(to, subject, body, qrText string) error {
	from := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")

	// Generate QR code
	qrImage, err := qrcode.Encode(qrText, qrcode.Medium, 256)
	if err != nil {
		// Fallback to plain text
		return SendEmail(to, subject, body+"\nQR Code Text: "+qrText)
	}

	// Prepare email with attachment
	boundary := fmt.Sprintf("boundary-%d", time.Now().UnixNano())

	var msg bytes.Buffer
	msg.WriteString(fmt.Sprintf("From: %s\r\n", os.Getenv("SMTP_FROM")))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary))
	msg.WriteString("\r\n")

	// Text part
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	msg.WriteString("Content-Transfer-Encoding: 7bit\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)
	msg.WriteString("\r\n\r\n")

	// QR code attachment
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: image/png\r\n")
	msg.WriteString("Content-Transfer-Encoding: base64\r\n")
	msg.WriteString("Content-Disposition: attachment; filename=\"whatsapp_qr.png\"\r\n")
	msg.WriteString("\r\n")

	// Base64 encode QR code
	encoded := base64.StdEncoding.EncodeToString(qrImage)

	// Write in chunks of 76 chars per line
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		msg.WriteString(encoded[i:end] + "\r\n")
	}

	msg.WriteString("\r\n")
	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	// Send email
	addr := fmt.Sprintf("%s:%s", host, port)
	auth := smtp.PlainAuth("", from, pass, host)
	return smtp.SendMail(addr, auth, from, []string{to}, msg.Bytes())
}