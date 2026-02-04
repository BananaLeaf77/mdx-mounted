package domain

import "context"

type AdminUseCase interface {
	// Self
	UpdateAdmin(ctx context.Context, payload User) error

	// Teacher Management
	CreateTeacher(ctx context.Context, user *User, instrumentIDs []int) (*User, error)
	GetAllTeachers(ctx context.Context) ([]User, error)
	GetTeacherByUUID(ctx context.Context, uuid string) (*User, error)
	UpdateTeacher(ctx context.Context, user *User, instrumentIDs []int) error

	// Student Management
	GetStudentByUUID(ctx context.Context, uuid string) (*User, error)
	AssignPackageToStudent(ctx context.Context, studentUUID string, packageID int) error
	GetAllStudents(ctx context.Context) ([]User, error)

	// Manager Management
	CreateManager(ctx context.Context, user *User) (*User, error)
	GetAllManagers(ctx context.Context) ([]User, error)
	GetManagerByUUID(ctx context.Context, uuid string) (*User, error)
	UpdateManager(ctx context.Context, user *User) error

	// Package
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
}

type AdminRepository interface {
	// Self
	UpdateAdmin(ctx context.Context, payload User) error

	// Teacher Management
	CreateTeacher(ctx context.Context, user *User, instrumentIDs []int) (*User, error)
	GetAllTeachers(ctx context.Context) ([]User, error)
	GetTeacherByUUID(ctx context.Context, uuid string) (*User, error)
	UpdateTeacher(ctx context.Context, user *User, instrumentIDs []int) error

	// Student Management
	GetStudentByUUID(ctx context.Context, uuid string) (*User, error)
	AssignPackageToStudent(ctx context.Context, studentUUID string, packageID int) (*User, *Package, error)
	GetAllStudents(ctx context.Context) ([]User, error)

	// Manager Management
	CreateManager(ctx context.Context, user *User) (*User, error)
	GetAllManagers(ctx context.Context) ([]User, error)
	GetManagerByUUID(ctx context.Context, uuid string) (*User, error)
	UpdateManager(ctx context.Context, user *User) error

	// Package
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
}
