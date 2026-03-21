package domain

import "context"

// FinanceUseCase is consumed by the delivery layer for finance-scoped operations.
type FinanceUseCase interface {
	// CreateFinance creates a new user with role "finance".
	// Only admin can call this.
	CreateFinance(ctx context.Context, user *User) (*User, error)

	// GetAllFinance returns all active finance users.
	GetAllFinance(ctx context.Context) ([]User, error)

	// GetFinanceByUUID returns a single finance user.
	GetFinanceByUUID(ctx context.Context, uuid string) (*User, error)

	// UpdateFinance updates a finance user's own profile.
	UpdateFinance(ctx context.Context, user *User) error

	// DeleteFinance soft-deletes a finance user (admin only).
	DeleteFinance(ctx context.Context, uuid string) error
}

// FinanceRepository is the persistence layer for finance users.
type FinanceRepository interface {
	CreateFinance(ctx context.Context, user *User) (*User, error)
	GetAllFinance(ctx context.Context) ([]User, error)
	GetFinanceByUUID(ctx context.Context, uuid string) (*User, error)
	UpdateFinance(ctx context.Context, user *User) error
	DeleteFinance(ctx context.Context, uuid string) error
}