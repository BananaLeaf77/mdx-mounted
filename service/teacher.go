package service

import (
	"chronosphere/config"
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"fmt"
	"os"
	"time"

	zlog "github.com/rs/zerolog/log"
)

type teacherService struct {
	repo      domain.TeacherRepository
	messenger *config.WAManager
}

func NewTeacherService(repo domain.TeacherRepository, mgr *config.WAManager) domain.TeacherUseCase {
	return &teacherService{repo: repo, messenger: mgr}
}

func (s *teacherService) DeleteAvailabilityBasedOnDay(ctx context.Context, teacherUUID string, dayOfWeek string) error {
	return s.repo.DeleteAvailabilityBasedOnDay(ctx, teacherUUID, dayOfWeek)
}

func (s *teacherService) GetMyClassHistory(ctx context.Context, teacherUUID string, f domain.PaginationFilter) (*[]domain.ClassHistory, error) {
	res, err := s.repo.GetMyClassHistory(ctx, teacherUUID, f)
	if err != nil {
		return nil, err
	}
	if res != nil {
		for i := range *res {
			(*res)[i].Booking.Student.Email = ""
			(*res)[i].Booking.Student.Phone = ""
		}
	}
	return res, nil
}

func (s *teacherService) FinishClass(ctx context.Context, bookingID int, teacherUUID string, payload domain.ClassHistory) error {
	return s.repo.FinishClass(ctx, bookingID, teacherUUID, payload)
}

func (s *teacherService) CancelBookedClass(ctx context.Context, bookingID int, teacherUUID string, reason *string) error {
	if reason == nil {
		defaultReason := "Alasan tidak diberikan"
		reason = &defaultReason
	}
	data, err := s.repo.CancelBookedClass(ctx, bookingID, teacherUUID, reason)
	if err != nil {
		return err
	}

	if s.messenger == nil || !s.messenger.IsLoggedIn() {
		if data != nil {
			msg := fmt.Sprintf(
				"❌ *Kelas Dibatalkan Guru*\nSiswa: %s\nGuru: %s\nAlasan: %s\n\n🔕 Notifikasi WA dilewati (tidak terhubung)",
				data.Student.Name, data.Schedule.Teacher.Name, *reason,
			)
			if telegramErr := utils.NotifyTelegram(msg); telegramErr != nil {
				zlog.Warn().Msg(fmt.Sprintf("failed to send telegram cancel notif: %v", telegramErr))
			}
		}
		return nil
	}

	s.sendCancelClassByTeacherNotif(data, reason)
	return nil
}

func (s *teacherService) GetAllBookedClass(ctx context.Context, teacherUUID string, f domain.PaginationFilter) (*[]domain.Booking, error) {
	res, err := s.repo.GetAllBookedClass(ctx, teacherUUID, f)
	if err != nil {
		return nil, err
	}
	if res != nil {
		for i := range *res {
			(*res)[i].Student.Email = ""
			(*res)[i].Student.Phone = ""
		}
	}
	return res, nil
}

func (s *teacherService) GetMyProfile(ctx context.Context, uuid string) (*domain.User, error) {
	return s.repo.GetMyProfile(ctx, uuid)
}

func (s *teacherService) UpdateTeacherData(ctx context.Context, userUUID string, user domain.User) error {
	return s.repo.UpdateTeacherData(ctx, userUUID, user)
}

func (s *teacherService) GetMySchedules(ctx context.Context, teacherUUID string) (*[]domain.TeacherSchedule, error) {
	return s.repo.GetMySchedules(ctx, teacherUUID)
}

func (s *teacherService) AddAvailability(ctx context.Context, req *[]domain.TeacherSchedule) error {
	return s.repo.AddAvailability(ctx, req)
}

func (s *teacherService) DeleteAvailability(ctx context.Context, scheduleID int, teacherUUID string) error {
	return s.repo.DeleteAvailability(ctx, scheduleID, teacherUUID)
}

// ─────────────────────────────────────────────────────────────────────────────
// Notification helpers
// ─────────────────────────────────────────────────────────────────────────────
func (s *teacherService) sendCancelClassByTeacherNotif(booking *domain.Booking, reason *string) {
	loc, _ := time.LoadLocation("Asia/Makassar")
	classDate := booking.ClassDate.In(loc)

	dayName := map[string]string{
		"Monday": "Senin", "Tuesday": "Selasa", "Wednesday": "Rabu",
		"Thursday": "Kamis", "Friday": "Jumat", "Saturday": "Sabtu", "Sunday": "Minggu",
	}[classDate.Weekday().String()]

	dateStr := classDate.Format("02/01/2006")
	classTime := fmt.Sprintf("%s - %s", booking.Schedule.StartTime, booking.Schedule.EndTime)

	salutation := "Bapak"
	if booking.Schedule.Teacher.Gender == "female" {
		salutation = "Ibu"
	}

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

Pembatalan kelas berhasil:
👤 *Nama Siswa:* %s
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s
⏱️ *Durasi:* %d menit
🎵 *Instrument:* %s

*Alasan:* %s

🌐 Website: %s
🔔 %s Notification System`,
		salutation, booking.Schedule.Teacher.Name,
		booking.Student.Name,
		dayName, dateStr, classTime,
		booking.Schedule.Duration,
		instrumentName,
		*reason,
		"https://www.mdxmusiccourse.cloud/",
		os.Getenv("APP_NAME"))

	studentMsg := fmt.Sprintf(`*PEMBATALAN KELAS*

Halo %s,

⚠️ Kelas telah dibatalkan oleh guru!

*Detail Kelas:*
👨‍🏫 *Guru:* %s
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s
⏱️ *Durasi:* %d menit
🎵 *Instrument:* %s

*Alasan:* %s

🌐 Website: %s
🔔 %s Notification System`,
		booking.Student.Name,
		booking.Schedule.Teacher.Name,
		dayName, dateStr, classTime,
		booking.Schedule.Duration,
		instrumentName,
		*reason,
		"https://www.mdxmusiccourse.cloud/",
		os.Getenv("APP_NAME"))

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
			"❌ *Kelas Dibatalkan Guru*\nSiswa: %s\nGuru: %s\nTanggal: %s, %s\nJam: %s\nAlasan: %s\n\nWA Guru: %s\nWA Siswa: %s",
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
