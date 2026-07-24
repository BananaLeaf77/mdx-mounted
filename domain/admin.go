package domain

import "context"

// WANumberInfo is the school's WhatsApp number, formatted for both display
// and wa.me linking — used to get new students/teachers to message the
// school's number first (WhatsApp requires an inbound message before a
// business account can message a "cold" contact, otherwise sends fail
// with a 463 reachout-timelock error).
type WANumberInfo struct {
	Raw    string `json:"raw"`     // 6289529539993
	Local  string `json:"local"`   // 089529539993
	WALink string `json:"wa_link"` // https://wa.me/6289529539993
}

type WAWarmupInfo struct {
	Warmed      bool          `json:"warmed"`
	AdminNumber *WANumberInfo `json:"admin_number,omitempty"` // only present when warmed == false
}

// StudentActivityFilter defines the status filter for student listing
type StudentActivityFilter string

const (
	// StudentFilterAll returns all students (no filter)
	StudentFilterAll StudentActivityFilter = "all"
	// StudentFilterActive - has at least one active package (remaining_quota > 0 and end_date not expired)
	StudentFilterActive StudentActivityFilter = "active"
	// StudentFilterInactiveShort - no active package, last purchase was < 3 months ago (or never bought)
	StudentFilterInactiveShort StudentActivityFilter = "inactive_short"
	// StudentFilterInactiveLong - no active package AND no purchase in the last 3+ months
	StudentFilterInactiveLong StudentActivityFilter = "inactive_long"
)

type AdminUseCase interface {
	GetWhatsAppWarmupInfo(ctx context.Context, userUUID string) (*WAWarmupInfo, error)
	GetWhatsAppWarmupStatus(ctx context.Context, userUUID string) (bool, error)
	GetAdminWhatsAppNumber(ctx context.Context) (*WANumberInfo, error)
	// Self
	UpdateAdmin(ctx context.Context, payload User) error
	GetBookedClasses(ctx context.Context) ([]Booking, error)

	// Teacher Management
	GetTeacherAllClasses(ctx context.Context, teacherUUID string, f PaginationFilter) (*[]Booking, error)
	CreateTeacher(ctx context.Context, user *User, instrumentIDs []int) (*User, error)
	GetAllTeachers(ctx context.Context) ([]User, error)
	GetTeacherByUUID(ctx context.Context, uuid string) (*User, error)
	UpdateTeacher(ctx context.Context, user *User, instrumentIDs []int) error

	// Student Management
	CreateStudent(ctx context.Context, user *User) (*User, error)
	GetStudentByUUID(ctx context.Context, uuid string) (*User, error)
	AssignPackageToStudent(ctx context.Context, studentUUID string, packageID int) error
	AssignPackageToStudentManual(ctx context.Context, studentUUID string, packageID int, recordData *bool, proofImageURL *string, notes *string) (*User, *Package, error)
	GetAllStudents(ctx context.Context) ([]User, error)
	GetFilteredStudents(ctx context.Context, filter StudentActivityFilter) ([]User, error)

	// Manager Management
	CreateManager(ctx context.Context, user *User) (*User, error)
	GetAllManagers(ctx context.Context) ([]User, error)
	GetManagerByUUID(ctx context.Context, uuid string) (*User, error)
	UpdateManager(ctx context.Context, user *User) error

	// Package
	TogglePackageActive(ctx context.Context, id int, isActive bool) error
	GetAllPackages(ctx context.Context) ([]Package, error)
	GetPackagesByID(ctx context.Context, id int) (*Package, error)
	CreatePackage(ctx context.Context, pkg *Package) (*Package, error)
	UpdatePackage(ctx context.Context, pkg *Package) error
	DeletePackage(ctx context.Context, id int) error

	// Instrument Management
	GetAllInstruments(ctx context.Context) ([]Instrument, error)
	CreateInstrument(ctx context.Context, instrument *Instrument) (*Instrument, error)
	UpdateInstrument(ctx context.Context, instrument *Instrument) error
	DeleteInstrument(ctx context.Context, id int) error

	// Users
	GetAllUsers(ctx context.Context) ([]User, error)
	DeleteUser(ctx context.Context, uuid string) error
	ClearUserDeletedAt(ctx context.Context, uuid string) error

	// Class
	GetAllClassHistories(ctx context.Context) (*[]ClassHistory, error)

	// Setting
	GetSetting(ctx context.Context) (*Setting, error)
	UpdateSetting(ctx context.Context, setting *Setting) error

	// WhatsApp
	GetWhatsAppStatus(ctx context.Context) (map[string]interface{}, error)
	ConnectWhatsApp(ctx context.Context) (map[string]interface{}, error)
	DisconnectWhatsApp(ctx context.Context) error
	PingWhatsApp(ctx context.Context, phone string) error
}

type AdminRepository interface {
	// Self
	UpdateAdmin(ctx context.Context, payload User) error
	GetBookedClasses(ctx context.Context) ([]Booking, error)

	// Teacher Management
	GetTeacherAllClasses(ctx context.Context, teacherUUID string, f PaginationFilter) (*[]Booking, error)
	CreateTeacher(ctx context.Context, user *User, instrumentIDs []int) (*User, error)
	GetAllTeachers(ctx context.Context) ([]User, error)
	GetTeacherByUUID(ctx context.Context, uuid string) (*User, error)
	UpdateTeacher(ctx context.Context, user *User, instrumentIDs []int) error

	// Student Management
	CreateStudent(ctx context.Context, user *User) (*User, error)
	GetStudentByUUID(ctx context.Context, uuid string) (*User, error)
	AssignPackageToStudent(ctx context.Context, studentUUID string, packageID int) (*User, *Package, error)
	AssignPackageToStudentManual(ctx context.Context, studentUUID string, packageID int, recordData *bool, proofImageURL *string, notes *string) (*User, *Package, error)
	GetAllStudents(ctx context.Context) ([]User, error)
	GetFilteredStudents(ctx context.Context, filter StudentActivityFilter) ([]User, error)

	// Manager Management
	CreateManager(ctx context.Context, user *User) (*User, error)
	GetAllManagers(ctx context.Context) ([]User, error)
	GetManagerByUUID(ctx context.Context, uuid string) (*User, error)
	UpdateManager(ctx context.Context, user *User) error

	// Package
	TogglePackageActive(ctx context.Context, id int, isActive bool) error
	GetAllPackages(ctx context.Context) ([]Package, error)
	GetPackagesByID(ctx context.Context, id int) (*Package, error)
	CreatePackage(ctx context.Context, pkg *Package) (*Package, error)
	UpdatePackage(ctx context.Context, pkg *Package) error
	DeletePackage(ctx context.Context, id int) error
	// Instrument Management
	GetAllInstruments(ctx context.Context) ([]Instrument, error)
	CreateInstrument(ctx context.Context, instrument *Instrument) (*Instrument, error)
	UpdateInstrument(ctx context.Context, instrument *Instrument) error
	DeleteInstrument(ctx context.Context, id int) error

	// Users
	GetAllUsers(ctx context.Context) ([]User, error)
	DeleteUser(ctx context.Context, uuid string) error
	ClearUserDeletedAt(ctx context.Context, uuid string) error

	// Class
	GetAllClassHistories(ctx context.Context) (*[]ClassHistory, error)

	// Setting
	GetSetting(ctx context.Context) (*Setting, error)
	UpdateSetting(ctx context.Context, setting *Setting) error

	// WhatsApp Cleanup
	CleanupWhatsAppData(ctx context.Context, deviceJID string) error

	// Payment
	CreateRecognitionRows(ctx context.Context, rows []PaymentRecognition) error
	GetExistingRecognitionSourceIDs(ctx context.Context, sourceType string) (map[int]bool, error)
	GetRecognitionRows(ctx context.Context, filter RecognitionRowFilter) ([]RecognitionRowDetail, int64, error)
}
