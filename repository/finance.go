package repository

import (
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"gorm.io/gorm"
)

type financeRepo struct {
	db *gorm.DB
}

func NewFinanceRepository(db *gorm.DB) domain.FinanceRepository {
	return &financeRepo{db: db}
}

// ─────────────────────────────────────────────────────────────────────────────
// CreateFinance
// ─────────────────────────────────────────────────────────────────────────────

func (r *financeRepo) CreateFinance(ctx context.Context, user *domain.User) (*domain.User, error) {
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if rec := recover(); rec != nil {
			tx.Rollback()
		}
	}()

	// Uniqueness check on email / phone
	var existing domain.User
	if err := tx.Where("(email = ? OR phone = ?) AND deleted_at IS NULL", user.Email, user.Phone).
		First(&existing).Error; err == nil {
		tx.Rollback()
		return nil, errors.New("email atau nomor telepon sudah digunakan")
	}

	// Default profile image
	defImage := os.Getenv("DEFAULT_PROFILE_IMAGE")
	if user.Image == nil || *user.Image == "" {
		user.Image = &defImage
	}

	// No teacher/student profile needed
	user.TeacherProfile = nil
	user.StudentProfile = nil

	if err := tx.Create(user).Error; err != nil {
		tx.Rollback()
		return nil, errors.New(utils.TranslateDBError(err))
	}

	// Refresh to populate UUID if the DB generated it
	if user.UUID == "" {
		if err := tx.Where("email = ? AND deleted_at IS NULL", user.Email).First(user).Error; err != nil {
			tx.Rollback()
			return nil, errors.New("gagal mendapatkan UUID pengguna")
		}
	}

	if err := tx.Commit().Error; err != nil {
		return nil, errors.New(utils.TranslateDBError(err))
	}

	return user, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// GetAllFinance
// ─────────────────────────────────────────────────────────────────────────────

func (r *financeRepo) GetAllFinance(ctx context.Context) ([]domain.User, error) {
	var users []domain.User
	if err := r.db.WithContext(ctx).
		Where("role = ? AND deleted_at IS NULL", domain.RoleFinance).
		Find(&users).Error; err != nil {
		return nil, errors.New(utils.TranslateDBError(err))
	}
	return users, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// GetFinanceByUUID
// ─────────────────────────────────────────────────────────────────────────────

func (r *financeRepo) GetFinanceByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	var user domain.User
	if err := r.db.WithContext(ctx).
		Where("uuid = ? AND role = ? AND deleted_at IS NULL", uuid, domain.RoleFinance).
		First(&user).Error; err != nil {
		return nil, errors.New(utils.TranslateDBError(err))
	}
	return &user, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// UpdateFinance
// ─────────────────────────────────────────────────────────────────────────────

func (r *financeRepo) UpdateFinance(ctx context.Context, user *domain.User) error {
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if rec := recover(); rec != nil {
			tx.Rollback()
		}
	}()

	// Confirm user exists
	var existing domain.User
	if err := tx.Where("uuid = ? AND role = ? AND deleted_at IS NULL", user.UUID, domain.RoleFinance).
		First(&existing).Error; err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("pengguna finance tidak ditemukan")
		}
		return fmt.Errorf("error mencari pengguna: %w", err)
	}

	// Phone uniqueness
	if user.Phone != "" {
		var cnt int64
		if err := tx.Model(&domain.User{}).
			Where("phone = ? AND uuid != ? AND deleted_at IS NULL", user.Phone, user.UUID).
			Count(&cnt).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("error memeriksa telepon: %w", err)
		}
		if cnt > 0 {
			tx.Rollback()
			return errors.New("nomor telepon sudah digunakan oleh pengguna lain")
		}
	}

	if err := tx.Model(&domain.User{}).Where("uuid = ?", user.UUID).Updates(user).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("gagal memperbarui data pengguna finance: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("gagal commit transaksi: %w", err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// DeleteFinance  (soft-delete)
// ─────────────────────────────────────────────────────────────────────────────

func (r *financeRepo) DeleteFinance(ctx context.Context, uuid string) error {
	var user domain.User
	if err := r.db.WithContext(ctx).
		Where("uuid = ? AND role = ? AND deleted_at IS NULL", uuid, domain.RoleFinance).
		First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("pengguna finance tidak ditemukan")
		}
		return fmt.Errorf("error mencari pengguna: %w", err)
	}

	result := r.db.WithContext(ctx).Model(&domain.User{}).
		Where("uuid = ?", uuid).
		Update("deleted_at", time.Now())

	if result.Error != nil {
		return fmt.Errorf("gagal menonaktifkan pengguna finance: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return errors.New("pengguna finance tidak ditemukan")
	}
	return nil
}
