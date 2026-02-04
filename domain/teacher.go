package domain

import (
	"context"
)

type TeacherUseCase interface {
	GetMyProfile(ctx context.Context, userUUID string) (*User, error)
	UpdateTeacherData(ctx context.Context, userUUID string, payload User) error

	GetMySchedules(ctx context.Context, teacherUUID string) (*[]TeacherSchedule, error)
	AddAvailability(ctx context.Context, schedule *[]TeacherSchedule) error
	DeleteAvailability(ctx context.Context, scheduleID int, teacherUUID string) error
	GetAllBookedClass(ctx context.Context, uuid string) (*[]Booking, error)
	CancelBookedClass(ctx context.Context, bookingID int, teacherUUID string, reason *string) error
	FinishClass(ctx context.Context, bookingID int, teacherUUID string, payload ClassHistory) error
	GetMyClassHistory(ctx context.Context, teacherUUID string) (*[]ClassHistory, error)
	DeleteAvailabilityBasedOnDay(ctx context.Context, teacherUUID string, dayOfWeek string) error
}

type TeacherRepository interface {
	GetMyProfile(ctx context.Context, userUUID string) (*User, error)
	UpdateTeacherData(ctx context.Context, userUUID string, payload User) error

	AddAvailability(ctx context.Context, schedule *[]TeacherSchedule) error
	GetMySchedules(ctx context.Context, teacherUUID string) (*[]TeacherSchedule, error)
	DeleteAvailability(ctx context.Context, scheduleID int, teacherUUID string) error
	GetAllBookedClass(ctx context.Context, uuid string) (*[]Booking, error)
	CancelBookedClass(ctx context.Context, bookingID int, teacherUUID string, reason *string) (*Booking, error)
	FinishClass(ctx context.Context, bookingID int, teacherUUID string, payload ClassHistory) error
	GetMyClassHistory(ctx context.Context, teacherUUID string) (*[]ClassHistory, error)
	DeleteAvailabilityBasedOnDay(ctx context.Context, teacherUUID string, dayOfWeek string) error
}
