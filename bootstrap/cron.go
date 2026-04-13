package bootstrap

import (
	"chronosphere/config"
	"chronosphere/domain"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"chronosphere/utils"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

func InitCron(teacherPaymentService domain.TeacherPaymentUseCase, db *gorm.DB, waMgr *config.WAManager) *cron.Cron {
	log.Println("⏰ Initializing Cron Jobs...")

	c := cron.New(cron.WithLocation(time.Local))

	// Every 1st of the month at 00:00 — generate monthly teacher payments
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
			log.Printf("❌ [CRON] GenerateMonthlyPayments %d-%02d: %v", year, month, err)
			return
		}
		log.Printf("✅ [CRON] Monthly payments generated for %d-%02d: %d records", year, month, len(details))
	})
	if err != nil {
		log.Fatalf("❌ Failed to register monthly payment cron: %v", err)
	}

	// Every day at 01:00 WITA — remind students who haven't booked in 7 days
	// For testing only - runs every minute
	// _, err = c.AddFunc("*/1 * * * *", func() {
	_, err = c.AddFunc("0 1 * * *", func() { // Changed from "0 1 * * 1" (Mon) to "0 1 * * *" (daily)
		log.Println("🔔 [CRON] Starting daily student booking reminder...")

		if waMgr == nil || !waMgr.IsLoggedIn() {
			log.Println("⚠️  [CRON] WhatsApp not connected, skipping student reminder")
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		if err := sendDailyBookingReminder(ctx, db, waMgr); err != nil { // Renamed function
			log.Printf("❌ [CRON] Daily student reminder failed: %v", err)
		}
	})
	if err != nil {
		log.Fatalf("❌ Failed to register daily reminder cron: %v", err)
	}

	c.Start()
	log.Println("✅ Cron Jobs started.")
	return c
}

// Renamed from sendWeeklyBookingReminder to sendDailyBookingReminder
func sendDailyBookingReminder(ctx context.Context, db *gorm.DB, waMgr *config.WAManager) error {
	loc, _ := time.LoadLocation("Asia/Makassar")
	now := time.Now().In(loc)
	sevenDaysAgo := now.AddDate(0, 0, -7)
	appName := os.Getenv("APP_NAME")
	if appName == "" {
		appName = "MadEU"
	}

	type studentRow struct {
		UUID  string
		Name  string
		Phone string
	}

	var students []studentRow

	err := db.WithContext(ctx).
		Model(&domain.User{}).
		Select("users.uuid, users.name, users.phone").
		Joins("INNER JOIN student_packages ON student_packages.student_uuid = users.uuid AND student_packages.remaining_quota > 0 AND student_packages.end_date >= ?", now).
		Joins("LEFT JOIN bookings ON bookings.student_uuid = users.uuid AND bookings.booked_at >= ?", sevenDaysAgo).
		Where("users.role = ?", domain.RoleStudent).
		Where("users.deleted_at IS NULL").
		Where("bookings.id IS NULL").
		Group("users.uuid, users.name, users.phone").
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

		if waMgr == nil || !waMgr.IsLoggedIn() {
			log.Printf("🔕 WhatsApp not connected, skipping reminder")
			return nil
		}

		if err := waMgr.SendMessage(phone, msg); err != nil {
			log.Printf("⚠️  [CRON] Failed to send reminder to %s (%s): %v", s.Name, phone, err)
		} else {
			sent++
		}

		select {
		case <-ctx.Done():
			log.Printf("⚠️  [CRON] Context cancelled after %d reminders", sent)
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	log.Printf("✅ [CRON] Daily reminder sent to %d/%d students", sent, len(students))
	return nil
}
