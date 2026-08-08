package repository

import (
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type managerRepo struct {
	db *gorm.DB
}

func NewManagerRepository(db *gorm.DB) domain.ManagerRepository {
	return &managerRepo{db: db}
}

func (r *managerRepo) GetAllBookedClasses(ctx context.Context) (*[]domain.Booking, error) {
	var bookings []domain.Booking

	if err := r.db.WithContext(ctx).
		Preload("Student").
		Preload("Schedule").
		Preload("Schedule.Teacher").
		Preload("Schedule.TeacherProfile.Instruments").
		Preload("PackageUsed").
		Preload("PackageUsed.Package").
		Preload("PackageUsed.Package.Instrument").
		Preload("ClassHistory").
		Preload("CancelUser").
		Where("status IN ?", []string{
			domain.StatusBooked,
			domain.StatusOngoing,
			domain.StatusUpcoming,
			domain.StatusRescheduled,
			domain.StatusClassFinished,
		}).
		Order("class_date ASC").
		Find(&bookings).Error; err != nil {
		return nil, fmt.Errorf("gagal mengambil data kelas: %w", err)
	}

	loc, _ := time.LoadLocation("Asia/Makassar")
	now := time.Now().In(loc)

	for i := range bookings {
		// Fix trial instrument
		if bookings[i].PackageUsed.Package != nil && bookings[i].PackageUsed.Package.IsTrial {
			var instrument domain.Instrument
			r.db.WithContext(ctx).Where("id = ?", bookings[i].InstrumentID).First(&instrument)
			packageCopy := *bookings[i].PackageUsed.Package
			packageCopy.TrialInstrument = instrument.Name
			bookings[i].PackageUsed.Package = &packageCopy
		}

		// Compute live status
		parsedStart, _ := time.Parse("15:04", bookings[i].Schedule.StartTime)
		parsedEnd, _ := time.Parse("15:04", bookings[i].Schedule.EndTime)
		classDateLoc := bookings[i].ClassDate.In(loc)

		classStart := time.Date(
			classDateLoc.Year(), classDateLoc.Month(), classDateLoc.Day(),
			parsedStart.Hour(), parsedStart.Minute(), 0, 0, loc,
		)
		classEnd := time.Date(
			classDateLoc.Year(), classDateLoc.Month(), classDateLoc.Day(),
			parsedEnd.Hour(), parsedEnd.Minute(), 0, 0, loc,
		)

		switch {
		case now.Before(classStart):
			bookings[i].Status = domain.StatusUpcoming
		case now.After(classStart) && now.Before(classEnd):
			bookings[i].Status = domain.StatusOngoing
		case now.Equal(classEnd) || now.After(classEnd):
			bookings[i].IsReadyToFinish = true
			bookings[i].Status = domain.StatusClassFinished
		}
	}

	return &bookings, nil
}

func (r *managerRepo) CancelBookedClass(ctx context.Context, bookingID int, managerUUID string, reason *string) error {
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if rec := recover(); rec != nil {
			tx.Rollback()
		}
	}()

	// Load booking
	var booking domain.Booking
	if err := tx.
		Preload("Schedule").
		Preload("Schedule.Teacher").
		Preload("Student").
		Preload("PackageUsed").
		Preload("PackageUsed.Package").
		Preload("PackageUsed.Package.Instrument").
		Where("id = ? AND status = ?", bookingID, domain.StatusBooked).
		First(&booking).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("booking tidak ditemukan atau sudah dibatalkan")
		}
		return fmt.Errorf("gagal mengambil booking: %w", err)
	}

	cancelTime := time.Now()

	// Update booking status
	if err := tx.Model(&booking).
		UpdateColumns(map[string]interface{}{
			"status":       domain.StatusCancelled,
			"cancelled_at": cancelTime,
			"canceled_by":  managerUUID,
			"notes":        reason,
		}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("gagal membatalkan booking: %w", err)
	}

	// Refund quota
	if err := tx.Model(&domain.StudentPackage{}).
		Where("id = ?", booking.StudentPackageID).
		Update("remaining_quota", gorm.Expr("remaining_quota + 1")).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("gagal refund kuota: %w", err)
	}

	// Release overlapping schedules if no other active bookings
	if err := tx.Exec(`
		UPDATE teacher_schedules ts
		SET is_booked = false
		WHERE ts.teacher_uuid = ? AND ts.day_of_week = ?
		  AND (ts.start_time::time, ts.end_time::time) OVERLAPS (?::time, ?::time)
		  AND NOT EXISTS (
		      SELECT 1 FROM bookings b
		      JOIN teacher_schedules ts2 ON ts2.id = b.schedule_id
		      WHERE b.status IN ('booked', 'rescheduled')
		        AND ts2.teacher_uuid = ts.teacher_uuid
		        AND ts2.day_of_week = ts.day_of_week
		        AND (ts2.start_time::time, ts2.end_time::time) OVERLAPS (ts.start_time::time, ts.end_time::time)
		  )
	`, booking.Schedule.TeacherUUID, booking.Schedule.DayOfWeek,
		booking.Schedule.StartTime, booking.Schedule.EndTime).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("gagal memperbarui status jadwal: %w", err)
	}

	// Upsert class history
	var history domain.ClassHistory
	err := tx.Where("booking_id = ?", booking.ID).First(&history).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := tx.Create(&domain.ClassHistory{
			BookingID: booking.ID,
			Status:    domain.StatusCancelled,
			Notes:     reason,
		}).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("gagal membuat riwayat kelas: %w", err)
		}
	} else if err == nil {
		history.Status = domain.StatusCancelled
		history.Notes = reason
		if err := tx.Save(&history).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("gagal update class history: %w", err)
		}
	} else {
		tx.Rollback()
		return err
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("gagal menyimpan pembatalan: %w", err)
	}

	// Fix trial instrument for notification
	if booking.PackageUsed.Package != nil && booking.PackageUsed.Package.Instrument == nil && booking.InstrumentID > 0 {
		var inst domain.Instrument
		if err := r.db.WithContext(ctx).Where("id = ?", booking.InstrumentID).First(&inst).Error; err == nil {
			booking.PackageUsed.Package.Instrument = &inst
		}
	}

	return nil
}

func (r *managerRepo) GetTeacherSchedules(ctx context.Context, teacherUUID string, requiredDuration int) ([]domain.TeacherSchedule, error) {
	query := r.db.WithContext(ctx).
		Where("teacher_uuid = ? AND deleted_at IS NULL", teacherUUID)

	if requiredDuration > 0 {
		query = query.Where("duration = ?", requiredDuration)
	}

	var schedules []domain.TeacherSchedule
	if err := query.Find(&schedules).Error; err != nil {
		return nil, err
	}

	return schedules, nil
}

func (r *managerRepo) GetAllTeachers(ctx context.Context, exceptTeacherUUID string) ([]domain.User, error) {
	var teachers []domain.User

	query := r.db.WithContext(ctx).
		Where("role = ? AND deleted_at IS NULL", domain.RoleTeacher).
		Preload("TeacherProfile.Instruments")

	if exceptTeacherUUID != "" {
		query = query.Where("uuid != ?", exceptTeacherUUID)
	}

	if err := query.Find(&teachers).Error; err != nil {
		return nil, err
	}
	return teachers, nil
}

func (r *managerRepo) GetCancelledClassHistories(ctx context.Context) (*[]domain.ClassHistory, error) {
	var histories []domain.ClassHistory

	err := r.db.WithContext(ctx).
		Preload("Booking").
		Preload("Booking.Student").
		Preload("Booking.Schedule").
		Preload("Booking.Schedule.Teacher").
		Preload("Booking.PackageUsed").
		Preload("Booking.PackageUsed.Package.Instrument").
		Preload("Booking.CancelUser").
		Joins("LEFT JOIN bookings ON class_histories.booking_id = bookings.id").
		Where("class_histories.status = ?", domain.StatusCancelled).
		Where("bookings.status = ?", domain.StatusCancelled).
		Order("bookings.class_date DESC").
		Find(&histories).Error

	if err != nil {
		return nil, fmt.Errorf("gagal mengambil riwayat kelas yang dibatalkan: %w", err)
	}

	return &histories, nil
}

func (r *managerRepo) RebookWithSubstitute(ctx context.Context, req domain.RebookInput) (*domain.Booking, error) {
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 1. Load and validate original booking — must be cancelled, not already rebooked
	var original domain.Booking
	if err := tx.
		Preload("PackageUsed").
		Preload("PackageUsed.Package").
		Preload("Schedule"). // needed to know the original teacher's uuid for step 3
		Where("id = ? AND status = ?", req.OriginalBookingID, domain.StatusCancelled).
		First(&original).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("booking yang dibatalkan tidak ditemukan atau sudah diproses ulang")
		}
		return nil, fmt.Errorf("error mencari booking: %w", err)
	}

	// 2. Validate substitute schedule exists and belongs to a teacher
	var subSchedule domain.TeacherSchedule
	if err := tx.
		Preload("Teacher").
		Where("id = ? AND deleted_at IS NULL", req.SubScheduleID).
		First(&subSchedule).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("jadwal guru pengganti tidak ditemukan")
		}
		return nil, fmt.Errorf("error mencari jadwal: %w", err)
	}

	// 3. Make sure the substitute is not the same teacher who cancelled
	if subSchedule.TeacherUUID == original.Schedule.TeacherUUID {
		tx.Rollback()
		return nil, errors.New("guru pengganti tidak boleh sama dengan guru yang membatalkan")
	}

	// 3b. Substitute schedule's duration must match the student's package duration —
	// same guard BookClass already enforces at normal booking time. Without this,
	// a 60-minute package can silently get paired with a 30-minute substitute slot
	// (or vice versa), shorting the student and creating a teacher-pay mismatch
	// since GenerateMonthlyPayments has no way to know the actual class length.
	if original.PackageUsed.Package != nil && subSchedule.Duration != original.PackageUsed.Package.Duration {
		tx.Rollback()
		return nil, fmt.Errorf(
			"durasi jadwal pengganti (%d menit) tidak sesuai dengan durasi paket siswa (%d menit)",
			subSchedule.Duration, original.PackageUsed.Package.Duration,
		)
	}

	// 4. Create new booking for the substitute
	newBooking := domain.Booking{
		StudentUUID:      original.StudentUUID,
		ScheduleID:       req.SubScheduleID,
		StudentPackageID: original.StudentPackageID,
		ClassDate:        req.ClassDate,
		InstrumentID:     original.InstrumentID, // Keep original instrument
		Status:           domain.StatusBooked,
		BookedAt:         time.Now(),
		IsManual:         true,
	}

	if err := tx.Create(&newBooking).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal membuat booking baru: %w", err)
	}

	// 5. Deduct quota back — it was refunded when the original teacher cancelled
	if err := tx.Model(&domain.StudentPackage{}).
		Where("id = ?", original.StudentPackageID).
		UpdateColumn("remaining_quota", gorm.Expr("remaining_quota - 1")).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal mengurangi kuota paket: %w", err)
	}

	// 6. Guard against quota going negative
	var pkg domain.StudentPackage
	if err := tx.Where("id = ?", original.StudentPackageID).First(&pkg).Error; err == nil {
		if pkg.RemainingQuota < 0 {
			tx.Rollback()
			return nil, errors.New("kuota paket siswa tidak mencukupi untuk pemesanan ulang ini")
		}
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("gagal menyimpan: %w", err)
	}

	// Reload with full relations for notification
	if err := r.db.WithContext(ctx).
		Preload("Student").
		Preload("Schedule").
		Preload("Schedule.Teacher").
		Preload("PackageUsed.Package.Instrument").
		First(&newBooking, newBooking.ID).Error; err != nil {
		return nil, fmt.Errorf("gagal memuat data booking: %w", err)
	}

	if newBooking.PackageUsed.Package != nil && newBooking.PackageUsed.Package.IsTrial {
		var instrument domain.Instrument
		if err := r.db.WithContext(ctx).Where("id = ?", newBooking.InstrumentID).First(&instrument).Error; err == nil {
			packageCopy := *newBooking.PackageUsed.Package
			packageCopy.TrialInstrument = instrument.Name

			if packageCopy.Instrument == nil {
				packageCopy.Instrument = &domain.Instrument{}
			}
			packageCopy.Instrument.Name = instrument.Name

			newBooking.PackageUsed.Package = &packageCopy
		}
	}

	return &newBooking, nil
}

func (r *managerRepo) GetSetting(ctx context.Context) (*domain.Setting, error) {
	var setting domain.Setting
	err := r.db.WithContext(ctx).First(&setting).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// If not found, create a default setting and return
			setting = domain.Setting{
				RegistrationFee: 50000,
			}
			errCreate := r.db.WithContext(ctx).Create(&setting).Error
			if errCreate != nil {
				return nil, errors.New(utils.TranslateDBError(errCreate))
			}
			return &setting, nil
		}
		return nil, errors.New(utils.TranslateDBError(err))
	}
	return &setting, nil
}

func (r *managerRepo) UpdateSetting(ctx context.Context, setting *domain.Setting) error {
	var existing domain.Setting
	err := r.db.WithContext(ctx).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Create it if it doesn't exist yet
			setting.ID = 1
			return r.db.WithContext(ctx).Create(setting).Error
		}
		return errors.New(utils.TranslateDBError(err))
	}

	setting.ID = existing.ID // preserve ID
	if err := r.db.WithContext(ctx).Save(setting).Error; err != nil {
		return errors.New(utils.TranslateDBError(err))
	}
	return nil
}

func (r *managerRepo) UpdateStudent(ctx context.Context, student *domain.User) error {
	if student.UUID == "" {
		return errors.New("uuid siswa tidak boleh kosong")
	}

	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Verify student exists
	var existing domain.User
	if err := tx.Where("uuid = ? AND role = ? AND deleted_at IS NULL", student.UUID, domain.RoleStudent).
		First(&existing).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("siswa tidak ditemukan")
		}
		return fmt.Errorf("error mencari siswa: %w", err)
	}

	// Check phone duplicate (only if phone is being updated)
	if student.Phone != "" && student.Phone != existing.Phone {
		var phoneCount int64
		if err := tx.Model(&domain.User{}).
			Where("phone = ? AND uuid != ?", student.Phone, student.UUID).
			Count(&phoneCount).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("error checking phone: %w", err)
		}
		if phoneCount > 0 {
			tx.Rollback()
			return errors.New("nomor telepon sudah digunakan oleh pengguna lain")
		}
	}

	// Check email duplicate (only if email is being updated)
	if student.Email != "" && student.Email != existing.Email {
		var emailCount int64
		if err := tx.Model(&domain.User{}).
			Where("email = ? AND uuid != ?", student.Email, student.UUID).
			Count(&emailCount).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("error checking email: %w", err)
		}
		if emailCount > 0 {
			tx.Rollback()
			return errors.New("email sudah digunakan oleh pengguna lain")
		}
	}

	// Build update map with only provided fields
	updates := map[string]interface{}{}

	if student.Name != "" {
		updates["name"] = student.Name
	}
	if student.Gender != "" {
		updates["gender"] = student.Gender
	}
	if student.Email != "" {
		updates["email"] = student.Email
	}
	if student.Phone != "" {
		updates["phone"] = student.Phone
	}
	if student.Image != nil {
		updates["image"] = student.Image
	}
	if student.Password != "" {
		updates["password"] = student.Password
	}

	if len(updates) == 0 {
		tx.Rollback()
		return errors.New("tidak ada data yang diperbarui")
	}

	if err := tx.Model(&domain.User{}).
		Where("uuid = ?", student.UUID).
		Updates(updates).Error; err != nil {
		tx.Rollback()
		return errors.New(utils.TranslateDBError(err))
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("gagal commit transaction: %w", err)
	}

	return nil
}

func (r *managerRepo) UpdateManager(ctx context.Context, payload *domain.User) error {
	// Mulai transaction
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Cek apakah user exists dan belum dihapus
	var existingUser domain.User
	err := tx.Where("uuid = ? AND role = ?", payload.UUID, domain.RoleManagement).First(&existingUser).Error
	if err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("manager tidak ditemukan")
		}
		return fmt.Errorf("error mencari manager: %w", err)
	}

	// Check email duplicate dengan user lain
	// var emailCount int64
	// err = tx.Model(&domain.User{}).
	// 	Where("email = ? AND uuid != ?", payload.Email, payload.UUID).
	// 	Count(&emailCount).Error
	// if err != nil {
	// 	tx.Rollback()
	// 	return fmt.Errorf("error checking email: %w", err)
	// }
	// if emailCount > 0 {
	// 	tx.Rollback()
	// 	return errors.New("email sudah digunakan oleh user lain")
	// }

	// Check phone duplicate dengan user lain
	var phoneCount int64
	err = tx.Model(&domain.User{}).
		Where("phone = ? AND uuid != ?", payload.Phone, payload.UUID).
		Count(&phoneCount).Error
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("error checking phone: %w", err)
	}
	if phoneCount > 0 {
		tx.Rollback()
		return errors.New("nomor telepon sudah digunakan oleh user lain")
	}

	// Update user data
	err = tx.Model(&domain.User{}).
		Where("uuid = ?", payload.UUID).
		Updates(payload).Error
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("gagal memperbarui data manager: %w", err)
	}

	// Commit transaction jika semua berhasil
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("gagal commit transaction: %w", err)
	}

	return nil
}

func (r *managerRepo) GetAllStudents(ctx context.Context) ([]domain.User, error) {
	var students []domain.User
	if err := r.db.WithContext(ctx).
		Preload("StudentProfile.Packages", "end_date >= ?", time.Now()).
		Preload("StudentProfile.Packages.Package.Instrument").
		Where("role = ? AND deleted_at IS NULL", domain.RoleStudent).
		Find(&students).Error; err != nil {
		return nil, err
	}
	return students, nil
}

func (r *managerRepo) GetStudentByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	var student domain.User
	if err := r.db.WithContext(ctx).
		Preload("StudentProfile.Packages", "end_date >= ?", time.Now()).
		Preload("StudentProfile.Packages.Package.Instrument").
		Where("uuid = ? AND role = ? AND deleted_at IS NULL", uuid, domain.RoleStudent).
		First(&student).Error; err != nil {
		return nil, err
	}
	return &student, nil
}

// CreateStudent inserts a new student
func (r *managerRepo) CreateStudent(ctx context.Context, user *domain.User) (*domain.User, error) {
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var existing domain.User
	if err := tx.
		Where("(email = ? OR phone = ?)", user.Email, user.Phone).
		First(&existing).Error; err == nil {
		tx.Rollback()
		return nil, errors.New("email atau nomor telepon sudah digunakan")
	}

	user.TeacherProfile = nil
	user.Role = domain.RoleStudent

	if err := tx.Create(user).Error; err != nil {
		tx.Rollback()
		return nil, errors.New(utils.TranslateDBError(err))
	}

	if user.UUID == "" {
		if err := tx.
			Where("email = ?", user.Email).
			First(user).Error; err != nil {
			tx.Rollback()
			return nil, errors.New("gagal mendapatkan UUID user")
		}
	}

	// Create StudentProfile
	studentProfile := domain.StudentProfile{UserUUID: user.UUID}
	if err := tx.Create(&studentProfile).Error; err != nil {
		tx.Rollback()
		return nil, errors.New(utils.TranslateDBError(err))
	}

	if err := tx.Commit().Error; err != nil {
		return nil, errors.New(utils.TranslateDBError(err))
	}

	return user, nil
}

func (r *managerRepo) ModifyStudentPackageQuota(ctx context.Context, studentUUID string, packageID int, incomingQuota int) (*domain.User, error) {
	if incomingQuota > 50 {
		return nil, fmt.Errorf("quota cannot exceed 50")
	}

	// First, find the student package directly
	var studentPackage domain.StudentPackage
	if err := r.db.WithContext(ctx).
		Preload("Package").
		Where("student_uuid = ? AND package_id = ? AND end_date >= ?", studentUUID, packageID, time.Now()).
		First(&studentPackage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("active package not found for this student")
		}
		return nil, err
	}

	// Verify the student exists and has the correct role
	var student domain.User
	if err := r.db.WithContext(ctx).
		Where("uuid = ? AND role = ? AND deleted_at IS NULL", studentUUID, domain.RoleStudent).
		First(&student).Error; err != nil {
		return nil, err
	}

	// Update the remaining quota
	studentPackage.RemainingQuota = incomingQuota

	// Ensure remaining quota doesn't go negative
	if studentPackage.RemainingQuota < 0 {
		studentPackage.RemainingQuota = 0
	}

	// Save the student package
	err := r.db.WithContext(ctx).Save(&studentPackage).Error
	if err != nil {
		return nil, err
	}

	// Now query the full student data with all relationships
	var fullStudent domain.User
	if err := r.db.WithContext(ctx).
		// Preload StudentProfile
		Preload("StudentProfile").
		// Preload StudentProfile's Packages with nested Package and Instrument
		Preload("StudentProfile.Packages", func(db *gorm.DB) *gorm.DB {
			return db.
				Preload("Package").
				Where("end_date >= ?", time.Now()) // Only active packages
		}).
		// Preload TeacherProfile (if needed, though student won't have one)
		Preload("TeacherProfile").
		// Main where clause
		Where("uuid = ? AND role = ? AND deleted_at IS NULL", studentUUID, domain.RoleStudent).
		First(&fullStudent).Error; err != nil {
		return nil, err
	}

	return &fullStudent, nil
}
