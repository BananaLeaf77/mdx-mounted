package dto

import (
	"chronosphere/domain"
	"strings"
)

// BulkAddAvailabilityRequest is the new frontend-friendly payload.
// Each entry in DaySlots represents one day column in the UI.
type BulkAddAvailabilityRequest struct {
	DaySlots []DaySlot `json:"day_slots" binding:"required,min=1,dive"`
}

// DaySlot represents one day's configuration chosen by the teacher.
type DaySlot struct {
	// Day in Indonesian lowercase: senin, selasa, rabu, kamis, jumat, sabtu, minggu
	Day string `json:"day" binding:"required,oneof=senin selasa rabu kamis jumat sabtu minggu"`

	// StartHours are the fixed hour anchors selected by the teacher.
	// Format: "HH:MM" e.g. "07:00", "07:30", "08:00" ... "21:00"
	StartHours []string `json:"start_hours" binding:"required,min=1,dive,timeformat"`

	// Durations is a subset of [30, 60] — which class lengths are enabled.
	Durations []int `json:"durations" binding:"required,min=1,dive,oneof=30 60"`
}

type AddMultipleAvailabilityRequest struct {
	SlotsAvailability []SlotsAvailability `json:"slots_availability" binding:"required,min=1,dive"`
}

type SlotsAvailability struct {
	DayOfTheWeek []string `json:"day_of_the_week" binding:"required,min=1,dive,oneof=senin selasa rabu kamis jumat sabtu minggu"`
	StartTime    string   `json:"start_time" binding:"required,timeformat"`
	EndTime      string   `json:"end_time" binding:"required,timeformat"`
}

// Request untuk Create Teacher
// dto/teacher_dto.go — CreateTeacherRequest
type CreateTeacherRequest struct {
	Name              string  `json:"name" binding:"required,min=3,max=50"`
	Email             string  `json:"email" binding:"required,email"`
	Gender            string  `json:"gender" binding:"required,oneof=male female"`
	Phone             string  `json:"phone" binding:"required,min=8,max=20"`
	CountryCode       string  `json:"country_code" binding:"omitempty,len=2"`
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
	Name        string `json:"name" binding:"required,min=3,max=50"`
	Phone       string `json:"phone" binding:"required,min=8,max=20"`
	CountryCode string `json:"country_code" binding:"omitempty,len=2"` // defaults to "ID" if empty
	Image             *string  `json:"image" binding:"omitempty,url"`
	Gender            string   `json:"gender" binding:"required,oneof=male female"`
	Bio               *string  `json:"bio" binding:"omitempty,max=1000"`
	Education         *string  `json:"education" binding:"omitempty,max=500"`
	Certificates      *string  `json:"certificates" binding:"omitempty,max=500"`
	YearsOfExperience *int     `json:"years_of_experience" binding:"omitempty,min=0"`
	Experience        *string  `json:"experience" binding:"omitempty,max=1000"`
	TeachingStyle     *string  `json:"teaching_style" binding:"omitempty,max=500"`
	Specialties       *string  `json:"specialties" binding:"omitempty,max=500"`
	Languages         *string  `json:"languages" binding:"omitempty,max=200"`
	AlbumURLs         []string `json:"album_urls" binding:"omitempty,dive,url"`
}

func MapCreateTeacherRequestToUserByTeacher(req *UpdateTeacherProfileRequestByTeacher) domain.User {
	user := domain.User{
		Name:        req.Name,
		Phone:       req.Phone,
		Image:       req.Image,
		Gender:      req.Gender,
		CountryCode: req.CountryCode,
		TeacherProfile: &domain.TeacherProfile{
			Bio:               deref(req.Bio),
			Education:         deref(req.Education),
			Certificates:      deref(req.Certificates),
			YearsOfExperience: derefInt(req.YearsOfExperience),
			Experience:        deref(req.Experience),
			TeachingStyle:     deref(req.TeachingStyle),
			Specialties:       deref(req.Specialties),
			Languages:         deref(req.Languages),
		},
	}

	// Add Album URLs if provided
	if len(req.AlbumURLs) > 0 {
		for i, url := range req.AlbumURLs {
			user.TeacherProfile.Album = append(user.TeacherProfile.Album, domain.TeacherAlbum{
				URL:   url,
				Order: i + 1,
			})
		}
	}

	return user
}

// Mapper: Convert DTO → Domain
func MapCreateTeacherRequestToUser(req *CreateTeacherRequest) *domain.User {
	return &domain.User{
		Name:     req.Name,
		Email:    strings.ToLower(req.Email),
		Phone:    req.Phone,
		Password: req.Password,
		CountryCode: req.CountryCode,
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
			Instruments:       mapInstrumentIDs(req.InstrumentIDs),
			Bio:               deref(req.Bio),
			Education:         deref(req.Education),
			Certificates:      deref(req.Certificates),
			YearsOfExperience: derefInt(req.YearsOfExperience),
			Experience:        deref(req.Experience),
			TeachingStyle:     deref(req.TeachingStyle),
			Specialties:       deref(req.Specialties),
			Languages:         deref(req.Languages),
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

func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}
