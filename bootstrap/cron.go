package bootstrap

import (
	"chronosphere/config"
	"chronosphere/domain"
	"chronosphere/service"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"chronosphere/utils"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

func InitCron(teacherPaymentService domain.TeacherPaymentUseCase, db *gorm.DB, waMgr *config.WAManager, adminRepo domain.AdminRepository) *cron.Cron {
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
	_, err = c.AddFunc("0 5 * * *", func() { // Changed from "0 1 * * 1" (Mon) to "0 1 * * *" (daily)
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

	// Every day at 05:00 — activate manual payments with a scheduled future date
	_, err = c.AddFunc("0 5 * * *", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := activateDuePackages(ctx, db, adminRepo, waMgr); err != nil {
			log.Printf("❌ [CRON] activateDuePackages: %v", err)
		}
	})
	if err != nil {
		log.Fatalf("❌ Failed to register activateDuePackages cron: %v", err)
	}

	c.Start()
	log.Println("✅ Cron Jobs started.")
	return c
}

func activateDuePackages(ctx context.Context, db *gorm.DB, adminRepo domain.AdminRepository, waMgr *config.WAManager) error {
	today := time.Now().Truncate(24 * time.Hour)

	var due []domain.PendingPackageActivation
	if err := db.WithContext(ctx).
		Where("status = ? AND activate_on <= ?", "scheduled", today).
		Find(&due).Error; err != nil {
		return fmt.Errorf("gagal mengambil jadwal aktivasi: %w", err)
	}

	log.Printf("⏰ [CRON] %d package(s) due for activation", len(due))

	for _, pa := range due {
		if _, _, err := adminRepo.AssignPackageToStudent(ctx, pa.StudentUUID, pa.PackageID); err != nil {
			log.Printf("❌ [CRON] activation failed for pending #%d: %v", pa.ID, err)
			continue
		}

		now := time.Now()
		if err := db.WithContext(ctx).
			Model(&domain.PendingPackageActivation{}).
			Where("id = ?", pa.ID).
			Updates(map[string]interface{}{"status": "activated", "activated_at": &now}).Error; err != nil {
			log.Printf("⚠️ [CRON] activated #%d but failed to update status: %v", pa.ID, err)
		}

		// Write recognition rows now that the package is actually live —
		// mirrors what ConfirmManualPayment does for immediate activations.
		if pkg, err := adminRepo.GetPackagesByID(ctx, pa.PackageID); err == nil {
			months := pkg.ExpiredDuration / 30
			if months <= 0 {
				months = 1
			}
			rows := service.BuildRecognitionRows("manual_payment", pa.ManualPaymentID, pa.StudentUUID, pa.PackageID, pa.PricePaid, months, now)
			if err := adminRepo.CreateRecognitionRows(ctx, rows); err != nil {
				log.Printf("⚠️ [CRON] failed to write recognition rows for pending #%d: %v", pa.ID, err)
			}
		} else {
			log.Printf("⚠️ [CRON] recognition rows skipped for pending #%d, package lookup failed: %v", pa.ID, err)
		}

		notifyPackageActivated(ctx, adminRepo, waMgr, pa.StudentUUID, pa.PackageID)
	}

	log.Printf("✅ [CRON] activateDuePackages done")
	return nil
}

func notifyPackageActivated(ctx context.Context, adminRepo domain.AdminRepository, waMgr *config.WAManager, studentUUID string, packageID int) {
	if waMgr == nil || !waMgr.IsLoggedIn() {
		return
	}
	student, err := adminRepo.GetStudentByUUID(ctx, studentUUID)
	if err != nil {
		return
	}
	pkg, err := adminRepo.GetPackagesByID(ctx, packageID)
	if err != nil {
		return
	}
	phone := utils.NormalizePhoneNumber(student.Phone)
	if phone == "" {
		return
	}

	appURL := "https://www.mdxmusiccourse.cloud/"
	appName := utils.GetAppName()

	msg := fmt.Sprintf(
		"🎉 Halo %s!\n\n"+
			"✅ *Paket Aktif!*\n\n"+
			"Paket *\"%s\"* kamu sekarang sudah aktif dan siap digunakan.\n\n"+
			"📅 Kuota: %d sesi\n\n"+
			"Silakan login untuk mulai booking sesi. Selamat belajar! 🎵\n\n"+
			"🌐 %s\n"+
			"🔔 %s Notification System",
		student.Name, pkg.Name, pkg.Quota,
		appURL, appName,
	)

	if err := waMgr.SendMessage(phone, msg); err != nil {
		log.Printf("⚠️ WA activation notify failed for %s: %v", phone, err)
	}
}

// Renamed from sendWeeklyBookingReminder to sendDailyBookingReminder
func sendDailyBookingReminder(ctx context.Context, db *gorm.DB, waMgr *config.WAManager) error {
	loc, _ := time.LoadLocation("Asia/Makassar")
	now := time.Now().In(loc)
	sevenDaysAgo := now.AddDate(0, 0, -7)
	appName := os.Getenv("APP_NAME")
	if appName == "" {
		appName = "MDX"
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
			"https://www.mdxmusiccourse.cloud/",
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
