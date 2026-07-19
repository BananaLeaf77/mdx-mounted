package domain

import (
	"context"
	"time"
)

// Payment status constants
const (
	PaymentStatusPending = "PENDING"
	PaymentStatusPaid    = "PAID"
	PaymentStatusExpired = "EXPIRED"
	PaymentStatusFailed  = "FAILED"

	PaymentMethodManualConfirm = "Manual Confirmation"
)

// BackfillResult reports what the one-time recognition backfill did.
type BackfillResult struct {
	PaymentsScanned       int      `json:"payments_scanned"`
	ManualPaymentsScanned int      `json:"manual_payments_scanned"`
	RowsCreated           int      `json:"rows_created"`
	Skipped               int      `json:"skipped_already_recognized"`
	Errors                []string `json:"errors,omitempty"`
}

// RecognitionRowFilter scopes a detailed, paginated recognition-row query.
type RecognitionRowFilter struct {
	StudentName string `form:"student_name"`
	PackageID   int    `form:"package_id"`
	StartPeriod string `form:"start_period"` // YYYY-MM
	EndPeriod   string `form:"end_period"`   // YYYY-MM
	Page        int    `form:"page,default=1"`
	Limit       int    `form:"limit,default=20"`
}

// RecognitionRowDetail is one payment_recognitions row enriched with
// human-readable student/package names, for admin & finance auditing.
type RecognitionRowDetail struct {
	ID          int       `json:"id"`
	SourceType  string    `json:"source_type"`
	SourceID    int       `json:"source_id"`
	StudentUUID string    `json:"student_uuid"`
	StudentName string    `json:"student_name"`
	PackageID   int       `json:"package_id"`
	PackageName string    `json:"package_name"`
	PeriodYear  int       `json:"period_year"`
	PeriodMonth int       `json:"period_month"`
	Amount      float64   `json:"amount"`
	CreatedAt   time.Time `json:"created_at"`
}

type Payment struct {
	ID              int        `gorm:"primaryKey" json:"id"`
	StudentUUID     string     `gorm:"type:uuid;not null" json:"student_uuid"`
	PackageID       int        `gorm:"not null" json:"package_id"`
	XenditInvoiceID string     `gorm:"unique;not null" json:"xendit_invoice_id"`
	ExternalID      string     `gorm:"unique;not null" json:"external_id"`
	Amount          float64    `gorm:"not null" json:"amount"`
	Status          string     `gorm:"size:20;default:'PENDING'" json:"status"`
	PaymentMethod   *string    `json:"payment_method,omitempty"`
	PaidAt          *time.Time `json:"paid_at,omitempty"`
	InvoiceURL      string     `gorm:"type:text" json:"invoice_url"`
	CreatedAt       time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"autoUpdateTime" json:"updated_at"`

	Student User    `gorm:"foreignKey:StudentUUID;references:UUID" json:"student,omitempty"`
	Package Package `gorm:"foreignKey:PackageID" json:"package,omitempty"`
}

// XenditWebhookPayload represents the webhook callback from Xendit
type XenditWebhookPayload struct {
	ID                     string  `json:"id"`
	ExternalID             string  `json:"external_id"`
	UserID                 string  `json:"user_id"`
	Status                 string  `json:"status"`
	MerchantName           string  `json:"merchant_name"`
	Amount                 float64 `json:"amount"`
	PayerEmail             string  `json:"payer_email,omitempty"`
	Description            string  `json:"description,omitempty"`
	PaymentMethod          string  `json:"payment_method,omitempty"`
	PaymentChannel         string  `json:"payment_channel,omitempty"`
	PaidAt                 string  `json:"paid_at,omitempty"`
	Currency               string  `json:"currency,omitempty"`
	PaymentDestination     string  `json:"payment_destination,omitempty"`
	SuccessRedirectURL     string  `json:"success_redirect_url,omitempty"`
	FailureRedirectURL     string  `json:"failure_redirect_url,omitempty"`
	CreditCardChargeID     string  `json:"credit_card_charge_id,omitempty"`
	AdjustedReceivedAmount float64 `json:"adjusted_received_amount,omitempty"`
	BankCode               string  `json:"bank_code,omitempty"`
	EwalletType            string  `json:"ewallet_type,omitempty"`
	OnDemandLink           string  `json:"on_demand_link,omitempty"`
	RecurringPaymentID     string  `json:"recurring_payment_id,omitempty"`
}

// ========================================================================
// Admin Reporting Types (from Chrono)
// ========================================================================

type ProfitFilter struct {
	StartDate string `form:"start_date"` // YYYY-MM-DD
	EndDate   string `form:"end_date"`   // YYYY-MM-DD
}

type HistoryFilter struct {
	Page      int    `form:"page,default=1"`
	Limit     int    `form:"limit,default=10"`
	StartDate string `form:"start_date"`
	EndDate   string `form:"end_date"`
	Status    string `form:"status"` // PENDING, PAID, EXPIRED, FAILED
}

type PackageSummary struct {
	PackageName  string  `json:"package_name"`
	TotalSold    int     `json:"total_sold"`
	TotalRevenue float64 `json:"total_revenue"`
}

// MonthlyRevenueFilter scopes the recognized-revenue report to a period range.
// Both bounds are optional and use "YYYY-MM" format (e.g. "2026-07").
type MonthlyRevenueFilter struct {
	StartPeriod string `form:"start_period"` // YYYY-MM
	EndPeriod   string `form:"end_period"`   // YYYY-MM
}

// MonthlyRevenue is one row of recognized revenue for a given accounting period,
// aggregated from payment_recognitions (i.e. already split across a package's
// duration, not the lump sum paid on a single date).
type MonthlyRevenue struct {
	PeriodYear  int     `json:"period_year"`
	PeriodMonth int     `json:"period_month"`
	Amount      float64 `json:"amount"`
}

// ========================================================================
// Interfaces
// ========================================================================

type PaymentUseCase interface {
	CreateInvoice(ctx context.Context, studentUUID string, packageID int) (*Payment, error)
	HandleWebhook(ctx context.Context, payload XenditWebhookPayload) error
	GetPaymentsByStudent(ctx context.Context, studentUUID string) ([]Payment, error)
	GetInvoicePDF(ctx context.Context, externalID string, requesterUUID string, isAdmin bool) ([]byte, error) // NEW

	GetTotalProfit(ctx context.Context, filter ProfitFilter) (float64, error)
	GetPaymentHistory(ctx context.Context, filter HistoryFilter) ([]Payment, int64, error)
	GetPackageSummary(ctx context.Context) ([]PackageSummary, error)

	// GetMonthlyRecognizedRevenue returns revenue grouped by accounting period,
	// sourced from payment_recognitions (covers both Xendit and manual payments).
	GetMonthlyRecognizedRevenue(ctx context.Context, filter MonthlyRevenueFilter) ([]MonthlyRevenue, error)
	BackfillPaymentRecognitions(ctx context.Context) (*BackfillResult, error)
	GetRecognitionRows(ctx context.Context, filter RecognitionRowFilter) ([]RecognitionRowDetail, int64, error)
}

type PaymentRepository interface {
	CreatePayment(ctx context.Context, payment *Payment) error
	GetPaymentByExternalID(ctx context.Context, externalID string) (*Payment, error)
	UpdatePaymentStatus(ctx context.Context, externalID string, status string, method *string, paidAt *time.Time) error
	GetPaymentsByStudent(ctx context.Context, studentUUID string) ([]Payment, error)

	// Admin Reporting
	GetTotalProfit(ctx context.Context, filter ProfitFilter) (float64, error)
	GetPaymentHistory(ctx context.Context, filter HistoryFilter) ([]Payment, int64, error)
	GetPackageSummary(ctx context.Context) ([]PackageSummary, error)
	GetMonthlyRecognizedRevenue(ctx context.Context, filter MonthlyRevenueFilter) ([]MonthlyRevenue, error)
	GetAllPaidPayments(ctx context.Context) ([]Payment, error)
}
