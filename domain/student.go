package domain

import (
	"context"
	"time"
)

// ScheduleAvailabilityResult enriches TeacherSchedule with availability flags
// and teacher performance data for the student's schedule browsing view.
type ScheduleAvailabilityResult struct {
	TeacherSchedule

	// Availability flags (computed per student context)
	IsBookedSameDayAndTime *bool `json:"is_booked_same_day_and_time,omitempty"`
	IsDurationCompatible   *bool `json:"is_duration_compatible,omitempty"`
	IsRoomAvailable        *bool `json:"is_room_available,omitempty"`
	IsFullyAvailable       *bool `json:"is_fully_available,omitempty"`

	// IsTeacherBusy is true when the teacher already has an active booking on this
	// date whose time window overlaps this schedule slot (e.g. their 60-min slot is
	// booked, so their simultaneous 30-min slot at the same start time is blocked too).
	IsTeacherBusy *bool `json:"is_teacher_busy,omitempty"`

	// Teacher performance (count of completed ClassHistory entries)
	TeacherFinishedClassCount int `json:"teacher_finished_class_count"`
}

// BulkBookPreview is one candidate session returned by the preview endpoint.
// Nothing is written to the database.
type BulkBookPreview struct {
	ScheduleID int       `json:"schedule_id"`
	DayOfWeek  string    `json:"day_of_week"`
	StartTime  string    `json:"start_time"`
	EndTime    string    `json:"end_time"`
	ClassDate  time.Time `json:"class_date"`
}

// BulkBookResult is returned after a successful bulk-book commit.
type BulkBookResult struct {
	TotalBooked int       `json:"total_booked"`
	QuotaUsed   int       `json:"quota_used"`
	Bookings    []Booking `json:"bookings"`
}

type StudentUseCase interface {
	GetAvailableSchedulesTrial(ctx context.Context, studentUUID string, packageID int) (*[]ScheduleAvailabilityResult, error)
	BookClassTrial(ctx context.Context, studentUUID string, scheduleID int, packageID int, instrumentID int) (*Booking, error)
	GetMyProfile(ctx context.Context, userUUID string) (*User, error)
	UpdateStudentData(ctx context.Context, userUUID string, user User) error
	GetAllAvailablePackages(ctx context.Context, studentUUID *string) (*[]Package, *Setting, error)

	// BookClass auto-selects the best active non-trial package for the given instrument.
	// Returns the saved booking for downstream use (e.g. WhatsApp notification).
	BookClass(ctx context.Context, studentUUID string, scheduleID int, instrumentID int) (*Booking, error)
	GetMyBookedClasses(ctx context.Context, studentUUID string, f PaginationFilter) (*[]Booking, error)
	CancelBookedClass(ctx context.Context, bookingID int, studentUUID string, reason *string) error

	// GetAvailableSchedules returns all teacher schedules enriched with availability flags
	// and teacher performance metrics. Trial packages show ALL schedules regardless of instrument/duration.
	GetAvailableSchedules(ctx context.Context, studentUUID string, instrumentID int) (*[]ScheduleAvailabilityResult, error)

	GetMyClassHistory(ctx context.Context, studentUUID string, f PaginationFilter) (*[]ClassHistory, error)
	GetTeacherDetails(ctx context.Context, teacherUUID string) (*User, error)

	// BulkBook: spend the full package quota in one action.
	GetTeacherSchedulesForPackage(ctx context.Context, teacherUUID string, studentPackageID int, studentUUID string) (*[]TeacherSchedule, error)
	BulkBookPreview(ctx context.Context, studentUUID string, studentPackageID int, scheduleIDs []int) ([]BulkBookPreview, error)
	BulkBookClass(ctx context.Context, studentUUID string, studentPackageID int, scheduleIDs []int) (*BulkBookResult, error)
}

type StudentRepository interface {
	GetAvailableSchedulesTrial(ctx context.Context, studentUUID string, packageID int) (*[]ScheduleAvailabilityResult, error)
	BookClassTrial(ctx context.Context, studentUUID string, scheduleID int, packageID int, instrumentID int) (*Booking, error)
	GetMyProfile(ctx context.Context, userUUID string) (*User, error)
	UpdateStudentData(ctx context.Context, userUUID string, user User) error
	GetAllAvailablePackages(ctx context.Context, studentUUID *string) (*[]Package, *Setting, error)

	// BookClass auto-selects the best active non-trial package for the given instrument.
	// Returns the saved booking for downstream use (e.g. WhatsApp notification).
	BookClass(ctx context.Context, studentUUID string, scheduleID int, instrumentID int) (*Booking, error)
	GetMyBookedClasses(ctx context.Context, studentUUID string, f PaginationFilter) (*[]Booking, error)
	CancelBookedClass(ctx context.Context, bookingID int, studentUUID string, reason *string) (*Booking, error)

	// GetAvailableSchedules with packageID for trial-aware filtering.
	GetAvailableSchedules(ctx context.Context, studentUUID string, instrumentID int) (*[]ScheduleAvailabilityResult, error)

	GetMyClassHistory(ctx context.Context, studentUUID string, f PaginationFilter) (*[]ClassHistory, error)

	// GetTeacherSchedulesBasedOnInstrumentIDs kept for internal use.
	GetTeacherSchedulesBasedOnInstrumentIDs(ctx context.Context, instrumentIDs []int) (*[]TeacherSchedule, error)

	GetTeacherDetails(ctx context.Context, teacherUUID string) (*User, error)

	// BulkBook: spend the full package quota in one action.
	GetTeacherSchedulesForPackage(ctx context.Context, teacherUUID string, studentPackageID int, studentUUID string) (*[]TeacherSchedule, error)
	BulkBookPreview(ctx context.Context, studentUUID string, studentPackageID int, scheduleIDs []int) ([]BulkBookPreview, error)
	BulkBookClass(ctx context.Context, studentUUID string, studentPackageID int, scheduleIDs []int) (*BulkBookResult, error)
}
