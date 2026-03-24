package dto

import (
	"chronosphere/domain"
	"strings"
)

type AddMultipleAvailabilityRequest struct {
	SlotsAvailability []SlotsAvailability `json:"slots_availability" binding:"required,min=1,dive"`
}

type SlotsAvailability struct {
	DayOfTheWeek []string `json:"day_of_the_week" binding:"required,min=1,dive,oneof=senin selasa rabu kamis jumat sabtu minggu"`
	StartTime    string   `json:"start_time" binding:"required,timeformat"`
	EndTime      string   `json:"end_time" binding:"required,timeformat"`
}
	
// Request untuk Create Teacher
type CreateTeacherRequest struct {
	Name              string  `json:"name" binding:"required,min=3,max=50"`
	Email             string  `json:"email" binding:"required,email"`
	Gender            string  `json:"gender" binding:"required,oneof=male female"`
	Phone             string  `json:"phone" binding:"required,numeric,min=9,max=14"`
	Password          string  `json:"password" binding:"required,min=8"`
	Image             *string `json:"image" binding:"omitempty,url"`
	Bio               *string `json:"bio" binding:"omitempty,max=1000"`
	Education         *string `json:"education" binding:"omitempty,max=500"`
	Certificates      *string `json:"certificates" binding:"omitempty,max=500"`
	YearsOfExperience *int    `json:"years_of_experience" binding:"omitempty,min=0"`
	Experience        *string `json:"experience" binding:"omitempty,max=1000"`
	TeachingStyle     *string `json:"teaching_style" binding:"omitempty,max=500"`
	Specialties       *string `json:"specialties" binding:"omitempty,max=500"`
	Languages         *string `json:"languages" binding:"omitempty,max=200"`
	InstrumentIDs     []int   `json:"instrument_ids" binding:"required,min=1,dive,gt=0"`
}

// Request untuk Update Teacher (by Admin)
type UpdateTeacherProfileRequest struct {
	InstrumentIDs     []int   `json:"instrument_ids" binding:"required,min=1,dive,gt=0"`
	Bio               *string `json:"bio" binding:"omitempty,max=1000"`
	Education         *string `json:"education" binding:"omitempty,max=500"`
	Certificates      *string `json:"certificates" binding:"omitempty,max=500"`
	YearsOfExperience *int    `json:"years_of_experience" binding:"omitempty,min=0"`
	Experience        *string `json:"experience" binding:"omitempty,max=1000"`
	TeachingStyle     *string `json:"teaching_style" binding:"omitempty,max=500"`
	Specialties       *string `json:"specialties" binding:"omitempty,max=500"`
	Languages         *string `json:"languages" binding:"omitempty,max=200"`
}

// Request untuk Update Teacher Profile (by Teacher themselves)
type UpdateTeacherProfileRequestByTeacher struct {
	Name              string  `json:"name" binding:"required,min=3,max=50"`
	Phone             string  `json:"phone" binding:"required,numeric,min=9,max=14"`
	Image             *string `json:"image" binding:"omitempty,url"`
	Gender            string  `json:"gender" binding:"required,oneof=male female"`
	Bio               *string `json:"bio" binding:"omitempty,max=1000"`
	Education         *string `json:"education" binding:"omitempty,max=500"`
	Certificates      *string `json:"certificates" binding:"omitempty,max=500"`
	YearsOfExperience *int    `json:"years_of_experience" binding:"omitempty,min=0"`
	Experience        *string `json:"experience" binding:"omitempty,max=1000"`
	TeachingStyle     *string `json:"teaching_style" binding:"omitempty,max=500"`
	Specialties       *string `json:"specialties" binding:"omitempty,max=500"`
	Languages         *string `json:"languages" binding:"omitempty,max=200"`
}

func MapCreateTeacherRequestToUserByTeacher(req *UpdateTeacherProfileRequestByTeacher) domain.User {
	return domain.User{
		Name: req.Name,
		// Email: req.Email,
		Phone:  req.Phone,
		Image:  req.Image,
		Gender: req.Gender,
		TeacherProfile: &domain.TeacherProfile{
			Bio: deref(req.Bio),
		},
	}
}

// Mapper: Convert DTO → Domain
func MapCreateTeacherRequestToUser(req *CreateTeacherRequest) *domain.User {
	return &domain.User{
		Name:     req.Name,
		Email:    strings.ToLower(req.Email),
		Phone:    req.Phone,
		Password: req.Password,
		Role:     domain.RoleTeacher,
		Gender:   req.Gender,
		Image:    req.Image,
		TeacherProfile: &domain.TeacherProfile{
			Bio:         deref(req.Bio),
			Instruments: mapInstrumentIDs(req.InstrumentIDs),
		},
	}
}

// Simplified request - teacher only needs to provide notes and optional photos
type FinishClassRequest struct {
    BookingID    int      `json:"booking_id" binding:"required,gt=0"`
    Notes        string   `json:"notes" binding:"omitempty,max=2000"`
    DocumentURLs []string `json:"documentations,omitempty" binding:"omitempty,dive,url"`
}

// ✅ Update mapper to handle string time conversion
func MapFinishClassRequestToClassHistory(req *FinishClassRequest, bookingID int) (domain.ClassHistory, error) {
	history := domain.ClassHistory{
		BookingID: bookingID,
		Notes:     &req.Notes,
		Status:    domain.StatusCompleted,
	}

	// Add documentation URLs if provided
	if len(req.DocumentURLs) > 0 {
		for _, url := range req.DocumentURLs {
			history.Documentations = append(history.Documentations, domain.ClassDocumentation{
				URL: url,
			})
		}
	}

	return history, nil
}
func MapUpdateTeacherRequestToUser(req *UpdateTeacherProfileRequest) *domain.User {
	return &domain.User{
		TeacherProfile: &domain.TeacherProfile{
			Instruments: mapInstrumentIDs(req.InstrumentIDs),
		},
	}
}

// helper internal
func mapInstrumentIDs(ids []int) []domain.Instrument {
	instruments := make([]domain.Instrument, len(ids))
	for i, id := range ids {
		instruments[i] = domain.Instrument{ID: id}
	}
	return instruments
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
