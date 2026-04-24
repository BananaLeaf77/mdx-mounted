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
	if !s.messenger.IsLoggedIn() {
		log.Printf("🔕 WhatsApp not connected, skipping cancel notification")
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
		booking.PackageUsed.Package.Instrument.Name,
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
		booking.PackageUsed.Package.Instrument.Name,
		*reason,
		"https://www.mdxmusiccourse.cloud/",
		os.Getenv("APP_NAME"))

	mgr := s.messenger
	tPhone := booking.Schedule.Teacher.Phone
	sPhone := booking.Student.Phone
	go func() {
		for _, pair := range [][2]string{{tPhone, teacherMsg}, {sPhone, studentMsg}} {
			normalized := utils.NormalizePhoneNumber(pair[0])
			if normalized == "" {
				continue
			}
			if err := mgr.SendMessage(normalized, pair[1]); err != nil {
				log.Printf("🔕 WA send to %s failed: %v", pair[0], err)
			} else {
				log.Printf("🔔 WA notification sent to: %s", pair[0])
			}
		}
	}()
}