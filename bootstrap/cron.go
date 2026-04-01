package bootstrap

import (
	"chronosphere/domain"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"chronosphere/utils"

	"github.com/robfig/cron/v3"
	"go.mau.fi/whatsmeow"
	"gorm.io/gorm"
)

func InitCron(teacherPaymentService domain.TeacherPaymentUseCase, db *gorm.DB, waClient *whatsmeow.Client) *cron.Cron {
	log.Println("⏰ Initializing Cron Jobs...")

	c := cron.New(cron.WithLocation(time.Local))

	// ── Every 1st of the month at 00:00: generate monthly teacher payments ────
	_, err := c.AddFunc("0 0 1 * *", func() {
		log.Println("🚀 [CRON] Starting GenerateMonthlyPayments for the previous month...")

		loc, _ := time.LoadLocation("Asia/Makassar")
		now := time.Now().In(loc)
		targetMonth := now.AddDate(0, -1, 0)

		year := targetMonth.Year()
		month := int(targetMonth.Month())

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		details, err := teacherPaymentService.GenerateMonthlyPayments(ctx, year, month)
		if err != nil {
			log.Printf("❌ [CRON] Failed to GenerateMonthlyPayments for %d-%02d: %v", year, month, err)
			return
		}

		log.Printf("✅ [CRON] Successfully generated monthly payments for %d-%02d. Total records processed: %d", year, month, len(details))
	})
	if err != nil {
		log.Fatalf("❌ Failed to initialize monthly payment cron: %v", err)
	}

	// ── Every Monday at 09:00 WITA: remind students who haven't booked in 7 days ─
	_, err = c.AddFunc("0 9 * * 1", func() {
		log.Println("🔔 [CRON] Starting weekly student booking reminder...")

		if waClient == nil || !waClient.IsConnected() {
			log.Println("⚠️  [CRON] WhatsApp client not connected, skipping student reminder")
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		if err := sendWeeklyBookingReminder(ctx, db, waClient); err != nil {
			log.Printf("❌ [CRON] Weekly student reminder failed: %v", err)
		}
	})
	if err != nil {
		log.Fatalf("❌ Failed to initialize weekly reminder cron: %v", err)
	}

	c.Start()
	log.Println("✅ Cron Jobs started.")

	return c
}

// sendWeeklyBookingReminder finds all active students (have at least one active package)
// who have NOT booked any class in the last 7 days and sends them a WhatsApp reminder.
func sendWeeklyBookingReminder(ctx context.Context, db *gorm.DB, waClient *whatsmeow.Client) error {
	loc, _ := time.LoadLocation("Asia/Makassar")
	now := time.Now().In(loc)
	sevenDaysAgo := now.AddDate(0, 0, -7)
	appName := os.Getenv("APP_NAME")
	if appName == "" {
		appName = "MadEU"
	}

	// Find students who:
	//   1. Have at least one active package (remaining_quota > 0, end_date not expired)
	//   2. Have NOT made any booking in the last 7 days
	type studentRow struct {
		UUID  string
		Name  string
		Phone string
	}

	var students []studentRow

	err := db.WithContext(ctx).Raw(`
		SELECT u.uuid, u.name, u.phone
		FROM users u
		WHERE u.role = ?
		  AND u.deleted_at IS NULL
		  -- must have at least one active package
		  AND EXISTS (
			  SELECT 1
			  FROM student_packages sp
			  WHERE sp.student_uuid = u.uuid
			    AND sp.remaining_quota > 0
			    AND sp.end_date >= ?
		  )
		  -- must NOT have any booking in the last 7 days
		  AND NOT EXISTS (
			  SELECT 1
			  FROM bookings b
			  WHERE b.student_uuid = u.uuid
			    AND b.booked_at >= ?
		  )
	`, domain.RoleStudent, now, sevenDaysAgo).
		Scan(&students).Error

	if err != nil {
		return fmt.Errorf("gagal mengambil data siswa: %w", err)
	}

	log.Printf("🔔 [CRON] Found %d students to remind", len(students))

	sent := 0
	for _, s := range students {
		phone := utils.NormalizePhoneNumber(s.Phone)
		if phone == "" {
			continue
		}

		msg := fmt.Sprintf(`Halo %s! 👋

Kami melihat kamu belum memesan kelas dalam seminggu terakhir. Jangan sampai semangat belajarmu berhenti ya! 🎵

Kamu masih punya kuota sesi yang bisa digunakan. Yuk, segera jadwalkan kelas berikutnya sebelum kuota kamu kedaluwarsa!

📅 *Cara pesan kelas:*
Buka aplikasi → Pilih jadwal → Konfirmasi pemesanan

🌐 %s
🔔 %s Notification System`,
			s.Name,
			"https://www.madeu.app",
			appName,
		)

		// Fire-and-forget per student; log errors but continue
		if err := utils.SendWhatsAppMessage(waClient, phone, msg); err != nil {
			log.Printf("⚠️  [CRON] Failed to send reminder to %s (%s): %v", s.Name, phone, err)
		} else {
			sent++
		}

		// Small delay to avoid flooding the WhatsApp connection
		select {
		case <-ctx.Done():
			log.Printf("⚠️  [CRON] Context cancelled after sending %d reminders", sent)
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	log.Printf("✅ [CRON] Weekly reminder sent to %d/%d students", sent, len(students))
	return nil
}
