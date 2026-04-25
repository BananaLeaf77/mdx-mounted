package repository

import (
	"chronosphere/domain"
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type manualPaymentRepo struct {
	db *gorm.DB
}

func NewManualPaymentRepository(db *gorm.DB) domain.ManualPaymentRepository {
	return &manualPaymentRepo{db: db}
}

// compile-time interface assertion
var _ domain.ManualPaymentRepository = (*manualPaymentRepo)(nil)

func (r *manualPaymentRepo) Create(ctx context.Context, mp *domain.ManualPayment) error {
	if err := r.db.WithContext(ctx).Create(mp).Error; err != nil {
		return fmt.Errorf("gagal menyimpan permintaan pembayaran: %w", err)
	}
	return nil
}

func (r *manualPaymentRepo) GetAll(ctx context.Context, status string) ([]domain.ManualPayment, error) {
	var records []domain.ManualPayment

	q := r.db.WithContext(ctx).
		Preload("Student").
		Preload("Package").
		Preload("Package.Instrument").
		Order("created_at DESC")

	if status != "" {
		q = q.Where("status = ?", status)
	}

	if err := q.Find(&records).Error; err != nil {
		return nil, fmt.Errorf("gagal mengambil daftar pembayaran manual: %w", err)
	}
	return records, nil
}

func (r *manualPaymentRepo) GetByStudent(ctx context.Context, studentUUID string) ([]domain.ManualPayment, error) {
	var records []domain.ManualPayment

	if err := r.db.WithContext(ctx).
		Preload("Package").
		Preload("Package.Instrument").
		Where("student_uuid = ?", studentUUID).
		Order("created_at DESC").
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("gagal mengambil riwayat pembayaran manual: %w", err)
	}
	return records, nil
}

func (r *manualPaymentRepo) GetByID(ctx context.Context, id int) (*domain.ManualPayment, error) {
	var record domain.ManualPayment
	err := r.db.WithContext(ctx).
		Preload("Student").
		Preload("Package").
		Preload("Package.Instrument").
		Where("id = ?", id).
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("permintaan pembayaran tidak ditemukan")
		}
		return nil, fmt.Errorf("gagal mengambil data pembayaran: %w", err)
	}
	return &record, nil
}

func (r *manualPaymentRepo) UpdateStatus(
	ctx context.Context,
	id int,
	status string,
	adminUUID string,
	proofImageURL *string,
	notes *string,
	confirmedAt *time.Time,
) error {
	updates := map[string]interface{}{
		"status":          status,
		"confirmed_by":    adminUUID,
		"proof_image_url": proofImageURL, // nullable — only set when admin provides it
		"notes":           notes,
		"confirmed_at":    confirmedAt,
	}

	result := r.db.WithContext(ctx).
		Model(&domain.ManualPayment{}).
		Where("id = ?", id).
		Updates(updates)

	if result.Error != nil {
		return fmt.Errorf("gagal memperbarui status pembayaran: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return errors.New("permintaan pembayaran tidak ditemukan")
	}
	return nil
}

func (r *manualPaymentRepo) HasPendingForPackage(ctx context.Context, studentUUID string, packageID int) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&domain.ManualPayment{}).
		Where("student_uuid = ? AND package_id = ? AND status = ?",
			studentUUID, packageID, domain.ManualPaymentStatusPending).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("gagal memeriksa permintaan pending: %w", err)
	}
	return count > 0, nil
}