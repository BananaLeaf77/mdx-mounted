package domain

import "context"

type ManagerUseCase interface {
	GetAllStudents(ctx context.Context) ([]User, error)
	GetStudentByUUID(ctx context.Context, uuid string) (*User, error)
	ModifyStudentPackageQuota(ctx context.Context, studentUUID string, packageID int, incomingQuota int) error
	UpdateManager(ctx context.Context, manager *User) error
}

type ManagerRepository interface {
	GetAllStudents(ctx context.Context) ([]User, error)
	GetStudentByUUID(ctx context.Context, uuid string) (*User, error)
	ModifyStudentPackageQuota(ctx context.Context, studentUUID string, packageID int, incomingQuota int) (*User, error)
	UpdateManager(ctx context.Context, manager *User) error
}
