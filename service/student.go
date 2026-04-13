package service

import (
	"chronosphere/config"
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"fmt"
	"log"
	"os"
	"time"
)

type studentUseCase struct {
	repo      domain.StudentRepository
	messenger *config.WAManager
}

func NewStudentUseCase(repo domain.StudentRepository, mgr *config.WAManager) domain.StudentUseCase {
	return &studentUseCase{repo: repo, messenger: mgr}
}

func (s *studentUseCase) GetTeacherDetails(ctx context.Context, teacherUUID string) (*domain.User, error) {
	teacher, err := s.repo.GetTeacherDetails(ctx, teacherUUID)
	if err != nil {
		return nil, err
	}
	if teacher != nil {
		teacher.Email = ""
		teacher.Phone = ""
	}
	return teacher, nil
}

func (s *studentUseCase) GetMyClassHistory(ctx context.Context, studentUUID string, f domain.PaginationFilter) (*[]domain.ClassHistory, error) {
	histories, err := s.repo.GetMyClassHistory(ctx, studentUUID, f)
	if err != nil {
		return nil, err
	}
	if histories != nil {
		for i := range *histories {
			(*histories)[i].Booking.Schedule.Teacher.Email = ""
			(*histories)[i].Booking.Schedule.Teacher.Phone = ""
		}
	}
	return histories, nil
}

func (s *studentUseCase) GetTeacherSchedulesForPackage(ctx context.Context, teacherUUID string, studentPackageID int, studentUUID string) (*[]domain.TeacherSchedule, error) {
	schedules, err := s.repo.GetTeacherSchedulesForPackage(ctx, teacherUUID, studentPackageID, studentUUID)
	if err != nil {
		return nil, err
	}
	if schedules != nil {
		for i := range *schedules {
			(*schedules)[i].Teacher.Email = ""
			(*schedules)[i].Teacher.Phone = ""
		}
	}
	return schedules, nil
}

func (s *studentUseCase) BulkBookPreview(ctx context.Context, studentUUID string, studentPackageID int, scheduleIDs []int) ([]domain.BulkBookPreview, error) {
	return s.repo.BulkBookPreview(ctx, studentUUID, studentPackageID, scheduleIDs)
}

func (s *studentUseCase) BulkBookClass(ctx context.Context, studentUUID string, studentPackageID int, scheduleIDs []int) (*domain.BulkBookResult, error) {
	res, err := s.repo.BulkBookClass(ctx, studentUUID, studentPackageID, scheduleIDs)
	if err != nil {
		return nil, err
	}
	if res != nil {
		for i := range res.Bookings {
			res.Bookings[i].Schedule.Teacher.Email = ""
			res.Bookings[i].Schedule.Teacher.Phone = ""
		}
	}
	return res, nil
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
	if s.messenger == nil || !s.messenger.IsLoggedIn() {
		log.Printf("🔕 WhatsApp not connected, skipping cancel notification")
		return nil
	}
	s.sendCancelClassNotif(data, reason)
	return nil
}

func (s *studentUseCase) BookClass(ctx context.Context, studentUUID string, scheduleID int, instrumentID int) (*domain.Booking, error) {
	data, err := s.repo.BookClass(ctx, studentUUID, scheduleID, instrumentID)
	if err != nil {
		return nil, err
	}
	if s.messenger == nil || !s.messenger.IsLoggedIn() {
		log.Printf("🔕 WhatsApp not connected, skipping book notification")
	} else {
		s.sendBookClassNotif(data)
	}
	if data != nil {
		data.Schedule.Teacher.Email = ""
		data.Schedule.Teacher.Phone = ""
	}
	return data, nil
}

func (s *studentUseCase) GetAvailableSchedules(ctx context.Context, studentUUID string, instrumentID int) (*[]domain.ScheduleAvailabilityResult, error) {
	res, err := s.repo.GetAvailableSchedules(ctx, studentUUID, instrumentID)
	if err != nil {
		return nil, err
	}
	if res != nil {
		for i := range *res {
			(*res)[i].TeacherSchedule.Teacher.Email = ""
			(*res)[i].TeacherSchedule.Teacher.Phone = ""
		}
	}
	return res, nil
}

func (s *studentUseCase) GetAvailableSchedulesTrial(ctx context.Context, studentUUID string, packageID int, instrumentID int) (*[]domain.ScheduleAvailabilityResult, error) {
	res, err := s.repo.GetAvailableSchedulesTrial(ctx, studentUUID, packageID, instrumentID)
	if err != nil {
		return nil, err
	}
	if res != nil {
		for i := range *res {
			(*res)[i].TeacherSchedule.Teacher.Email = ""
			(*res)[i].TeacherSchedule.Teacher.Phone = ""
		}
	}
	return res, nil
}

func (s *studentUseCase) GetAllInstruments(ctx context.Context) ([]domain.Instrument, error) {
	return s.repo.GetAllInstruments(ctx)
}

func (s *studentUseCase) BookClassTrial(ctx context.Context, studentUUID string, scheduleID int, packageID int, instrumentID int) (*domain.Booking, error) {
	data, err := s.repo.BookClassTrial(ctx, studentUUID, scheduleID, packageID, instrumentID)
	if err != nil {
		return nil, err
	}
	if s.messenger == nil || !s.messenger.IsLoggedIn() {
		log.Printf("🔕 WhatsApp not connected, skipping book notification")
	} else {
		s.sendBookClassNotif(data)
	}
	if data != nil {
		data.Schedule.Teacher.Email = ""
		data.Schedule.Teacher.Phone = ""
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
	res, err := s.repo.GetMyBookedClasses(ctx, studentUUID, f)
	if err != nil {
		return nil, err
	}
	if res != nil {
		for i := range *res {
			(*res)[i].Schedule.Teacher.Email = ""
			(*res)[i].Schedule.Teacher.Phone = ""
		}
	}
	return res, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Notification helpers
// ─────────────────────────────────────────────────────────────────────────────

func (s *studentUseCase) sendCancelClassNotif(booking *domain.Booking, reason *string) {
	loc, _ := time.LoadLocation("Asia/Makassar")
	classDate := booking.ClassDate.In(loc)
	dayName := indonesianDayName(classDate.Weekday())
	dateStr := classDate.Format("02/01/2006")
	classTime := fmt.Sprintf("%s - %s", booking.Schedule.StartTime, booking.Schedule.EndTime)
	salutation := salutationFor(booking.Schedule.Teacher.Gender)

	teacherMsg := fmt.Sprintf(`*PEMBATALAN KELAS*

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

	studentMsg := fmt.Sprintf(`*PEMBATALAN KELAS*

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

	mgr := s.messenger
	tPhone := booking.Schedule.Teacher.Phone
	sPhone := booking.Student.Phone
	go func() {
		sendWA(mgr, tPhone, teacherMsg)
		sendWA(mgr, sPhone, studentMsg)
	}()
}

func (s *studentUseCase) sendBookClassNotif(booking *domain.Booking) {
	loc, _ := time.LoadLocation("Asia/Makassar")
	classDate := booking.ClassDate.In(loc)
	dayName := indonesianDayName(classDate.Weekday())
	dateStr := classDate.Format("02/01/2006")
	classTime := fmt.Sprintf("%s - %s", booking.Schedule.StartTime, booking.Schedule.EndTime)
	salutation := salutationFor(booking.Schedule.Teacher.Gender)

	teacherMsg := fmt.Sprintf(`*PEMBERITAHUAN KELAS BARU*

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

	studentMsg := fmt.Sprintf(`*KONFIRMASI PEMESANAN KELAS*

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

	mgr := s.messenger
	tPhone := booking.Schedule.Teacher.Phone
	sPhone := booking.Student.Phone
	go func() {
		sendWA(mgr, tPhone, teacherMsg)
		sendWA(mgr, sPhone, studentMsg)
	}()
}

// sendWA is a fire-and-forget helper shared across student service.
func sendWA(mgr *config.WAManager, phone, msg string) {
	normalized := utils.NormalizePhoneNumber(phone)
	if normalized == "" {
		return
	}
	if err := mgr.SendMessage(normalized, msg); err != nil {
		log.Printf("🔕 WA send failed to %s: %v", phone, err)
	} else {
		log.Printf("🔔 WA notification sent to: %s", phone)
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
