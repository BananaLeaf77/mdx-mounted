package service

import (
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.mau.fi/whatsmeow"
)

type studentUseCase struct {
	repo      domain.StudentRepository
	messenger *whatsmeow.Client
}

func NewStudentUseCase(repo domain.StudentRepository, meowClient *whatsmeow.Client) domain.StudentUseCase {
	return &studentUseCase{repo: repo, messenger: meowClient}
}

func (s *studentUseCase) GetTeacherDetails(ctx context.Context, teacherUUID string) (*domain.User, error) {
	return s.repo.GetTeacherDetails(ctx, teacherUUID)
}

func (s *studentUseCase) GetMyClassHistory(ctx context.Context, studentUUID string, f domain.PaginationFilter) (*[]domain.ClassHistory, error) {
	return s.repo.GetMyClassHistory(ctx, studentUUID, f)
}

// ─── Bulk Book ────────────────────────────────────────────────────────────────

func (s *studentUseCase) GetTeacherSchedulesForPackage(ctx context.Context, teacherUUID string, studentPackageID int, studentUUID string) (*[]domain.TeacherSchedule, error) {
	return s.repo.GetTeacherSchedulesForPackage(ctx, teacherUUID, studentPackageID, studentUUID)
}

func (s *studentUseCase) BulkBookPreview(ctx context.Context, studentUUID string, studentPackageID int, scheduleIDs []int) ([]domain.BulkBookPreview, error) {
	return s.repo.BulkBookPreview(ctx, studentUUID, studentPackageID, scheduleIDs)
}

func (s *studentUseCase) BulkBookClass(ctx context.Context, studentUUID string, studentPackageID int, scheduleIDs []int) (*domain.BulkBookResult, error) {
	return s.repo.BulkBookClass(ctx, studentUUID, studentPackageID, scheduleIDs)
}


func (s *studentUseCase) CancelBookedClass(ctx context.Context, bookingID int, studentUUID string, reason *string) error {
	if reason == nil {
		defaultReason := "Alasan tidak diberikan"
		reason = &defaultReason
	}

	data, err := s.repo.CancelBookedClass(ctx, bookingID, studentUUID, reason)
	if err != nil {
		return err
	}

	if s.messenger != nil {
		s.sendCancelClassNotif(data, reason)
	}
	return nil
}

func (s *studentUseCase) BookClass(
	ctx context.Context,
	studentUUID string,
	scheduleID int,
	instrumentID int,
) (*domain.Booking, error) {
	data, err := s.repo.BookClass(ctx, studentUUID, scheduleID, instrumentID)
	if err != nil {
		return nil, err
	}
	if s.messenger != nil {
		s.sendBookClassNotif(data)
	}
	return data, nil
}

// GetAvailableSchedules delegates to the repository with the selected packageID,
// which enables trial-vs-regular distinction and correct room categorization.
func (s *studentUseCase) GetAvailableSchedules(
	ctx context.Context,
	studentUUID string,
	instrumentID int,
) (*[]domain.ScheduleAvailabilityResult, error) {
	return s.repo.GetAvailableSchedules(ctx, studentUUID, instrumentID)
}

func (s *studentUseCase) GetAvailableSchedulesTrial(
	ctx context.Context,
	studentUUID string,
	packageID int,
) (*[]domain.ScheduleAvailabilityResult, error) {
	return s.repo.GetAvailableSchedulesTrial(ctx, studentUUID, packageID)
}

func (s *studentUseCase) BookClassTrial(
	ctx context.Context,
	studentUUID string,
	scheduleID int,
	packageID int,
	instrumentID int,
) (*domain.Booking, error) {
	data, err := s.repo.BookClassTrial(ctx, studentUUID, scheduleID, packageID, instrumentID)
	if err != nil {
		return nil, err
	}
	if s.messenger != nil {
		s.sendBookClassNotif(data) // reuse existing notification helper
	}
	return data, nil
}

func (s *studentUseCase) GetMyProfile(ctx context.Context, userUUID string) (*domain.User, error) {
	return s.repo.GetMyProfile(ctx, userUUID)
}

func (s *studentUseCase) UpdateStudentData(ctx context.Context, userUUID string, user domain.User) error {
	return s.repo.UpdateStudentData(ctx, userUUID, user)
}

func (s *studentUseCase) GetAllAvailablePackages(ctx context.Context, studentUUID *string) (*[]domain.Package, *domain.Setting, error) {
	return s.repo.GetAllAvailablePackages(ctx, studentUUID)
}

func (s *studentUseCase) GetMyBookedClasses(ctx context.Context, studentUUID string, f domain.PaginationFilter) (*[]domain.Booking, error) {
	return s.repo.GetMyBookedClasses(ctx, studentUUID, f)
}

// ─────────────────────────────────────────────────────────────────────────────
// WhatsApp notification helpers (unchanged logic, extracted for clarity)
// ─────────────────────────────────────────────────────────────────────────────

func (s *studentUseCase) sendCancelClassNotif(booking *domain.Booking, reason *string) {
	loc, _ := time.LoadLocation("Asia/Makassar")
	classDate := booking.ClassDate.In(loc)
	dayName := indonesianDayName(classDate.Weekday())
	dateStr := classDate.Format("02/01/2006")
	classTime := fmt.Sprintf("%s - %s", booking.Schedule.StartTime, booking.Schedule.EndTime)

	salutation := salutationFor(booking.Schedule.Teacher.Gender)
	teacherMessage := fmt.Sprintf(`*PEMBATALAN KELAS*

Halo %s %s,

⚠️ Siswa *%s* telah membatalkan kelas dengan detail:
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s
⏱️ *Durasi:* %d menit
🎵 *Instrumen:* %s

*Alasan:* %s

🌐 Website: %s
🔔 %s Notification System`,
		salutation, booking.Schedule.Teacher.Name,
		booking.Student.Name,
		dayName, dateStr, classTime,
		booking.Schedule.Duration,
		booking.PackageUsed.Package.Instrument.Name,
		*reason,
		"https://www.madeu.app", os.Getenv("APP_NAME"),
	)

	studentMessage := fmt.Sprintf(`*PEMBATALAN KELAS*

Halo %s,

✅ Pembatalan kelas Anda telah berhasil!

*Detail Kelas:*
👨‍🏫 *Guru:* %s
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s
⏱️ *Durasi:* %d menit
🎵 *Instrumen:* %s

*Alasan:* %s

🌐 Website: %s
🔔 %s Notification System`,
		booking.Student.Name,
		booking.Schedule.Teacher.Name,
		dayName, dateStr, classTime,
		booking.Schedule.Duration,
		booking.PackageUsed.Package.Instrument.Name,
		*reason,
		"https://www.madeu.app", os.Getenv("APP_NAME"),
	)

	tPhone, sPhone, tMsg, sMsg :=
		booking.Schedule.Teacher.Phone, booking.Student.Phone,
		teacherMessage, studentMessage

	go func() {
		notifyCtx := context.Background()
		sendWA(s.messenger, notifyCtx, tPhone, tMsg)
		sendWA(s.messenger, notifyCtx, sPhone, sMsg)
	}()
}

func (s *studentUseCase) sendBookClassNotif(booking *domain.Booking) {
	loc, _ := time.LoadLocation("Asia/Makassar")
	classDate := booking.ClassDate.In(loc)
	dayName := indonesianDayName(classDate.Weekday())
	dateStr := classDate.Format("02/01/2006")
	classTime := fmt.Sprintf("%s - %s", booking.Schedule.StartTime, booking.Schedule.EndTime)

	salutation := salutationFor(booking.Schedule.Teacher.Gender)
	teacherMessage := fmt.Sprintf(`*PEMBERITAHUAN KELAS BARU*

Halo %s %s,

Siswa *%s* telah memesan kelas dengan detail:
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s
⏱️ *Durasi:* %d menit
🎵 *Instrumen:* %s

_Silakan persiapkan materi. Jangan lupa mencatat hasil kelas setelah selesai._

🌐 Website: %s
🔔 %s Notification System`,
		salutation, booking.Schedule.Teacher.Name,
		booking.Student.Name,
		dayName, dateStr, classTime,
		booking.Schedule.Duration,
		booking.PackageUsed.Package.Instrument.Name,
		"https://www.madeu.app", os.Getenv("APP_NAME"),
	)

	studentMessage := fmt.Sprintf(`*KONFIRMASI PEMESANAN KELAS*

Halo %s,

✅ Pemesanan kelas Anda telah berhasil!

*Detail Kelas:*
👨‍🏫 *Guru:* %s
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s
⏱️ *Durasi:* %d menit
🎵 *Instrumen:* %s

*Jika ada perubahan:*
• Hubungi guru atau admin
• Batalkan minimal 1 hari (24 jam) sebelum kelas

_Selamat belajar! 🎶_

🌐 Website: %s
🔔 %s Notification System`,
		booking.Student.Name,
		booking.Schedule.Teacher.Name,
		dayName, dateStr, classTime,
		booking.Schedule.Duration,
		booking.PackageUsed.Package.Instrument.Name,
		"https://www.madeu.app", os.Getenv("APP_NAME"),
	)

	tPhone, sPhone, tMsg, sMsg :=
		booking.Schedule.Teacher.Phone, booking.Student.Phone,
		teacherMessage, studentMessage

	go func() {
		notifyCtx := context.Background()
		sendWA(s.messenger, notifyCtx, tPhone, tMsg)
		sendWA(s.messenger, notifyCtx, sPhone, sMsg)
	}()
}

// ─────────────────────────────────────────────────────────────────────────────
// Package-level helpers shared across student service
// ─────────────────────────────────────────────────────────────────────────────

func sendWA(client *whatsmeow.Client, ctx context.Context, phone, msg string) {
	normalized := utils.NormalizePhoneNumber(phone)
	if normalized == "" {
		return
	}
	if err := utils.SendWhatsAppMessage(client, normalized, msg); err != nil {
		log.Printf("🔕 Failed to send WhatsApp: %v", err)
	} else {
		log.Printf("🔔 WhatsApp notification sent to: %s", phone)
	}
}

func indonesianDayName(wd time.Weekday) string {
	m := map[time.Weekday]string{
		time.Sunday:    "Minggu",
		time.Monday:    "Senin",
		time.Tuesday:   "Selasa",
		time.Wednesday: "Rabu",
		time.Thursday:  "Kamis",
		time.Friday:    "Jumat",
		time.Saturday:  "Sabtu",
	}
	return m[wd]
}

func salutationFor(gender string) string {
	if gender == "female" {
		return "Ibu"
	}
	return "Bapak"
}
