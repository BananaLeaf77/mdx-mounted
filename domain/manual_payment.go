package domain

import (
	"context"
	"time"
)

const (
	ManualPaymentStatusPending   = "pending"
	ManualPaymentStatusConfirmed = "confirmed"
	ManualPaymentStatusRejected  = "rejected"
)

// ManualPayment tracks purchase requests that students initiate via WhatsApp.
// Admin confirms (or rejects) them manually after receiving payment.
type ManualPayment struct {
	ID          int        `gorm:"primaryKey" json:"id"`
	StudentUUID string     `gorm:"type:uuid;not null;index" json:"student_uuid"`
	Student     User       `gorm:"foreignKey:StudentUUID;references:UUID" json:"student,omitempty"`
	PackageID   int        `gorm:"not null" json:"package_id"`
	Package     Package    `gorm:"foreignKey:PackageID" json:"package,omitempty"`
	Status      string     `gorm:"size:20;default:'pending';index" json:"status"` // pending | confirmed | rejected
	TotalAmount float64    `gorm:"not null;default:0" json:"total_amount"`
	Notes         *string    `gorm:"type:text" json:"notes,omitempty"`            // admin notes on confirm/reject
	ProofImageURL *string    `gorm:"type:text" json:"proof_image_url,omitempty"` // optional payment proof (image URL)
	ConfirmedBy   *string    `gorm:"type:uuid" json:"confirmed_by,omitempty"`    // admin UUID
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty"`
	CreatedAt   time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"autoUpdateTime" json:"updated_at"`

	// Virtual — populated in service layer for the WA message preview
	RegistrationFee float64 `gorm:"-" json:"registration_fee,omitempty"`
	PackagePrice    float64 `gorm:"-" json:"package_price,omitempty"`
	IsFirstPurchase bool    `gorm:"-" json:"is_first_purchase,omitempty"`
}

// ManualPaymentConfirmRequest is the admin payload for confirming a payment.
type ManualPaymentConfirmRequest struct {
	ProofImageURL *string `json:"proof_image_url" binding:"omitempty,url"`
	Notes         *string `json:"notes" binding:"omitempty,max=500"`
}

// ManualPaymentRejectRequest is the admin payload for rejecting a payment.
type ManualPaymentRejectRequest struct {
	Notes *string `json:"notes" binding:"omitempty,max=500"`
}

// ManualPaymentUseCase is consumed by the delivery layer.
type ManualPaymentUseCase interface {
	// RequestManualPayment is called by the student.
	// Creates a pending record and fires a WhatsApp message to admin.
	RequestManualPayment(ctx context.Context, studentUUID string, packageID int) (*ManualPayment, error)

	// GetAllManualPayments returns all records; optionally filtered by status.
	// Accessible by admin / manager.
	GetAllManualPayments(ctx context.Context, status string) ([]ManualPayment, error)

	// GetMyManualPayments returns the calling student's payment requests.
	GetMyManualPayments(ctx context.Context, studentUUID string) ([]ManualPayment, error)

	// ConfirmManualPayment marks a payment as confirmed and assigns the package.
	// Accessible by admin / manager.
	ConfirmManualPayment(ctx context.Context, paymentID int, adminUUID string, req ManualPaymentConfirmRequest) error

	// RejectManualPayment marks a payment as rejected.
	// Accessible by admin / manager.
	RejectManualPayment(ctx context.Context, paymentID int, adminUUID string, req ManualPaymentRejectRequest) error
}

// ManualPaymentRepository handles persistence for manual payments.
type ManualPaymentRepository interface {
	Create(ctx context.Context, mp *ManualPayment) error
	GetAll(ctx context.Context, status string) ([]ManualPayment, error)
	GetByStudent(ctx context.Context, studentUUID string) ([]ManualPayment, error)
	GetByID(ctx context.Context, id int) (*ManualPayment, error)
	UpdateStatus(ctx context.Context, id int, status string, adminUUID string, proofImageURL *string, notes *string, confirmedAt *time.Time) error
	// HasPendingForPackage checks whether the student already has a pending request for the same package.
	HasPendingForPackage(ctx context.Context, studentUUID string, packageID int) (bool, error)
}