package repository

import (
	"chronosphere/domain"
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type paymentRepo struct {
	db *gorm.DB
}

func NewPaymentRepository(db *gorm.DB) domain.PaymentRepository {
	return &paymentRepo{db: db}
}

func (r *paymentRepo) CreatePayment(ctx context.Context, payment *domain.Payment) error {
	return r.db.WithContext(ctx).Create(payment).Error
}

func (r *paymentRepo) GetPaymentByExternalID(ctx context.Context, externalID string) (*domain.Payment, error) {
	var payment domain.Payment
	err := r.db.WithContext(ctx).
		Preload("Student").
		Preload("Package").
		Preload("Package.Instrument").
		Where("external_id = ?", externalID).
		First(&payment).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("payment tidak ditemukan")
		}
		return nil, err
	}
	return &payment, nil
}

func (r *paymentRepo) UpdatePaymentStatus(ctx context.Context, externalID string, status string, method *string, paidAt *time.Time) error {
	updates := map[string]interface{}{
		"status": status,
	}
	if method != nil {
		updates["payment_method"] = *method
	}
	if paidAt != nil {
		updates["paid_at"] = *paidAt
	}

	result := r.db.WithContext(ctx).
		Model(&domain.Payment{}).
		Where("external_id = ?", externalID).
		Updates(updates)

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("payment dengan external_id %s tidak ditemukan", externalID)
	}
	return nil
}

func (r *paymentRepo) GetPaymentsByStudent(ctx context.Context, studentUUID string) ([]domain.Payment, error) {
	var payments []domain.Payment
	err := r.db.WithContext(ctx).
		Preload("Package").
		Preload("Package.Instrument").
		Where("student_uuid = ?", studentUUID).
		Order("created_at DESC").
		Find(&payments).Error
	if err != nil {
		return nil, err
	}
	return payments, nil
}

// ========================================================================
// Admin Reporting Methods
// ========================================================================

// GetTotalProfit calculates total revenue from paid payments
func (r *paymentRepo) GetTotalProfit(ctx context.Context, filter domain.ProfitFilter) (float64, error) {
	var total float64
	query := r.db.WithContext(ctx).Model(&domain.Payment{}).
		Where("status = ?", domain.PaymentStatusPaid)

	if filter.StartDate != "" {
		query = query.Where("DATE(paid_at) >= ?", filter.StartDate)
	}
	if filter.EndDate != "" {
		query = query.Where("DATE(paid_at) <= ?", filter.EndDate)
	}

	err := query.Select("COALESCE(SUM(amount), 0)").Scan(&total).Error
	if err != nil {
		return 0, fmt.Errorf("failed to calculate profit: %w", err)
	}
	return total, nil
}

// GetPaymentHistory retrieves payment history with pagination and filters
func (r *paymentRepo) GetPaymentHistory(ctx context.Context, filter domain.HistoryFilter) ([]domain.Payment, int64, error) {
	var payments []domain.Payment
	var total int64

	query := r.db.WithContext(ctx).Model(&domain.Payment{}).
		Preload("Student").
		Preload("Package").
		Preload("Package.Instrument")

	if filter.StartDate != "" {
		query = query.Where("DATE(created_at) >= ?", filter.StartDate)
	}
	if filter.EndDate != "" {
		query = query.Where("DATE(created_at) <= ?", filter.EndDate)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count payments: %w", err)
	}

	offset := (filter.Page - 1) * filter.Limit
	err := query.Order("created_at DESC").
		Limit(filter.Limit).
		Offset(offset).
		Find(&payments).Error
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch payment history: %w", err)
	}

	return payments, total, nil
}

// GetPackageSummary retrieves summary of sales per package
func (r *paymentRepo) GetPackageSummary(ctx context.Context) ([]domain.PackageSummary, error) {
	var summaries []domain.PackageSummary

	err := r.db.WithContext(ctx).Model(&domain.Payment{}).
		Select("packages.name as package_name, COUNT(payments.id) as total_sold, SUM(payments.amount) as total_revenue").
		Joins("JOIN packages ON packages.id = payments.package_id").
		Where("payments.status = ?", domain.PaymentStatusPaid).
		Group("packages.id, packages.name").
		Scan(&summaries).Error

	if err != nil {
		return nil, fmt.Errorf("failed to fetch package summary: %w", err)
	}

	return summaries, nil
}

// GetMonthlyRecognizedRevenue aggregates payment_recognitions by accounting
// period (year + month), covering both Xendit payments and manual payments
// since recognition rows are written for both source types on confirmation.
// Unlike GetTotalProfit/GetPaymentHistory (which bucket by paid_at/created_at
// and show the full lump sum on a single date), this reflects revenue spread
// evenly across a package's duration — e.g. a 3-month 1.2jt package confirmed
// in July shows 400k in July, 400k in August, 400k in September.
func (r *paymentRepo) GetMonthlyRecognizedRevenue(ctx context.Context, filter domain.MonthlyRevenueFilter) ([]domain.MonthlyRevenue, error) {
	var rows []domain.MonthlyRevenue

	query := r.db.WithContext(ctx).
		Table("payment_recognitions").
		Select("period_year, period_month, COALESCE(SUM(amount), 0) as amount")

	if filter.StartPeriod != "" {
		year, month, err := parseRevenuePeriod(filter.StartPeriod)
		if err != nil {
			return nil, fmt.Errorf("start_period tidak valid, gunakan format YYYY-MM: %w", err)
		}
		query = query.Where("(period_year * 100 + period_month) >= ?", year*100+month)
	}
	if filter.EndPeriod != "" {
		year, month, err := parseRevenuePeriod(filter.EndPeriod)
		if err != nil {
			return nil, fmt.Errorf("end_period tidak valid, gunakan format YYYY-MM: %w", err)
		}
		query = query.Where("(period_year * 100 + period_month) <= ?", year*100+month)
	}

	err := query.
		Group("period_year, period_month").
		Order("period_year ASC, period_month ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to fetch monthly recognized revenue: %w", err)
	}

	return rows, nil
}

// parseRevenuePeriod parses a "YYYY-MM" string into its year/month parts.
func parseRevenuePeriod(s string) (year, month int, err error) {
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return 0, 0, err
	}
	return t.Year(), int(t.Month()), nil
}