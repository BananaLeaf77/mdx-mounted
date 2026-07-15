package domain

import "time"

// domain/payment.go (or new domain/revenue.go)
type PaymentRecognition struct {
	ID          int       `gorm:"primaryKey"`
	SourceType  string    `gorm:"size:20;not null"` // "payment" | "manual_payment"
	SourceID    int       `gorm:"not null;index"`
	StudentUUID string    `gorm:"type:uuid;not null;index"`
	PackageID   int       `gorm:"not null"`
	PeriodYear  int       `gorm:"not null"`
	PeriodMonth int       `gorm:"not null"` // 1-12
	Amount      float64   `gorm:"not null"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`
}
