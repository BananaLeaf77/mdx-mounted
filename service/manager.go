package service

import (
	"chronosphere/config"
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type managerService struct {
	managerRepo domain.ManagerRepository
	messenger   *config.WAManager
}

func NewManagerService(managerRepo domain.ManagerRepository, mgr *config.WAManager) domain.ManagerUseCase {
	return &managerService{
		managerRepo: managerRepo,
		messenger:   mgr,
	}
}

func (s *managerService) GetTeacherSchedules(ctx context.Context, teacherUUID string) ([]domain.TeacherSchedule, error) {
	if teacherUUID == "" {
		return nil, errors.New("UUID guru tidak boleh kosong")
	}
	return s.managerRepo.GetTeacherSchedules(ctx, teacherUUID)
}

func (s *managerService) GetAllTeachers(ctx context.Context, exceptTeacherUUID string) ([]domain.User, error) {
	return s.managerRepo.GetAllTeachers(ctx, exceptTeacherUUID)
}

func (s *managerService) GetCancelledClassHistories(ctx context.Context) (*[]domain.ClassHistory, error) {
	return s.managerRepo.GetCancelledClassHistories(ctx)
}

func (s *managerService) RebookWithSubstitute(ctx context.Context, req domain.RebookInput) (*domain.Booking, error) {
	booking, err := s.managerRepo.RebookWithSubstitute(ctx, req)
	if err != nil {
		return nil, err
	}

	if s.messenger != nil && s.messenger.IsLoggedIn() {
		loc, _ := time.LoadLocation("Asia/Makassar")
		classDate := booking.ClassDate.In(loc)
		dayName := map[string]string{
			"Monday": "Senin", "Tuesday": "Selasa", "Wednesday": "Rabu",
			"Thursday": "Kamis", "Friday": "Jumat", "Saturday": "Sabtu", "Sunday": "Minggu",
		}[classDate.Weekday().String()]

		salutation := "Bapak"
		if booking.Schedule.Teacher.Gender == "female" {
			salutation = "Ibu"
		}

		msg := fmt.Sprintf(
			`*PENUGASAN GURU PENGGANTI*

Halo %s %s,

Anda ditugaskan sebagai guru pengganti untuk kelas berikut:
👤 *Siswa:* %s
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s - %s
🎵 *Instrumen:* %s

Kelas ini adalah pengganti dari kelas yang dibatalkan. Silakan selesaikan kelas dan tambahkan catatan melalui aplikasi.

🌐 %s
🔔 %s Notification System`,
			salutation, booking.Schedule.Teacher.Name,
			booking.Student.Name,
			dayName, classDate.Format("02/01/2006"),
			booking.Schedule.StartTime, booking.Schedule.EndTime,
			booking.PackageUsed.Package.Instrument.Name,
			"https://www.madeu.app",
			os.Getenv("APP_NAME"),
		)

		phone := utils.NormalizePhoneNumber(booking.Schedule.Teacher.Phone)
		if phone != "" {
			go func() {
				if err := s.messenger.SendMessage(phone, msg); err != nil {
					log.Printf("🔕 WA to sub teacher %s failed: %v", phone, err)
				}
			}()
		}
	}

	return booking, nil
}

func (s *managerService) GetSetting(ctx context.Context) (*domain.Setting, error) {
	return s.managerRepo.GetSetting(ctx)
}

func (s *managerService) UpdateSetting(ctx context.Context, setting *domain.Setting) error {
	if setting == nil {
		return errors.New("pengaturan tidak valid")
	}
	return s.managerRepo.UpdateSetting(ctx, setting)
}

func (s *managerService) UpdateStudent(ctx context.Context, student *domain.User) error {
	if student.UUID == "" {
		return errors.New("uuid siswa tidak boleh kosong")
	}
	if student.Password != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(student.Password), bcrypt.DefaultCost)
		if err != nil {
			return errors.New("gagal mengenkripsi password")
		}
		student.Password = string(hashed)
	}
	if err := s.managerRepo.UpdateStudent(ctx, student); err != nil {
		return errors.New(utils.TranslateDBError(err))
	}
	return nil
}

func (s *managerService) GetAllStudents(ctx context.Context) ([]domain.User, error) {
	return s.managerRepo.GetAllStudents(ctx)
}

func (s *managerService) UpdateManager(ctx context.Context, manager *domain.User) error {
	return s.managerRepo.UpdateManager(ctx, manager)
}

func (s *managerService) GetStudentByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	return s.managerRepo.GetStudentByUUID(ctx, uuid)
}

func (s *managerService) ModifyStudentPackageQuota(ctx context.Context, studentUUID string, packageID int, incomingQuota int) error {
	data, err := s.managerRepo.ModifyStudentPackageQuota(ctx, studentUUID, packageID, incomingQuota)
	if err != nil {
		return err
	}

	if s.messenger != nil && s.messenger.IsLoggedIn() {
		phone := utils.NormalizePhoneNumber(data.Phone)
		if phone != "" {
			msg := fmt.Sprintf(
				`*NOTIFIKASI PENYESUAIAN KUOTA*

Halo %s,

Telah dilakukan penyesuaian kuota paket les Anda:
📊 Kuota saat ini: %d sesi

Kuota yang telah dikembalikan dapat segera digunakan untuk penjadwalan sesi berikutnya.

🌐 Website: %s
🔔 %s Notification System`,
				data.Name, incomingQuota,
				"https://www.madeu.app",
				os.Getenv("APP_NAME"),
			)
			go func() {
				if err := s.messenger.SendMessage(phone, msg); err != nil {
					log.Printf("🔕 WA quota notification to %s failed: %v", phone, err)
				}
			}()
		}
	}
	return nil
}
