package dto

import "chronosphere/domain"

type UpdateStudentDataRequest struct {
	Name   string  `json:"name" binding:"required,min=3,max=50"`
	Phone  string  `json:"phone" binding:"required,numeric,min=9,max=14"`
	Image  *string `json:"image" binding:"omitempty,url"`
	Gender string  `json:"gender" binding:"required,oneof=male female"`
}

func MapUpdateStudentRequestByStudent(req *UpdateStudentDataRequest) domain.User {
	return domain.User{
		Name:   req.Name,
		Phone:  req.Phone,
		Image:  req.Image,
		Gender: req.Gender,
	}
}

type CreateStudentRequest struct {
	Name     string  `json:"name" binding:"required,min=3,max=50"`
	Gender   string  `json:"gender" binding:"required,oneof=male female"`
	Email    string  `json:"email" binding:"required,email"`
	Phone    string  `json:"phone" binding:"required,numeric,min=9,max=14"`
	Password string  `json:"password" binding:"required,min=8,max=64"`
	Image    *string `json:"image" binding:"omitempty,url"`
}

func MapCreateStudentRequestToUser(req *CreateStudentRequest) *domain.User {
	return &domain.User{
		Name:     req.Name,
		Gender:   req.Gender,
		Email:    req.Email,
		Phone:    req.Phone,
		Password: req.Password,
		Image:    req.Image,
	}
}

type BookClassRequest struct {
	ScheduleID   int `json:"schedule_id" binding:"required,min=1"`
	InstrumentID int `json:"instrument_id" binding:"required,min=1"`
}

type CancelBookingRequest struct {
	Reason *string `json:"reason" binding:"omitempty,max=2000"`
}

type BookClassTrialRequest struct {
    ScheduleID   int `json:"schedule_id"   binding:"required,min=1"`
    PackageID    int `json:"package_id"    binding:"required,min=1"` // student_packages.id
    InstrumentID int `json:"instrument_id" binding:"required,min=1"`
}

// BulkBookRequest is used to book all (or all remaining) quota sessions at once.
// The system cycles through the given schedule IDs (earliest-date-first) until
// the package's remaining_quota reaches zero.
type BulkBookRequest struct {
	StudentPackageID int   `json:"student_package_id" binding:"required,min=1"`
	ScheduleIDs      []int `json:"schedule_ids"       binding:"required,min=1"`
}