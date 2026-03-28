package repository

import (
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type studentRepository struct {
	db *gorm.DB
}

func NewStudentRepository(db *gorm.DB) domain.StudentRepository {
	return &studentRepository{db: db}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetTeacherDetails
// ─────────────────────────────────────────────────────────────────────────────

// GetAvailableSchedulesTrial returns all active teacher schedules for trial package browsing.
// Validations:
//   - packageID must belong to studentUUID and be a trial package with remaining quota > 0
//   - Returns all teacher schedules (no instrument/duration filter)
//   - Enriches with availability flags using the same ScheduleAvailabilityResult struct
func (r *studentRepository) GetAvailableSchedulesTrial(
	ctx context.Context,
	studentUUID string,
	packageID int,
) (*[]domain.ScheduleAvailabilityResult, error) {

	// ── 1. Validate: package must belong to student, be trial, have quota ─────
	var studentPkg domain.StudentPackage
	if err := r.db.WithContext(ctx).
		Joins("JOIN packages ON packages.id = student_packages.package_id").
		Preload("Package").
		Where("student_packages.id = ?", packageID).
		Where("student_packages.student_uuid = ?", studentUUID).
		Where("packages.is_trial = true").
		Where("student_packages.remaining_quota > 0").
		Where("student_packages.end_date > NOW()").
		First(&studentPkg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("paket trial tidak ditemukan, sudah habis, atau sudah kadaluarsa")
		}
		return nil, fmt.Errorf("gagal memvalidasi paket trial: %w", err)
	}

	// ── 2. Fetch ALL active teacher schedules (no instrument filter) ──────────
	var schedules []domain.TeacherSchedule
	if err := r.db.WithContext(ctx).
		Table("teacher_schedules").
		Joins("JOIN users ON users.uuid = teacher_schedules.teacher_uuid").
		Where("teacher_schedules.deleted_at IS NULL").
		Where("users.deleted_at IS NULL").
		Preload("Teacher").
		Preload("TeacherProfile.Instruments").
		Order("teacher_schedules.day_of_week ASC, teacher_schedules.start_time ASC").
		Find(&schedules).Error; err != nil {
		return nil, fmt.Errorf("gagal mengambil jadwal: %w", err)
	}

	// ── 3. Teacher finished-class counts (reuse existing helper) ─────────────
	teacherFinishedCounts, err := r.fetchTeacherFinishedClassCounts(ctx)
	if err != nil {
		teacherFinishedCounts = make(map[string]int)
	}

	// ── 4. Check which durations the student already used with this trial pkg ─
	// Business rule: 1×30min + 1×60min allowed. Block if already used that duration.
	var thirtyMinUsed, sixtyMinUsed int64
	r.db.WithContext(ctx).Model(&domain.Booking{}).
		Joins("JOIN teacher_schedules ts ON ts.id = bookings.schedule_id").
		Where("bookings.student_package_id = ?", packageID).
		Where("bookings.status IN ?", []string{domain.StatusBooked, domain.StatusCompleted}).
		Where("ts.duration = 30").Count(&thirtyMinUsed)
	r.db.WithContext(ctx).Model(&domain.Booking{}).
		Joins("JOIN teacher_schedules ts ON ts.id = bookings.schedule_id").
		Where("bookings.student_package_id = ?", packageID).
		Where("bookings.status IN ?", []string{domain.StatusBooked, domain.StatusCompleted}).
		Where("ts.duration = 60").Count(&sixtyMinUsed)

	// ── 5. Enrich each schedule ───────────────────────────────────────────────
	var results []domain.ScheduleAvailabilityResult

	for i := range schedules {
		sch := &schedules[i]

		result := domain.ScheduleAvailabilityResult{
			TeacherSchedule:           *sch,
			TeacherFinishedClassCount: teacherFinishedCounts[sch.TeacherUUID],
		}

		// 5a. Next class date
		startTimeParsed, _ := time.Parse("15:04", sch.StartTime)
		next := utils.GetNextClassDate(sch.DayOfWeek, startTimeParsed)
		result.NextClassDate = &next

		// 5b. IsDurationCompatible — trial rule: 30min slot ok if 30min quota not used, same for 60
		switch sch.Duration {
		case 30:
			result.IsDurationCompatible = ptrBool(thirtyMinUsed == 0)
		case 60:
			result.IsDurationCompatible = ptrBool(sixtyMinUsed == 0)
		default:
			result.IsDurationCompatible = ptrBool(false)
		}

		// 5c. IsRoomAvailable — we don't know the instrument yet (student picks at booking),
		//     so we can't do a precise drum/non-drum room check here.
		//     Mark as available; the actual check happens in BookClassTrial.
		result.IsRoomAvailable = ptrBool(!sch.IsBooked)

		// 5d. IsBookedSameDayAndTime
		var existingCount int64
		if err := r.db.WithContext(ctx).
			Model(&domain.Booking{}).
			Joins("JOIN teacher_schedules ts ON ts.id = bookings.schedule_id").
			Where("bookings.student_uuid = ?", studentUUID).
			Where("bookings.status IN ?", []string{domain.StatusBooked, domain.StatusRescheduled}).
			Where("bookings.class_date = ?", next).
			Where("ts.start_time = ?", sch.StartTime).
			Count(&existingCount).Error; err == nil {
			result.IsBookedSameDayAndTime = ptrBool(existingCount > 0)
		} else {
			result.IsBookedSameDayAndTime = ptrBool(false)
		}

		result.IsFullyAvailable = ptrBool(
			*result.IsRoomAvailable &&
				*result.IsDurationCompatible &&
				!*result.IsBookedSameDayAndTime &&
				!sch.IsBooked,
		)

		results = append(results, result)
	}

	return &results, nil
}

// BookClassTrial books a class using a trial package.
// The student explicitly provides instrumentID since the trial package has no fixed instrument.
func (r *studentRepository) BookClassTrial(
	ctx context.Context,
	studentUUID string,
	scheduleID int,
	packageID int,
	instrumentID int,
) (*domain.Booking, error) {
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// ── 1. Validate trial package ownership + quota ───────────────────────────
	var studentPkg domain.StudentPackage
	if err := tx.
		Joins("JOIN packages ON packages.id = student_packages.package_id").
		Preload("Package").
		Where("student_packages.id = ?", packageID).
		Where("student_packages.student_uuid = ?", studentUUID).
		Where("packages.is_trial = true").
		Where("student_packages.remaining_quota > 0").
		Where("student_packages.end_date > NOW()").
		First(&studentPkg).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("paket trial tidak ditemukan, sudah habis, atau sudah kadaluarsa")
		}
		return nil, fmt.Errorf("gagal memvalidasi paket trial: %w", err)
	}

	// ── 2. Load & validate schedule ───────────────────────────────────────────
	var schedule domain.TeacherSchedule
	if err := tx.
		Preload("Teacher").
		Preload("TeacherProfile.Instruments").
		Where("id = ? AND deleted_at IS NULL", scheduleID).
		First(&schedule).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("jadwal tidak ditemukan")
		}
		return nil, fmt.Errorf("gagal mengambil jadwal: %w", err)
	}

	if schedule.IsBooked {
		tx.Rollback()
		return nil, errors.New("jadwal ini sudah dipesan oleh siswa lain")
	}

	// ── 3. Validate teacher teaches the chosen instrument ─────────────────────
	teacherTeachesInstrument := false
	var bookedInstrumentName string
	var teacherInstrumentNames []string
	for _, inst := range schedule.TeacherProfile.Instruments {
		teacherInstrumentNames = append(teacherInstrumentNames, inst.Name)
		if inst.ID == instrumentID {
			teacherTeachesInstrument = true
			bookedInstrumentName = inst.Name
		}
	}
	if !teacherTeachesInstrument {
		tx.Rollback()
		return nil, fmt.Errorf(
			"guru ini tidak mengajar instrumen yang dipilih. Guru hanya mengajar: %s",
			strings.Join(teacherInstrumentNames, ", "),
		)
	}

	// ── 4. Trial duration quota check (1×30min + 1×60min) ────────────────────
	var durationCount int64
	if err := tx.Model(&domain.Booking{}).
		Joins("JOIN teacher_schedules ts ON ts.id = bookings.schedule_id").
		Where("bookings.student_package_id = ?", packageID).
		Where("bookings.status IN ?", []string{domain.StatusBooked, domain.StatusCompleted}).
		Where("ts.duration = ?", schedule.Duration).
		Count(&durationCount).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal memeriksa kuota trial: %w", err)
	}
	if durationCount >= 1 {
		tx.Rollback()
		return nil, fmt.Errorf(
			"kuota trial %d menit sudah digunakan. Setiap durasi hanya boleh 1 kali",
			schedule.Duration,
		)
	}

	// ── 5. Compute next class date + H-6 enforcement ──────────────────────────
	loc, err := time.LoadLocation("Asia/Makassar")
	if err != nil {
		loc = time.Local
	}
	now := time.Now().In(loc)

	startTimeParsed, _ := time.Parse("15:04", schedule.StartTime)
	classDate := utils.GetNextClassDate(schedule.DayOfWeek, startTimeParsed)

	classStartFull := time.Date(
		classDate.Year(), classDate.Month(), classDate.Day(),
		startTimeParsed.Hour(), startTimeParsed.Minute(), 0, 0, loc,
	)
	if classStartFull.Sub(now) < 6*time.Hour {
		tx.Rollback()
		return nil, fmt.Errorf(
			"pemesanan hanya bisa dilakukan minimal 6 jam sebelum kelas dimulai. Kelas ini dimulai pukul %s",
			schedule.StartTime,
		)
	}

	// ── 6. Room capacity check (now we know the instrument) ───────────────────
	isDrum := strings.EqualFold(bookedInstrumentName, "drum") ||
		strings.EqualFold(bookedInstrumentName, "drums")

	var bookingCount int64
	if err := r.countRoomUsage(tx, classDate, schedule.StartTime, instrumentID, isDrum, &bookingCount); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal memeriksa ketersediaan ruangan: %w", err)
	}

	limit := domain.RegularRoomLimit
	if isDrum {
		limit = domain.DrumRoomLimit
	}
	if bookingCount >= limit {
		tx.Rollback()
		return nil, errors.New("ruangan penuh untuk jam ini")
	}

	// ── 7. Student conflict check ─────────────────────────────────────────────
	var existingBookingCount int64
	if err := tx.Model(&domain.Booking{}).
		Joins("JOIN teacher_schedules ts ON ts.id = bookings.schedule_id").
		Where("bookings.student_uuid = ?", studentUUID).
		Where("bookings.status IN ?", []string{domain.StatusBooked, domain.StatusRescheduled}).
		Where("bookings.class_date = ?", classDate).
		Where("ts.start_time = ?", schedule.StartTime).
		Count(&existingBookingCount).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal memeriksa konflik jadwal: %w", err)
	}
	if existingBookingCount > 0 {
		tx.Rollback()
		return nil, fmt.Errorf(
			"kamu sudah memiliki kelas pada %s pukul %s",
			utils.GetDayName(classDate.Weekday()),
			schedule.StartTime,
		)
	}

	// ── 8. Create booking ─────────────────────────────────────────────────────
	newBooking := domain.Booking{
		StudentUUID:      studentUUID,
		ScheduleID:       schedule.ID,
		StudentPackageID: studentPkg.ID,
		InstrumentID:     instrumentID,
		ClassDate:        classDate,
		Status:           domain.StatusBooked,
		BookedAt:         time.Now(),
	}

	if err := tx.Create(&newBooking).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal membuat booking: %w", err)
	}

	// ── 9. Mark schedule as booked ────────────────────────────────────────────
	if err := tx.Model(&domain.TeacherSchedule{}).
		Where("id = ?", schedule.ID).
		Update("is_booked", true).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal memperbarui status jadwal: %w", err)
	}

	// ── 10. Deduct quota ──────────────────────────────────────────────────────
	if err := tx.Model(&domain.StudentPackage{}).
		Where("id = ?", studentPkg.ID).
		UpdateColumn("remaining_quota", gorm.Expr("remaining_quota - 1")).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal mengurangi kuota trial: %w", err)
	}

	// ── 11. Reload for notifications ──────────────────────────────────────────
	if err := tx.
		Preload("Student").
		Preload("Schedule.Teacher").
		Preload("PackageUsed.Package.Instrument").
		First(&newBooking, newBooking.ID).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal memuat data booking: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("gagal menyimpan booking: %w", err)
	}
	return &newBooking, nil
}

func (r *studentRepository) GetTeacherDetails(ctx context.Context, teacherUUID string) (*domain.User, error) {
	var teacher domain.User
	err := r.db.WithContext(ctx).
		Preload("TeacherProfile").
		Preload("TeacherProfile.Instruments").
		Preload("TeacherProfile.Album").
		Where("uuid = ? AND role = ?", teacherUUID, domain.RoleTeacher).
		First(&teacher).Error
	if err != nil {
		return nil, err
	}
	return &teacher, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// GetMyClassHistory
// ─────────────────────────────────────────────────────────────────────────────

func (r *studentRepository) GetMyClassHistory(ctx context.Context, studentUUID string, f domain.PaginationFilter) (*[]domain.ClassHistory, error) {
	var histories []domain.ClassHistory

	q := r.db.WithContext(ctx).
		Preload("Booking").
		Preload("Booking.Schedule").
		Preload("Booking.Schedule.Teacher").
		Preload("Booking.Schedule.TeacherProfile").
		Preload("Booking.Schedule.TeacherProfile.Instruments").
		Preload("Booking.Student").
		Preload("Booking.PackageUsed").
		Preload("Booking.PackageUsed.Package").
		Preload("Booking.PackageUsed.Package.Instrument").
		Preload("Documentations").
		Joins("LEFT JOIN bookings ON class_histories.booking_id = bookings.id").
		Where("bookings.student_uuid = ?", studentUUID).
		Order("bookings.class_date DESC")

	if !f.IsAll() {
		q = q.Limit(f.SafeLimit()).Offset(f.Offset())
	}

	if err := q.Find(&histories).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch class history: %w", err)
	}
	return &histories, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// CancelBookedClass  (unchanged logic — H-1/24h rule kept)
// ─────────────────────────────────────────────────────────────────────────────

func (r *studentRepository) CancelBookedClass(
	ctx context.Context,
	bookingID int,
	studentUUID string,
	reason *string,
) (*domain.Booking, error) {
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var booking domain.Booking
	if err := tx.Preload("Schedule").
		Preload("Schedule.Teacher").
		Preload("Student").
		Preload("PackageUsed").
		Preload("PackageUsed.Package").
		Preload("PackageUsed.Package.Instrument").
		Preload("CancelUser").
		Where("id = ? AND status = ?", bookingID, domain.StatusBooked).
		First(&booking).Error; err != nil {
		tx.Rollback()
		return nil, errors.New("booking tidak ditemukan atau sudah dibatalkan")
	}

	if booking.StudentUUID != studentUUID {
		tx.Rollback()
		return nil, errors.New("anda tidak memiliki akses ke booking ini")
	}

	loc, _ := time.LoadLocation("Asia/Makassar")
	now := time.Now().In(loc)
	classDate := booking.ClassDate.In(loc)

	if classDate.Before(now) {
		tx.Rollback()
		return nil, errors.New("tidak bisa membatalkan kelas yang sudah lewat")
	}

	// H-1 / 24-hour cancellation window
	if now.After(classDate.Add(-24 * time.Hour)) {
		tx.Rollback()
		return nil, errors.New("pembatalan hanya bisa dilakukan minimal H-1 (24 jam) sebelum kelas")
	}

	cancelTime := time.Now()

	if err := tx.Model(&booking).
		UpdateColumns(map[string]interface{}{
			"status":       domain.StatusCancelled,
			"cancelled_at": cancelTime,
			"canceled_by":  studentUUID,
			"notes":        reason,
		}).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal membatalkan booking: %w", err)
	}

	// Refund quota (skip for trial packages)
	if !booking.PackageUsed.Package.IsTrial {
		if err := tx.Model(&domain.StudentPackage{}).
			Where("id = ?", booking.StudentPackageID).
			Update("remaining_quota", gorm.Expr("remaining_quota + 1")).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("gagal refund quota: %w", err)
		}
	}

	if err := tx.Model(&domain.TeacherSchedule{}).
		Where("id = ?", booking.ScheduleID).
		Update("is_booked", false).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal memperbarui jadwal pengajar: %w", err)
	}

	// Upsert class history
	var history domain.ClassHistory
	err := tx.Where("booking_id = ?", booking.ID).First(&history).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := tx.Create(&domain.ClassHistory{BookingID: booking.ID, Status: domain.StatusCancelled, Notes: reason}).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("gagal membuat riwayat kelas (cancel): %w", err)
		}
	} else if err == nil {
		history.Status = domain.StatusCancelled
		history.Notes = reason
		if err := tx.Save(&history).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("gagal update class history: %w", err)
		}
	} else {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("gagal menyimpan pembatalan: %w", err)
	}
	return &booking, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// BookClass
//
// Changes vs old version:
//   - Accepts explicit packageID so the caller (and service) picks the package.
//   - Accepts explicit instrumentID — critical for trial packages where the package
//     instrument is irrelevant and the student picks which instrument to study.
//   - Trial packages bypass instrument + duration matching; they can book ANY schedule.
//   - H-6 hour booking window enforced.
//   - IsBooked flag on TeacherSchedule is NOT used as a "room full" gate anymore;
//     it is a weekly-recurrence flag. Room availability is checked by date+time count.
// ─────────────────────────────────────────────────────────────────────────────

func (r *studentRepository) BookClass(
	ctx context.Context,
	studentUUID string,
	scheduleID int,
	instrumentID int,
) (*domain.Booking, error) {
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// ── 1. Load & validate schedule ──────────────────────────────────────────
	var schedule domain.TeacherSchedule
	if err := tx.
		Preload("Teacher").
		Preload("TeacherProfile.Instruments").
		Where("id = ? AND deleted_at IS NULL", scheduleID).
		First(&schedule).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("jadwal tidak ditemukan")
		}
		return nil, fmt.Errorf("gagal mengambil jadwal: %w", err)
	}

	// ── 1b. Check if schedule slot is already taken ───────────────────────────
	if schedule.IsBooked {
		tx.Rollback()
		return nil, errors.New("jadwal ini sudah dipesan oleh siswa lain. Silakan pilih jadwal lain")
	}

	// ── 2. Verify teacher teaches the requested instrument ───────────────────
	teacherTeachesInstrument := false
	var bookedInstrumentName string
	var teacherInstrumentNames []string
	for _, inst := range schedule.TeacherProfile.Instruments {
		teacherInstrumentNames = append(teacherInstrumentNames, inst.Name)
		if inst.ID == instrumentID {
			teacherTeachesInstrument = true
			bookedInstrumentName = inst.Name
		}
	}
	if !teacherTeachesInstrument {
		tx.Rollback()
		return nil, fmt.Errorf(
			"guru ini tidak mengajar instrumen yang dipilih. Guru hanya mengajar: %s",
			strings.Join(teacherInstrumentNames, ", "),
		)
	}

	// ── 3. Auto-select best package ───────────────────────────────────────────
	// Strategy: non-trial, matching instrument + duration, has quota, not expired.
	// Tie-break: closest expiry first (use-it-before-you-lose-it).
	var studentPackage domain.StudentPackage
	if err := tx.
		Joins("JOIN packages ON packages.id = student_packages.package_id").
		Preload("Package").
		Preload("Package.Instrument").
		Where("student_packages.student_uuid = ?", studentUUID).
		Where("packages.instrument_id = ?", instrumentID).
		Where("packages.is_trial = false").
		Where("packages.duration = ?", schedule.Duration).
		Where("student_packages.remaining_quota > 0").
		Where("student_packages.end_date > NOW()").
		Order("student_packages.end_date ASC").
		First(&studentPackage).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf(
				"tidak ada paket aktif untuk instrumen ini dengan durasi %d menit. Silakan beli paket terlebih dahulu",
				schedule.Duration,
			)
		}
		return nil, fmt.Errorf("gagal mencari paket: %w", err)
	}

	// ── 4. Compute next class date (reuse utils.GetNextClassDate) ────────────
	loc, err := time.LoadLocation("Asia/Makassar")
	if err != nil {
		loc = time.Local
	}
	now := time.Now().In(loc)

	startTimeParsed, _ := time.Parse("15:04", schedule.StartTime)
	classDate := utils.GetNextClassDate(schedule.DayOfWeek, startTimeParsed)

	// ── 5. H-6 enforcement ────────────────────────────────────────────────────
	classStartFull := time.Date(
		classDate.Year(), classDate.Month(), classDate.Day(),
		startTimeParsed.Hour(), startTimeParsed.Minute(), 0, 0, loc,
	)
	if classStartFull.Sub(now) < 6*time.Hour {
		tx.Rollback()
		return nil, fmt.Errorf(
			"pemesanan hanya bisa dilakukan minimal 6 jam sebelum kelas dimulai. Kelas ini dimulai pukul %s",
			schedule.StartTime,
		)
	}

	// ── 6. Room capacity check ────────────────────────────────────────────────
	isDrum := strings.EqualFold(bookedInstrumentName, "drum") ||
		strings.EqualFold(bookedInstrumentName, "drums")

	var bookingCount int64
	if err := r.countRoomUsage(tx, classDate, schedule.StartTime, instrumentID, isDrum, &bookingCount); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal memeriksa ketersediaan ruangan: %w", err)
	}

	limit := domain.RegularRoomLimit
	if isDrum {
		limit = domain.DrumRoomLimit
	}
	if bookingCount >= limit {
		tx.Rollback()
		return nil, errors.New("ruangan penuh untuk jam ini")
	}

	// ── 7. Student conflict check ─────────────────────────────────────────────
	var existingBookingCount int64
	if err := tx.Model(&domain.Booking{}).
		Joins("JOIN teacher_schedules ts ON ts.id = bookings.schedule_id").
		Where("bookings.student_uuid = ?", studentUUID).
		Where("bookings.status IN ?", []string{domain.StatusBooked, domain.StatusRescheduled}).
		Where("bookings.class_date = ?", classDate).
		Where("ts.start_time = ?", schedule.StartTime).
		Count(&existingBookingCount).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal memeriksa konflik jadwal: %w", err)
	}
	if existingBookingCount > 0 {
		tx.Rollback()
		return nil, fmt.Errorf(
			"kamu sudah memiliki kelas pada %s pukul %s. Silakan pilih waktu lain",
			utils.GetDayName(classDate.Weekday()),
			schedule.StartTime,
		)
	}

	// ── 8. Create booking ─────────────────────────────────────────────────────
	newBooking := domain.Booking{
		StudentUUID:      studentUUID,
		ScheduleID:       schedule.ID,
		StudentPackageID: studentPackage.ID,
		InstrumentID:     instrumentID,
		ClassDate:        classDate,
		Status:           domain.StatusBooked,
		BookedAt:         time.Now(),
	}

	if err := tx.Create(&newBooking).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal membuat booking: %w", err)
	}

	// ── 9. Mark schedule as booked ────────────────────────────────────────────
	if err := tx.Model(&domain.TeacherSchedule{}).
		Where("id = ?", schedule.ID).
		Update("is_booked", true).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal memperbarui status jadwal: %w", err)
	}

	// ── 10. Deduct quota from the auto-selected package ───────────────────────
	if err := tx.Model(&domain.StudentPackage{}).
		Where("id = ?", studentPackage.ID).
		UpdateColumn("remaining_quota", gorm.Expr("remaining_quota - 1")).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal mengurangi kuota paket: %w", err)
	}

	// ── 11. Reload with relations for notifications ───────────────────────────
	if err := tx.
		Preload("Student").
		Preload("Schedule.Teacher").
		Preload("PackageUsed.Package.Instrument").
		First(&newBooking, newBooking.ID).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal memuat data booking: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("gagal menyimpan booking: %w", err)
	}
	return &newBooking, nil
}

// countRoomUsage counts active bookings for a given date/time slot by instrument category.
// This unified helper works correctly for both trial and regular package bookings because
// it counts by the actual *booked instrument* stored in bookings.instrument_id.
func (r *studentRepository) countRoomUsage(
	db *gorm.DB,
	classDate time.Time,
	startTime string,
	instrumentID int,
	isDrum bool,
	out *int64,
) error {
	// Determine instrument category via a subquery so we do not rely on package instrument.
	// bookings.instrument_id is the source of truth post-refactor.
	subquery := `
		SELECT COUNT(b.id)
		FROM bookings b
		JOIN teacher_schedules ts ON ts.id = b.schedule_id
		JOIN instruments i ON i.id = b.instrument_id
		WHERE b.status IN ('booked', 'rescheduled')
		  AND b.class_date = ?
		  AND ts.start_time = ?
	`
	if isDrum {
		subquery += " AND i.name ILIKE 'Drum%'"
	} else {
		subquery += " AND NOT (i.name ILIKE 'Drum%')"
	}

	return db.Raw(subquery, classDate, startTime).Scan(out).Error
}

// ─────────────────────────────────────────────────────────────────────────────
// GetMyBookedClasses
// ─────────────────────────────────────────────────────────────────────────────

func (r *studentRepository) GetMyBookedClasses(ctx context.Context, studentUUID string, f domain.PaginationFilter) (*[]domain.Booking, error) {
	var bookings []domain.Booking

	q := r.db.WithContext(ctx).
		Where("student_uuid = ? AND status IN ?", studentUUID, []string{domain.StatusBooked, domain.StatusRescheduled}).
		Preload("Schedule").
		Preload("PackageUsed").
		Preload("PackageUsed.Package").
		Preload("PackageUsed.Package.Instrument").
		Preload("Schedule.Teacher").
		Preload("Schedule.TeacherProfile.Instruments").
		Order("class_date ASC, booked_at DESC")

	if !f.IsAll() {
		q = q.Limit(f.SafeLimit()).Offset(f.Offset())
	}

	if err := q.Find(&bookings).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch booked classes: %w", err)
	}

	loc, _ := time.LoadLocation("Asia/Makassar")
	now := time.Now().In(loc)
	for i := range bookings {
		startTimeStr := bookings[i].Schedule.StartTime
		parsedStart, _ := time.Parse("15:04", startTimeStr)

		classDateLoc := bookings[i].ClassDate.In(loc)
		classStart := time.Date(
			classDateLoc.Year(), classDateLoc.Month(), classDateLoc.Day(),
			parsedStart.Hour(), parsedStart.Minute(), 0, 0, loc,
		)
		classEnd := classStart.Add(time.Duration(bookings[i].Schedule.Duration) * time.Minute)

		switch {
		case now.Before(classStart):
			bookings[i].Status = domain.StatusUpcoming
		case (now.Equal(classStart) || now.After(classStart)) && now.Before(classEnd):
			bookings[i].Status = domain.StatusOngoing
		}
		if now.Equal(classEnd) || now.After(classEnd) {
			bookings[i].IsReadyToFinish = true
			bookings[i].Status = domain.StatusClassFinished
		}
	}

	return &bookings, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// GetAvailableSchedules
//
// Key changes:
//   - Accepts packageID: loads the chosen package to determine trial status,
//     instrument, and duration constraints.
//   - Trial packages: return ALL active teacher schedules regardless of instrument/duration.
//   - Regular packages: filter by instrument and duration as before.
//   - Adds TeacherFinishedClassCount to each result so the frontend can sort/filter
//     teachers by performance.
//   - Returns []ScheduleAvailabilityResult (not *[]TeacherSchedule).
// ─────────────────────────────────────────────────────────────────────────────

func (r *studentRepository) GetAvailableSchedules(
	ctx context.Context,
	studentUUID string,
	instrumentID int,
) (*[]domain.ScheduleAvailabilityResult, error) {

	loc, _ := time.LoadLocation("Asia/Makassar")
	now := time.Now().In(loc)

	// ── 1. Fetch candidate schedules ─────────────────────────────────────────
	var schedules []domain.TeacherSchedule
	if err := r.db.WithContext(ctx).
		Distinct("teacher_schedules.*").
		Table("teacher_schedules").
		Joins("JOIN teacher_profiles ON teacher_profiles.user_uuid = teacher_schedules.teacher_uuid").
		Joins("JOIN teacher_instruments ON teacher_instruments.teacher_profile_user_uuid = teacher_profiles.user_uuid").
		Joins("JOIN users ON users.uuid = teacher_schedules.teacher_uuid").
		Where("teacher_instruments.instrument_id = ?", instrumentID).
		Where("teacher_schedules.deleted_at IS NULL").
		Where("users.deleted_at IS NULL").
		Preload("Teacher").
		Preload("TeacherProfile.Instruments").
		Order("teacher_schedules.day_of_week ASC, teacher_schedules.start_time ASC").
		Find(&schedules).Error; err != nil {
		return nil, fmt.Errorf("gagal mengambil jadwal: %w", err)
	}

	// ── 2. Load student's active packages for this instrument ─────────────────
	type pkgRow struct {
		Duration int `gorm:"column:duration"`
		ID       int `gorm:"column:id"`
	}
	var activePkgs []pkgRow

	if err := r.db.WithContext(ctx).Raw(`
		SELECT p.duration AS duration, sp.id AS id
		FROM student_packages sp
		JOIN packages p ON p.id = sp.package_id
		WHERE sp.student_uuid = ?
		  AND p.instrument_id = ?
		  AND p.is_trial = false
		  AND sp.remaining_quota > 0
		  AND sp.end_date AT TIME ZONE 'Asia/Makassar' >= ?
	`, studentUUID, instrumentID, now).Scan(&activePkgs).Error; err != nil {
		return nil, fmt.Errorf("gagal memuat paket siswa: %w", err)
	}

	fmt.Printf("[DEBUG] studentUUID=%s instrumentID=%d activePkgs=%+v\n",
		studentUUID, instrumentID, activePkgs)

	compatibleDurations := make(map[int]bool, 2)
	for _, p := range activePkgs {
		compatibleDurations[p.Duration] = true
	}

	// ── 3. Instrument category for room-capacity checks ──────────────────────
	var instrument domain.Instrument
	if err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", instrumentID).
		First(&instrument).Error; err != nil {
		return nil, fmt.Errorf("instrumen tidak ditemukan: %w", err)
	}

	isDrum := strings.EqualFold(instrument.Name, "drum") ||
		strings.EqualFold(instrument.Name, "drums")

	roomLimit := domain.RegularRoomLimit
	if isDrum {
		roomLimit = domain.DrumRoomLimit
	}

	// ── 4. Teacher finished-class counts ─────────────────────────────────────
	teacherFinishedCounts, err := r.fetchTeacherFinishedClassCounts(ctx)
	if err != nil {
		teacherFinishedCounts = make(map[string]int)
	}

	// ── 5. Enrich each schedule ───────────────────────────────────────────────
	var results []domain.ScheduleAvailabilityResult

	for i := range schedules {
		sch := &schedules[i]

		result := domain.ScheduleAvailabilityResult{
			TeacherSchedule:           *sch,
			TeacherFinishedClassCount: teacherFinishedCounts[sch.TeacherUUID],
		}

		// 5a. Next class date
		startTimeParsed, _ := time.Parse("15:04", sch.StartTime)
		next := utils.GetNextClassDate(sch.DayOfWeek, startTimeParsed)
		result.NextClassDate = &next

		// 5b. IsDurationCompatible
		result.IsDurationCompatible = ptrBool(compatibleDurations[sch.Duration])

		// 5c. IsRoomAvailable
		var bookingCount int64
		roomCountErr := r.countRoomUsage(
			r.db.WithContext(ctx),
			next, sch.StartTime,
			instrumentID, isDrum,
			&bookingCount,
		)
		if roomCountErr != nil {
			result.IsRoomAvailable = ptrBool(false)
		} else {
			result.IsRoomAvailable = ptrBool(bookingCount < roomLimit)
		}

		// 5d. IsBookedSameDayAndTime
		var existingCount int64
		if err := r.db.WithContext(ctx).
			Model(&domain.Booking{}).
			Joins("JOIN teacher_schedules ts ON ts.id = bookings.schedule_id").
			Where("bookings.student_uuid = ?", studentUUID).
			Where("bookings.status IN ?", []string{domain.StatusBooked, domain.StatusRescheduled}).
			Where("bookings.class_date = ?", next).
			Where("ts.start_time = ?", sch.StartTime).
			Count(&existingCount).Error; err == nil {
			result.IsBookedSameDayAndTime = ptrBool(existingCount > 0)
		} else {
			result.IsBookedSameDayAndTime = ptrBool(false)
		}

		// 6d. iscancelablefromnow
		result.IsCancelAbleFromNow = ptrBool(utils.CheckCancelAbleFromNow(next))

		result.IsFullyAvailable = ptrBool(
			*result.IsRoomAvailable &&
				*result.IsDurationCompatible &&
				!*result.IsBookedSameDayAndTime &&
				!sch.IsBooked,
		)

		results = append(results, result)
	}

	return &results, nil
}

// fetchTeacherFinishedClassCounts returns a map of teacherUUID → number of completed classes.
// A single aggregation query avoids N+1 problems.
func (r *studentRepository) fetchTeacherFinishedClassCounts(ctx context.Context) (map[string]int, error) {
	type row struct {
		TeacherUUID string
		Count       int
	}
	var rows []row

	err := r.db.WithContext(ctx).
		Table("class_histories ch").
		Select("ts.teacher_uuid AS teacher_uuid, COUNT(ch.id) AS count").
		Joins("JOIN bookings b ON b.id = ch.booking_id").
		Joins("JOIN teacher_schedules ts ON ts.id = b.schedule_id").
		Where("ch.status = ?", domain.StatusCompleted).
		Group("ts.teacher_uuid").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]int, len(rows))
	for _, r := range rows {
		result[r.TeacherUUID] = r.Count
	}
	return result, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func ptrBool(v bool) *bool { return &v }

func (r *studentRepository) GetStudentInstrumentIDs(ctx context.Context, studentUUID string) ([]int, error) {
	var ids []int
	err := r.db.WithContext(ctx).
		Table("student_packages").
		Select("packages.instrument_id").
		Joins("JOIN packages ON packages.id = student_packages.package_id").
		Where("student_packages.student_uuid = ?", studentUUID).
		Where("student_packages.end_date >= ? AND student_packages.remaining_quota > 0", time.Now()).
		Scan(&ids).Error
	return ids, err
}

func (r *studentRepository) GetTeacherSchedulesBasedOnInstrumentIDs(ctx context.Context, instrumentIDs []int) (*[]domain.TeacherSchedule, error) {
	var schedules []domain.TeacherSchedule
	err := r.db.WithContext(ctx).
		Distinct("teacher_schedules.*").
		Table("teacher_schedules").
		Joins("JOIN teacher_profiles ON teacher_profiles.user_uuid = teacher_schedules.teacher_uuid").
		Joins("JOIN teacher_instruments ON teacher_instruments.teacher_profile_user_uuid = teacher_profiles.user_uuid").
		Joins("JOIN users ON users.uuid = teacher_schedules.teacher_uuid").
		Where("teacher_instruments.instrument_id IN ?", instrumentIDs).
		Where("teacher_schedules.deleted_at IS NULL").
		Where("users.deleted_at IS NULL").
		Preload("Teacher").
		Preload("TeacherProfile.Instruments").
		Order("teacher_schedules.day_of_week ASC, teacher_schedules.start_time ASC").
		Find(&schedules).Error
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil jadwal: %w", err)
	}
	return &schedules, nil
}

func (r *studentRepository) GetAllAvailablePackages(ctx context.Context, studentUUID *string) (*[]domain.Package, *domain.Setting, error) {
	var packages []domain.Package
	var setting domain.Setting

	if err := r.db.WithContext(ctx).First(&setting).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, err
		}
		// No settings row yet (fresh DB) — proceed with zero-value setting.
	}

	// Hide teacher commission from public endpoint
	setting.TeacherCommission = 0

	// Determine whether to exclude trial packages and whether to zero the registration fee.
	// Both checks share a single student context query block to avoid duplicate DB round-trips.
	excludeTrial := false

	if studentUUID != nil {
		fmt.Println("masuk sini coy")
		// Count how many trial packages the student has ever purchased (paid).
		var trialPurchaseCount int64
		if err := r.db.WithContext(ctx).
			Table("payments").
			Joins("JOIN packages ON packages.id = payments.package_id").
			Where("payments.student_uuid = ?", *studentUUID).
			Where("payments.status = ?", domain.PaymentStatusPaid).
			Where("packages.is_trial = true").
			Count(&trialPurchaseCount).Error; err != nil {
			return nil, nil, fmt.Errorf("gagal memeriksa riwayat paket trial: %w", err)
		}

		if trialPurchaseCount > 0 {
			excludeTrial = true
		}

		// Count non-trial paid purchases to determine registration fee waiver.
		var priorPaidCount int64
		if err := r.db.WithContext(ctx).
			Table("payments").
			Joins("JOIN packages ON packages.id = payments.package_id").
			Where("payments.student_uuid = ?", *studentUUID).
			Where("payments.status = ?", domain.PaymentStatusPaid).
			Where("packages.is_trial = false").
			Count(&priorPaidCount).Error; err != nil {
			return nil, nil, fmt.Errorf("gagal memeriksa riwayat pembayaran: %w", err)
		}

		// Temporary debug — remove after confirming
		fmt.Printf("[DEBUG] studentUUID=%s trialPurchaseCount=%d priorPaidCount=%d\n",
			*studentUUID, trialPurchaseCount, priorPaidCount)

		if priorPaidCount > 0 {
			setting.RegistrationFee = 0
		}
	}

	// Build package query — conditionally exclude trial packages.
	query := r.db.WithContext(ctx).Preload("Instrument")
	if excludeTrial {
		query = query.Where("is_trial = false")
	}

	if err := query.Find(&packages).Error; err != nil {
		return nil, nil, err
	}

	return &packages, &setting, nil
}

func (r *studentRepository) GetMyProfile(ctx context.Context, userUUID string) (*domain.User, error) {
	var student domain.User
	err := r.db.WithContext(ctx).
		Preload("StudentProfile.Packages", "end_date >= ? AND remaining_quota > 0", time.Now()).
		Preload("StudentProfile.Packages.Package.Instrument").
		Where("uuid = ? AND role = ? AND deleted_at IS NULL", userUUID, domain.RoleStudent).
		First(&student).Error
	if err != nil {
		return nil, err
	}
	return &student, nil
}

func (r *studentRepository) UpdateStudentData(ctx context.Context, uuid string, payload domain.User) error {
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	var existingUser domain.User
	if err := tx.Where("uuid = ? AND role = ? AND deleted_at IS NULL", uuid, domain.RoleStudent).First(&existingUser).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("pengguna tidak ditemukan")
		}
		return fmt.Errorf("error mencari pengguna: %w", err)
	}

	var phoneCount int64
	if err := tx.Model(&domain.User{}).
		Where("phone = ? AND uuid != ?", payload.Phone, uuid).
		Count(&phoneCount).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("error checking phone: %w", err)
	}
	if phoneCount > 0 {
		tx.Rollback()
		return errors.New("nomor telepon sudah digunakan oleh pengguna lain")
	}

	if err := tx.Model(&domain.User{}).Where("uuid = ?", uuid).Updates(payload).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("gagal memperbarui data pengguna: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("gagal commit transaction: %w", err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// GetTeacherSchedulesForPackage
// Returns a teacher's schedules whose duration matches the given package.
// ─────────────────────────────────────────────────────────────────────────────

func (r *studentRepository) GetTeacherSchedulesForPackage(
	ctx context.Context,
	teacherUUID string,
	studentPackageID int,
	studentUUID string,
) (*[]domain.TeacherSchedule, error) {
	// Load the student package to get the session duration.
	var sp domain.StudentPackage
	if err := r.db.WithContext(ctx).
		Joins("JOIN packages ON packages.id = student_packages.package_id").
		Preload("Package").
		Where("student_packages.id = ? AND student_packages.student_uuid = ?", studentPackageID, studentUUID).
		First(&sp).Error; err != nil {
		return nil, errors.New("paket tidak ditemukan atau bukan milik Anda")
	}

	var schedules []domain.TeacherSchedule
	if err := r.db.WithContext(ctx).
		Where("teacher_uuid = ? AND duration = ? AND deleted_at IS NULL", teacherUUID, sp.Package.Duration).
		Preload("Teacher").
		Order("day_of_week ASC, start_time ASC").
		Find(&schedules).Error; err != nil {
		return nil, fmt.Errorf("gagal mengambil jadwal guru: %w", err)
	}

	return &schedules, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// generateBulkCandidates — shared engine for preview and actual booking.
//
// Algorithm (min-heap / earliest-first):
//  1. For each selected schedule, compute its "next valid date" (H-6 aware).
//  2. Always pick the schedule whose next date is earliest.
//  3. Check room capacity (DB + already-pending slots).
//  4. Check student self-conflict (DB + already-pending slots).
//  5. If a slot is blocked, advance that schedule by +7 days and retry.
//  6. If a slot is valid, record it and advance by +7 days.
//  7. Repeat until quota filled or max-attempts exhausted.
// ─────────────────────────────────────────────────────────────────────────────

type bulkCandidate struct {
	ScheduleID int
	DayOfWeek  string
	StartTime  string
	EndTime    string
	ClassDate  time.Time
}

func (r *studentRepository) generateBulkCandidates(
	ctx context.Context,
	studentUUID string,
	sp domain.StudentPackage,
	schedules []domain.TeacherSchedule,
	instrumentID int,
	isDrum bool,
) ([]bulkCandidate, error) {
	loc, _ := time.LoadLocation("Asia/Makassar")
	now := time.Now().In(loc)
	quota := sp.RemainingQuota

	// Initialise one cursor per selected schedule.
	type cursor struct {
		sch  domain.TeacherSchedule
		next time.Time
	}
	cursors := make([]cursor, len(schedules))
	for i, sch := range schedules {
		parsed, _ := time.Parse("15:04", sch.StartTime)
		cursors[i] = cursor{sch: sch, next: utils.GetNextClassDate(sch.DayOfWeek, parsed)}
	}

	var candidates []bulkCandidate
	maxAttempts := quota * 52 // up to ~1 year of weekly retries per slot

	for len(candidates) < quota && maxAttempts > 0 {
		maxAttempts--

		// Pick the cursor whose next date is earliest.
		minIdx := 0
		for i := 1; i < len(cursors); i++ {
			if cursors[i].next.Before(cursors[minIdx].next) {
				minIdx = i
			}
		}
		c := &cursors[minIdx]

		// ── Guard: date must be before package expiry ──────────────────────
		if c.next.After(sp.EndDate) {
			return nil, fmt.Errorf(
				"tidak dapat menjadwalkan semua %d sesi sebelum paket berakhir pada %s. "+
					"Hanya %d sesi yang dapat dijadwalkan dalam periode tersebut",
				quota, sp.EndDate.Format("02 Jan 2006"), len(candidates),
			)
		}

		// ── Guard: H-6 rule — class must start ≥ 6 h from now ─────────────
		startParsed, _ := time.Parse("15:04", c.sch.StartTime)
		classStart := time.Date(
			c.next.Year(), c.next.Month(), c.next.Day(),
			startParsed.Hour(), startParsed.Minute(), 0, 0, loc,
		)
		if classStart.Sub(now) < 6*time.Hour {
			c.next = c.next.AddDate(0, 0, 7)
			continue
		}

		// ── Guard: room capacity (DB bookings + already pending) ───────────
		var dbRoomCount int64
		_ = r.countRoomUsage(r.db.WithContext(ctx), c.next, c.sch.StartTime, instrumentID, isDrum, &dbRoomCount)

		var pendingRoomCount int64
		for _, cand := range candidates {
			if cand.ClassDate.Equal(c.next) && cand.StartTime == c.sch.StartTime {
				pendingRoomCount++
			}
		}
		roomLimit := domain.RegularRoomLimit
		if isDrum {
			roomLimit = domain.DrumRoomLimit
		}
		if dbRoomCount+pendingRoomCount >= roomLimit {
			c.next = c.next.AddDate(0, 0, 7)
			continue
		}

		// ── Guard: student self-conflict (DB) ──────────────────────────────
		var dbConflict int64
		r.db.WithContext(ctx).Model(&domain.Booking{}).
			Joins("JOIN teacher_schedules ts ON ts.id = bookings.schedule_id").
			Where("bookings.student_uuid = ?", studentUUID).
			Where("bookings.status IN ?", []string{domain.StatusBooked, domain.StatusRescheduled}).
			Where("bookings.class_date = ?", c.next).
			Where("ts.start_time = ?", c.sch.StartTime).
			Count(&dbConflict)

		// ── Guard: student self-conflict (pending list) ────────────────────
		var pendingConflict bool
		for _, cand := range candidates {
			if cand.ClassDate.Equal(c.next) && cand.StartTime == c.sch.StartTime {
				pendingConflict = true
				break
			}
		}

		if dbConflict > 0 || pendingConflict {
			c.next = c.next.AddDate(0, 0, 7)
			continue
		}

		// ── Valid slot — record it and advance cursor ───────────────────────
		candidates = append(candidates, bulkCandidate{
			ScheduleID: c.sch.ID,
			DayOfWeek:  c.sch.DayOfWeek,
			StartTime:  c.sch.StartTime,
			EndTime:    c.sch.EndTime,
			ClassDate:  c.next,
		})
		c.next = c.next.AddDate(0, 0, 7)
	}

	if len(candidates) < quota {
		return nil, fmt.Errorf(
			"tidak dapat menemukan cukup sesi yang tersedia. "+
				"Hanya %d dari %d sesi dapat dijadwalkan",
			len(candidates), quota,
		)
	}

	return candidates, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// BulkBookPreview — dry-run: returns candidate dates without any DB writes.
// ─────────────────────────────────────────────────────────────────────────────

func (r *studentRepository) BulkBookPreview(
	ctx context.Context,
	studentUUID string,
	studentPackageID int,
	scheduleIDs []int,
) ([]domain.BulkBookPreview, error) {
	sp, schedules, instrumentID, isDrum, err := r.validateBulkBookInputs(ctx, studentUUID, studentPackageID, scheduleIDs)
	if err != nil {
		return nil, err
	}

	candidates, err := r.generateBulkCandidates(ctx, studentUUID, sp, schedules, instrumentID, isDrum)
	if err != nil {
		return nil, err
	}

	previews := make([]domain.BulkBookPreview, len(candidates))
	for i, c := range candidates {
		previews[i] = domain.BulkBookPreview{
			ScheduleID: c.ScheduleID,
			DayOfWeek:  c.DayOfWeek,
			StartTime:  c.StartTime,
			EndTime:    c.EndTime,
			ClassDate:  c.ClassDate,
		}
	}
	return previews, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// BulkBookClass — generate candidate dates then commit all in one transaction.
// ─────────────────────────────────────────────────────────────────────────────

func (r *studentRepository) BulkBookClass(
	ctx context.Context,
	studentUUID string,
	studentPackageID int,
	scheduleIDs []int,
) (*domain.BulkBookResult, error) {
	sp, schedules, instrumentID, isDrum, err := r.validateBulkBookInputs(ctx, studentUUID, studentPackageID, scheduleIDs)
	if err != nil {
		return nil, err
	}

	candidates, err := r.generateBulkCandidates(ctx, studentUUID, sp, schedules, instrumentID, isDrum)
	if err != nil {
		return nil, err
	}

	loc, _ := time.LoadLocation("Asia/Makassar")
	now := time.Now().In(loc)
	quota := sp.RemainingQuota

	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if rec := recover(); rec != nil {
			tx.Rollback()
		}
	}()

	var bookingIDs []int
	for _, c := range candidates {
		b := domain.Booking{
			StudentUUID:      studentUUID,
			ScheduleID:       c.ScheduleID,
			StudentPackageID: sp.ID,
			InstrumentID:     instrumentID,
			ClassDate:        c.ClassDate,
			Status:           domain.StatusBooked,
			BookedAt:         now,
		}
		if err := tx.Create(&b).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("gagal membuat booking untuk %s: %w", c.ClassDate.Format("02 Jan 2006"), err)
		}
		bookingIDs = append(bookingIDs, b.ID)
	}

	// Deduct the full quota atomically.
	if err := tx.Model(&domain.StudentPackage{}).
		Where("id = ?", sp.ID).
		UpdateColumn("remaining_quota", gorm.Expr("remaining_quota - ?", quota)).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("gagal mengurangi kuota: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("gagal menyimpan booking: %w", err)
	}

	// Reload created bookings with full relations for the response.
	var bookings []domain.Booking
	r.db.WithContext(ctx).
		Where("id IN ?", bookingIDs).
		Preload("Schedule").
		Preload("Schedule.Teacher").
		Preload("PackageUsed.Package.Instrument").
		Order("class_date ASC").
		Find(&bookings)

	return &domain.BulkBookResult{
		TotalBooked: len(bookings),
		QuotaUsed:   quota,
		Bookings:    bookings,
	}, nil
}

// validateBulkBookInputs is a shared pre-flight check for both preview and commit.
func (r *studentRepository) validateBulkBookInputs(
	ctx context.Context,
	studentUUID string,
	studentPackageID int,
	scheduleIDs []int,
) (sp domain.StudentPackage, schedules []domain.TeacherSchedule, instrumentID int, isDrum bool, err error) {
	// ── 1. Load & validate student package ────────────────────────────────
	if err = r.db.WithContext(ctx).
		Joins("JOIN packages ON packages.id = student_packages.package_id").
		Preload("Package.Instrument").
		Where("student_packages.id = ?", studentPackageID).
		Where("student_packages.student_uuid = ?", studentUUID).
		Where("packages.is_trial = false").
		Where("student_packages.remaining_quota > 0").
		Where("student_packages.end_date > NOW()").
		First(&sp).Error; err != nil {
		err = errors.New("paket tidak valid, kuota habis, atau paket sudah berakhir")
		return
	}

	// ── 2. Load & validate selected schedules ────────────────────────────
	if err = r.db.WithContext(ctx).
		Where("id IN ? AND deleted_at IS NULL", scheduleIDs).
		Preload("Teacher").
		Preload("TeacherProfile.Instruments").
		Find(&schedules).Error; err != nil {
		return
	}
	if len(schedules) == 0 {
		err = errors.New("tidak ada jadwal yang ditemukan")
		return
	}
	if len(schedules) != len(scheduleIDs) {
		err = errors.New("beberapa jadwal tidak ditemukan atau sudah tidak aktif")
		return
	}

	// ── 3. All schedules must belong to the same teacher ─────────────────
	teacherUUID := schedules[0].TeacherUUID
	for _, sch := range schedules {
		if sch.TeacherUUID != teacherUUID {
			err = errors.New("semua jadwal harus berasal dari guru yang sama")
			return
		}
		if sch.Duration != sp.Package.Duration {
			err = fmt.Errorf(
				"durasi jadwal (%d menit) tidak sesuai dengan paket (%d menit)",
				sch.Duration, sp.Package.Duration,
			)
			return
		}
	}

	// ── 4. Teacher must teach the instrument in the package ───────────────
	if sp.Package.InstrumentID != nil {
		instrumentID = *sp.Package.InstrumentID
		teaches := false
		for _, inst := range schedules[0].TeacherProfile.Instruments {
			if inst.ID == instrumentID {
				teaches = true
				break
			}
		}
		if !teaches {
			err = errors.New("guru ini tidak mengajar instrumen dari paket yang dipilih")
			return
		}
		if sp.Package.Instrument != nil {
			isDrum = strings.EqualFold(sp.Package.Instrument.Name, "drum") ||
				strings.EqualFold(sp.Package.Instrument.Name, "drums")
		}
	}

	return sp, schedules, instrumentID, isDrum, nil
}
