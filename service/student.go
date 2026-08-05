package service

import (
	"chronosphere/config"
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"
)

type studentUseCase struct {
	repo      domain.StudentRepository
	messenger *config.WAManager
}

func NewStudentUseCase(repo domain.StudentRepository, mgr *config.WAManager) domain.StudentUseCase {
	return &studentUseCase{repo: repo, messenger: mgr}
}

func (s *studentUseCase) GetAdminWhatsAppNumber() string {
	if s.messenger == nil || !s.messenger.IsLoggedIn() {
		return ""
	}
	jid := s.messenger.GetJID()
	if jid == "" {
		return ""
	}
	// JID format: "628xxx@s.whatsapp.net" — extract the number part
	if idx := strings.Index(jid, "@"); idx != -1 {
		return jid[:idx]
	}
	return jid
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
		if data != nil {
			msg := fmt.Sprintf(
				"❌ *Kelas Dibatalkan Siswa*\nSiswa: %s\nGuru: %s\nAlasan: %s\n\n🔕 Notifikasi WA dilewati (tidak terhubung)",
				data.Student.Name, data.Schedule.Teacher.Name, *reason,
			)
			if telegramErr := utils.NotifyTelegram(msg); telegramErr != nil {
				zlog.Warn().Msg(fmt.Sprintf("failed to send telegram cancel notif: %v", telegramErr))
			}
		}
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
		if data != nil {
			msg := fmt.Sprintf(
				"📅 *Booking Baru*\nSiswa: %s\nGuru: %s\nTanggal: %s\nJam: %s\nNotifikasi WA: 🔕 Dilewati (WhatsApp tidak terhubung)",
				data.Student.Name,
				data.Schedule.Teacher.Name,
				data.ClassDate.Format("Mon, 02 Jan 2006"),
				data.ClassDate.Format("15:04"),
			)
			if telegramErr := utils.NotifyTelegram(msg); telegramErr != nil {
				zlog.Warn().Msg(fmt.Sprintf("failed to send telegram booking notif: %v", telegramErr))
			}
		}
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
		zlog.Warn().Msg("WhatsApp not connected, skipping book notification (BookClassTrial)")
	} else {
		s.sendBookClassNotif(data)
	}

	if data != nil {
		teacherName := data.Schedule.Teacher.Name
		msg := fmt.Sprintf(
			"📅 *Booking Baru*\nSiswa: %s\nGuru: %s\nTanggal: %s\nJam: %s",
			data.Student.Name,
			teacherName,
			data.ClassDate.Format("Mon, 02 Jan 2006"),
			data.ClassDate.Format("15:04"),
		)
		if telegramErr := utils.NotifyTelegram(msg); telegramErr != nil {
			zlog.Warn().Msg(fmt.Sprintf("failed to send telegram booking notif: %v", telegramErr))
		}
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

	instrumentName := "-"
	if booking.PackageUsed.Package != nil {
		if booking.PackageUsed.Package.TrialInstrument != "" {
			instrumentName = booking.PackageUsed.Package.TrialInstrument
		} else if booking.PackageUsed.Package.Instrument != nil {
			instrumentName = booking.PackageUsed.Package.Instrument.Name
		}
	}
	if instrumentName == "" {
		instrumentName = "-"
	}

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
		instrumentName,
		*reason,
		"https://www.mdxmusiccourse.cloud/", os.Getenv("APP_NAME"),
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
		instrumentName,
		*reason,
		"https://www.mdxmusiccourse.cloud/", os.Getenv("APP_NAME"),
	)

	mgr := s.messenger
	tPhone := booking.Schedule.Teacher.Phone
	sPhone := booking.Student.Phone
	tName := booking.Schedule.Teacher.Name
	sName := booking.Student.Name

	go func() {
		teacherErr := sendWA(mgr, tPhone, teacherMsg)
		studentErr := sendWA(mgr, sPhone, studentMsg)

		status := func(err error) string {
			if err != nil {
				return "❌ Gagal"
			}
			return "✅ Terkirim"
		}

		summary := fmt.Sprintf(
			"❌ *Kelas Dibatalkan Siswa*\nSiswa: %s\nGuru: %s\nTanggal: %s, %s\nJam: %s\nAlasan: %s\n\nWA Guru: %s\nWA Siswa: %s",
			sName, tName, dayName, dateStr, classTime, *reason,
			status(teacherErr), status(studentErr),
		)
		if teacherErr != nil {
			summary += fmt.Sprintf("\n\n⚠️ Error Guru: %v", teacherErr)
		}
		if studentErr != nil {
			summary += fmt.Sprintf("\n⚠️ Error Siswa: %v", studentErr)
		}

		if telegramErr := utils.NotifyTelegram(summary); telegramErr != nil {
			zlog.Warn().Msg(fmt.Sprintf("failed to send telegram cancel notif: %v", telegramErr))
		}
	}()
}

func (s *studentUseCase) sendBookClassNotif(booking *domain.Booking) {
	loc, _ := time.LoadLocation("Asia/Makassar")
	classDate := booking.ClassDate.In(loc)
	dayName := indonesianDayName(classDate.Weekday())
	dateStr := classDate.Format("02/01/2006")
	classTime := fmt.Sprintf("%s - %s", booking.Schedule.StartTime, booking.Schedule.EndTime)
	salutation := salutationFor(booking.Schedule.Teacher.Gender)

	instrumentName := "-"
	if booking.PackageUsed.Package != nil {
		if booking.PackageUsed.Package.TrialInstrument != "" {
			instrumentName = booking.PackageUsed.Package.TrialInstrument
		} else if booking.PackageUsed.Package.Instrument != nil {
			instrumentName = booking.PackageUsed.Package.Instrument.Name
		}
	}
	if instrumentName == "" {
		instrumentName = "-"
	}

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
		instrumentName,
		"https://www.mdxmusiccourse.cloud/", os.Getenv("APP_NAME"),
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
- Hubungi guru atau admin
- Batalkan minimal 1 hari (24 jam) sebelum kelas

_Selamat belajar! 🎶_

🌐 Website: %s
🔔 %s Notification System`,
		booking.Student.Name,
		booking.Schedule.Teacher.Name,
		dayName, dateStr, classTime,
		booking.Schedule.Duration,
		instrumentName,
		"https://www.mdxmusiccourse.cloud/", os.Getenv("APP_NAME"),
	)

	mgr := s.messenger
	tPhone := booking.Schedule.Teacher.Phone
	sPhone := booking.Student.Phone
	tName := booking.Schedule.Teacher.Name
	sName := booking.Student.Name

	go func() {
		teacherErr := sendWA(mgr, tPhone, teacherMsg)
		studentErr := sendWA(mgr, sPhone, studentMsg)

		status := func(err error) string {
			if err != nil {
				return "❌ Gagal"
			}
			return "✅ Terkirim"
		}

		summary := fmt.Sprintf(
			"📅 *Booking Baru*\nSiswa: %s\nGuru: %s\nTanggal: %s, %s\nJam: %s\n\nWA Guru: %s\nWA Siswa: %s",
			sName, tName, dayName, dateStr, classTime,
			status(teacherErr), status(studentErr),
		)
		if teacherErr != nil {
			summary += fmt.Sprintf("\n\n⚠️ Error Guru: %v", teacherErr)
		}
		if studentErr != nil {
			summary += fmt.Sprintf("\n⚠️ Error Siswa: %v", studentErr)
		}

		if telegramErr := utils.NotifyTelegram(summary); telegramErr != nil {
			zlog.Warn().Msg(fmt.Sprintf("failed to send telegram booking notif: %v", telegramErr))
		}
	}()
}

// sendWA is a fire-and-forget helper shared across student service.
func sendWA(mgr *config.WAManager, phone, msg string) error {
	normalized := utils.NormalizePhoneNumber(phone)
	if normalized == "" {
		zlog.Warn().Msg(fmt.Sprintf("WA send skipped, invalid phone: %s", phone))
		return fmt.Errorf("invalid phone number: %s", phone)
	}
	if err := mgr.SendMessage(normalized, msg); err != nil {
		zlog.Warn().Msg(fmt.Sprintf("WA send failed to %s: %v", phone, err))
		return err
	}
	zlog.Info().Msg(fmt.Sprintf("WA notification sent to: %s", phone))
	return nil
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
