package repository

import (
	"chronosphere/domain"
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type managerRepo struct {
	db *gorm.DB
}

func NewManagerRepository(db *gorm.DB) domain.ManagerRepository {
	return &managerRepo{db: db}
}

func (r *managerRepo) UpdateManager(ctx context.Context, payload *domain.User) error {
	// Mulai transaction
	tx := r.db.WithContext(ctx).Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Cek apakah user exists dan belum dihapus
	var existingUser domain.User
	err := tx.Where("uuid = ? AND role = ?", payload.UUID, domain.RoleManagement).First(&existingUser).Error
	if err != nil {
		tx.Rollback()
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("manager tidak ditemukan")
		}
		return fmt.Errorf("error mencari manager: %w", err)
	}

	// Check email duplicate dengan user lain
	// var emailCount int64
	// err = tx.Model(&domain.User{}).
	// 	Where("email = ? AND uuid != ?", payload.Email, payload.UUID).
	// 	Count(&emailCount).Error
	// if err != nil {
	// 	tx.Rollback()
	// 	return fmt.Errorf("error checking email: %w", err)
	// }
	// if emailCount > 0 {
	// 	tx.Rollback()
	// 	return errors.New("email sudah digunakan oleh user lain")
	// }

	// Check phone duplicate dengan user lain
	var phoneCount int64
	err = tx.Model(&domain.User{}).
		Where("phone = ? AND uuid != ?", payload.Phone, payload.UUID).
		Count(&phoneCount).Error
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("error checking phone: %w", err)
	}
	if phoneCount > 0 {
		tx.Rollback()
		return errors.New("nomor telepon sudah digunakan oleh user lain")
	}

	// Update user data
	err = tx.Model(&domain.User{}).
		Where("uuid = ?", payload.UUID).
		Updates(payload).Error
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("gagal memperbarui data manager: %w", err)
	}

	// Commit transaction jika semua berhasil
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("gagal commit transaction: %w", err)
	}

	return nil
}

func (r *managerRepo) GetAllStudents(ctx context.Context) ([]domain.User, error) {
	var students []domain.User
	if err := r.db.WithContext(ctx).
		Preload("StudentProfile.Packages", "end_date >= ?", time.Now()).
		Preload("StudentProfile.Packages.Package.Instrument").
		Where("role = ? AND deleted_at IS NULL", domain.RoleStudent).
		Find(&students).Error; err != nil {
		return nil, err
	}
	return students, nil
}

func (r *managerRepo) GetStudentByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	var student domain.User
	if err := r.db.WithContext(ctx).
		Preload("StudentProfile.Packages", "end_date >= ?", time.Now()).
		Preload("StudentProfile.Packages.Package.Instrument").
		Where("uuid = ? AND role = ? AND deleted_at IS NULL", uuid, domain.RoleStudent).
		First(&student).Error; err != nil {
		return nil, err
	}
	return &student, nil
}

func (r *managerRepo) ModifyStudentPackageQuota(ctx context.Context, studentUUID string, packageID int, incomingQuota int) (*domain.User, error) {
	if incomingQuota > 50 {
		return nil, fmt.Errorf("quota cannot exceed 50")
	}

	// First, find the student package directly
	var studentPackage domain.StudentPackage
	if err := r.db.WithContext(ctx).
		Preload("Package").
		Preload("Package.Instrument").
		Where("student_uuid = ? AND package_id = ? AND end_date >= ?", studentUUID, packageID, time.Now()).
		First(&studentPackage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("active package not found for this student")
		}
		return nil, err
	}

	// Verify the student exists and has the correct role
	var student domain.User
	if err := r.db.WithContext(ctx).
		Where("uuid = ? AND role = ? AND deleted_at IS NULL", studentUUID, domain.RoleStudent).
		First(&student).Error; err != nil {
		return nil, err
	}

	// Update the remaining quota
	studentPackage.RemainingQuota = incomingQuota

	// Ensure remaining quota doesn't go negative
	if studentPackage.RemainingQuota < 0 {
		studentPackage.RemainingQuota = 0
	}

	// Save the student package
	err := r.db.WithContext(ctx).Save(&studentPackage).Error
	if err != nil {
		return nil, err
	}

	// Now query the full student data with all relationships
	var fullStudent domain.User
	if err := r.db.WithContext(ctx).
		// Preload StudentProfile
		Preload("StudentProfile").
		// Preload StudentProfile's Packages with nested Package and Instrument
		Preload("StudentProfile.Packages", func(db *gorm.DB) *gorm.DB {
			return db.
				Preload("Package").
				Preload("Package.Instrument").
				Where("end_date >= ?", time.Now()) // Only active packages
		}).
		// Preload TeacherProfile (if needed, though student won't have one)
		Preload("TeacherProfile").
		// Main where clause
		Where("uuid = ? AND role = ? AND deleted_at IS NULL", studentUUID, domain.RoleStudent).
		First(&fullStudent).Error; err != nil {
		return nil, err
	}

	return &fullStudent, nil
}
