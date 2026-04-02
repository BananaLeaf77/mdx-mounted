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

type teacherService struct {
	repo      domain.TeacherRepository
	messenger *whatsmeow.Client
}

func NewTeacherService(TeacherRepo domain.TeacherRepository, meow *whatsmeow.Client) domain.TeacherUseCase {
	return &teacherService{repo: TeacherRepo, messenger: meow}
}

func (s *teacherService) DeleteAvailabilityBasedOnDay(ctx context.Context, teacherUUID string, dayOfWeek string) error {
	return s.repo.DeleteAvailabilityBasedOnDay(ctx, teacherUUID, dayOfWeek)
}

func (s *teacherService) GetMyClassHistory(ctx context.Context, teacherUUID string, f domain.PaginationFilter) (*[]domain.ClassHistory, error) {
	return s.repo.GetMyClassHistory(ctx, teacherUUID, f)
}

func (s *teacherService) FinishClass(ctx context.Context, bookingID int, teacherUUID string, payload domain.ClassHistory) error {
	return s.repo.FinishClass(ctx, bookingID, teacherUUID, payload)
}

func (s *teacherService) CancelBookedClass(ctx context.Context, bookingID int, teacherUUID string, reason *string) error {
	// set default reason if its nil
	if reason == nil {
		defaultReason := "Alasan tidak diberikan"
		reason = &defaultReason
	}

	data, err := s.repo.CancelBookedClass(ctx, bookingID, teacherUUID, reason)
	if err != nil {
		return err
	}

	// Send WhatsApp messages to teacher and student
	if s.messenger != nil {
		s.sendCancelClassByTeacherNotif(data, reason)
	}

	return nil
}

func (s *teacherService) sendCancelClassByTeacherNotif(booking *domain.Booking, reason *string) {
	// Format the class date and time in Indonesian
	loc, _ := time.LoadLocation("Asia/Makassar") // WITA timezone
	classDate := booking.ClassDate.In(loc)

	// Indonesian day names
	dayName := map[string]string{
		"Monday":    "Senin",
		"Tuesday":   "Selasa",
		"Wednesday": "Rabu",
		"Thursday":  "Kamis",
		"Friday":    "Jumat",
		"Saturday":  "Sabtu",
		"Sunday":    "Minggu",
	}[classDate.Weekday().String()]

	dateStr := classDate.Format("02/01/2006")
	classTime := fmt.Sprintf("%s - %s", booking.Schedule.StartTime, booking.Schedule.EndTime)

	// Message for teacher
	// Gender check
	var teacherMessage string
	if booking.Schedule.Teacher.Gender == "male" {
		teacherMessage = fmt.Sprintf(`https://www.madeu.app/ 

*PEMBATALAN KELAS* 

Halo Bapak %s, 

Pembatalan kelas berhasil:
👤 *Nama Siswa:* %s
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s
⏱️ *Durasi:* %d menit
🎵 *Instrument:* %s

*Alasan:* %s

Terima kasih! 🎵


%s
🔔 %s Notification System`,
			booking.Schedule.Teacher.Name,
			booking.Student.Name,
			dayName,
			dateStr,
			classTime,
			booking.Schedule.Duration,
			booking.PackageUsed.Package.Instrument.Name,
			*reason,
			"https://www.madeu.app/ ",
			os.Getenv("APP_NAME"))
	} else {
		teacherMessage = fmt.Sprintf(`https://www.madeu.app/ 

*PEMBATALAN KELAS* 

Halo Ibu %s, 

Pembatalan kelas berhasil:
👤 *Nama Siswa:* %s
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s
⏱️ *Durasi:* %d menit
🎵 *Instrument:* %s

*Alasan:* %s


%s
🔔 %s Notification System`,
			booking.Schedule.Teacher.Name,
			booking.Student.Name,
			dayName,
			dateStr,
			classTime,
			booking.Schedule.Duration,
			booking.PackageUsed.Package.Instrument.Name,
			*reason,
			"https://www.madeu.app/ ",
			os.Getenv("APP_NAME"))
	}

	// Message for student
	studentMessage := fmt.Sprintf(`https://www.madeu.app/ 

*PEMBATALAN KELAS* 

Halo %s, 

⚠️ Kelas telah dibatalkan oleh guru! 

*Detail Kelas:* 
👨‍🏫 *Guru:* %s
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s
⏱️ *Durasi:* %d menit
🎵 *Instrument:* %s

*Alasan:* %s

Terima kasih! 🎵


%s
🔔 %s Notification System`,
		booking.Student.Name,
		booking.Schedule.Teacher.Name,
		dayName,
		dateStr,
		classTime,
		booking.Schedule.Duration,
		booking.PackageUsed.Package.Instrument.Name,
		*reason,
		"https://www.madeu.app/ ",
		os.Getenv("APP_NAME"))

	// Send messages asynchronously (don't block the booking)
	go func() {
		// Parse phone numbers (remove + if present, ensure proper format)
		teacherPhone := utils.NormalizePhoneNumber(booking.Schedule.Teacher.Phone)
		studentPhone := utils.NormalizePhoneNumber(booking.Student.Phone)

		// Send to teacher
		if teacherPhone != "" {
			err := utils.SendWhatsAppMessage(s.messenger, teacherPhone, teacherMessage)
			if err != nil {
				log.Printf("🔕 Failed to send WhatsApp to teacher %s: %v", teacherPhone, err)
			} else {
				log.Printf("🔔 WhatsApp notification sent to teacher: %s", booking.Schedule.Teacher.Name)
			}
		}

		// Send to student
		if studentPhone != "" {
			err := utils.SendWhatsAppMessage(s.messenger, studentPhone, studentMessage)
			if err != nil {
				log.Printf("🔕 Failed to send WhatsApp to student %s: %v", studentPhone, err)
			} else {
				log.Printf("🔔 WhatsApp notification sent to student: %s", booking.Student.Name)
			}
		}

	}()
}

func (s *teacherService) GetAllBookedClass(ctx context.Context, teacherUUID string, f domain.PaginationFilter) (*[]domain.Booking, error) {
	return s.repo.GetAllBookedClass(ctx, teacherUUID, f)
}

func (s *teacherService) GetMyProfile(ctx context.Context, uuid string) (*domain.User, error) {
	return s.repo.GetMyProfile(ctx, uuid)
}

func (s *teacherService) UpdateTeacherData(ctx context.Context, userUUID string, user domain.User) error {
	return s.repo.UpdateTeacherData(ctx, userUUID, user)
}

// ✅ Get teacher schedules
func (uc *teacherService) GetMySchedules(ctx context.Context, teacherUUID string) (*[]domain.TeacherSchedule, error) {
	return uc.repo.GetMySchedules(ctx, teacherUUID)
}

func (s *teacherService) AddAvailability(ctx context.Context, req *[]domain.TeacherSchedule) error {
	return s.repo.AddAvailability(ctx, req)
}

// ✅ Delete availability (only if not booked)
func (uc *teacherService) DeleteAvailability(ctx context.Context, scheduleID int, teacherUUID string) error {
	return uc.repo.DeleteAvailability(ctx, scheduleID, teacherUUID)
}
