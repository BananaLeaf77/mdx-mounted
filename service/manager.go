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
	"strings"
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

func (s *managerService) GetAllBookedClasses(ctx context.Context) (*[]domain.Booking, error) {
	return s.managerRepo.GetAllBookedClasses(ctx)
}

func (s *managerService) CancelBookedClass(ctx context.Context, bookingID int, managerUUID string, reason *string) error {
	// Load booking first for notification
	var bookings *[]domain.Booking
	bookings, err := s.managerRepo.GetAllBookedClasses(ctx)
	if err != nil {
		return err
	}

	// Find the specific booking for notification
	var targetBooking *domain.Booking
	for i := range *bookings {
		if (*bookings)[i].ID == bookingID {
			targetBooking = &(*bookings)[i]
			break
		}
	}

	if err := s.managerRepo.CancelBookedClass(ctx, bookingID, managerUUID, reason); err != nil {
		return err
	}

	if targetBooking != nil && s.messenger != nil && s.messenger.IsLoggedIn() {
		s.sendManagerCancelNotif(targetBooking, reason)
	}

	return nil
}

func (s *managerService) sendManagerCancelNotif(booking *domain.Booking, reason *string) {
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

	cancelReason := "Dibatalkan oleh manajemen"
	if reason != nil && *reason != "" {
		cancelReason = *reason
	}

	appURL := "https://www.mdxmusiccourse.cloud/"
	appName := os.Getenv("APP_NAME")

	teacherMsg := fmt.Sprintf(`*PEMBATALAN KELAS OLEH MANAJEMEN*

Halo %s %s,

Kelas berikut telah dibatalkan oleh manajemen:
👤 *Siswa:* %s
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s
⏱️ *Durasi:* %d menit
🎵 *Instrumen:* %s

*Alasan:* %s

Kuota siswa telah dikembalikan secara otomatis.

🌐 %s
🔔 %s Notification System`,
		salutation, booking.Schedule.Teacher.Name,
		booking.Student.Name,
		dayName, dateStr, classTime,
		booking.Schedule.Duration,
		instrumentName,
		cancelReason,
		appURL, appName,
	)

	studentMsg := fmt.Sprintf(`*PEMBATALAN KELAS OLEH MANAJEMEN*

Halo %s,

⚠️ Kelas Anda telah dibatalkan oleh manajemen.

*Detail Kelas:*
👨‍🏫 *Guru:* %s
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s
⏱️ *Durasi:* %d menit
🎵 *Instrumen:* %s

*Alasan:* %s

✅ Kuota Anda telah dikembalikan dan dapat digunakan kembali.

🌐 %s
🔔 %s Notification System`,
		booking.Student.Name,
		booking.Schedule.Teacher.Name,
		dayName, dateStr, classTime,
		booking.Schedule.Duration,
		instrumentName,
		cancelReason,
		appURL, appName,
	)

	mgr := s.messenger
	tPhone := booking.Schedule.Teacher.Phone
	sPhone := booking.Student.Phone
	go func() {
		sendWA(mgr, tPhone, teacherMsg)
		sendWA(mgr, sPhone, studentMsg)
	}()
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
		s.sendRebookNotif(booking)
	}

	return booking, nil
}

func (s *managerService) sendRebookNotif(booking *domain.Booking) {
	loc, _ := time.LoadLocation("Asia/Makassar")
	classDate := booking.ClassDate.In(loc)
	dayName := indonesianDayName(classDate.Weekday())
	dateStr := classDate.Format("02/01/2006")
	classTime := fmt.Sprintf("%s - %s", booking.Schedule.StartTime, booking.Schedule.EndTime)
	salutation := salutationFor(booking.Schedule.Teacher.Gender)

	// ── Nil-safe instrument name (trial packages have no fixed instrument) ───
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

	appURL := "https://www.mdxmusiccourse.cloud/"
	appName := os.Getenv("APP_NAME")

	// ── 1. Notifikasi ke Guru Pengganti ─────────────────────────────────────
	teacherMsg := fmt.Sprintf(`*PENUGASAN GURU PENGGANTI*

Halo %s %s,

Anda ditugaskan sebagai guru pengganti untuk kelas berikut:
👤 *Siswa:* %s
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s
⏱️ *Durasi:* %d menit
🎵 *Instrumen:* %s

Kelas ini adalah pengganti dari kelas yang dibatalkan. Silakan selesaikan kelas dan tambahkan catatan melalui aplikasi.

🌐 Website: %s
🔔 %s Notification System`,
		salutation, booking.Schedule.Teacher.Name,
		booking.Student.Name,
		dayName, dateStr,
		classTime,
		booking.Schedule.Duration,
		instrumentName,
		appURL, appName,
	)

	// ── 2. Notifikasi ke Siswa ───────────────────────────────────────────────
	studentMsg := fmt.Sprintf(`*KONFIRMASI KELAS PENGGANTI*

Halo %s,

✅ Kelas pengganti Anda telah berhasil dijadwalkan!

*Detail Kelas:*
👨‍🏫 *Guru:* %s
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s
⏱️ *Durasi:* %d menit
🎵 *Instrumen:* %s

Kelas ini merupakan pengganti dari kelas yang sebelumnya dibatalkan. Semangat belajar! 🎶

🌐 Website: %s
🔔 %s Notification System`,
		booking.Student.Name,
		booking.Schedule.Teacher.Name,
		dayName, dateStr,
		classTime,
		booking.Schedule.Duration,
		instrumentName,
		appURL, appName,
	)

	// ── 3. Notifikasi ke Manager/Admin (JID yang sedang login) ──────────────
	managerMsg := fmt.Sprintf(`*RINGKASAN KELAS BAYANGAN*

Manager telah membuat kelas pengganti:
👤 *Siswa:* %s
👨‍🏫 *Guru Pengganti:* %s %s
📅 *Hari/Tanggal:* %s, %s
⏰ *Waktu:* %s
⏱️ *Durasi:* %d menit
🎵 *Instrumen:* %s

Notifikasi telah dikirim ke guru dan siswa.

🌐 Website: %s
🔔 %s Notification System`,
		booking.Student.Name,
		salutation, booking.Schedule.Teacher.Name,
		dayName, dateStr,
		classTime,
		booking.Schedule.Duration,
		instrumentName,
		appURL, appName,
	)

	mgr := s.messenger
	tPhone := booking.Schedule.Teacher.Phone
	sPhone := booking.Student.Phone

	// Ambil nomor manager dari JID yang sedang login
	managerJID := mgr.GetJID()
	managerPhone := ""
	if managerJID != "" {
		if at := strings.Index(managerJID, "@"); at != -1 {
			managerPhone = managerJID[:at]
		}
	}

	go func() {
		sendWA(mgr, tPhone, teacherMsg)
		sendWA(mgr, sPhone, studentMsg)
		if managerPhone != "" {
			sendWA(mgr, managerPhone, managerMsg)
		}
	}()
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

func (s *managerService) CreateStudent(ctx context.Context, user *domain.User) (*domain.User, error) {
	if user.Name == "" || user.Email == "" || user.Phone == "" || user.Password == "" {
		return nil, errors.New("semua field wajib diisi")
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.New("gagal mengenkripsi password")
	}
	user.Password = string(hashed)

	return s.managerRepo.CreateStudent(ctx, user)
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
				"https://www.mdxmusiccourse.cloud/",
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
