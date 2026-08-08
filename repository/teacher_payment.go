package repository

import (
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

type teacherPaymentRepo struct {
	db *gorm.DB
}

func NewTeacherPaymentRepository(db *gorm.DB) domain.TeacherPaymentRepository {
	return &teacherPaymentRepo{db: db}
}

// ─────────────────────────────────────────────────────────────────────────────
// GenerateMonthlyPayments
//
// Calculates earnings for every teacher who completed at least one class in
// the given period. Skips teachers who already have a payment record for the
// same period (idempotent — safe to call multiple times).
//
// Earning per class = StudentPackage.PricePaid × commissionRate
// ─────────────────────────────────────────────────────────────────────────────

func (r *teacherPaymentRepo) GenerateMonthlyPayments(
	ctx context.Context,
	year int,
	month int,
	commissionRate float64,
) ([]domain.TeacherPaymentDetail, error) {

	periodStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0).Add(-time.Nanosecond)

	type aggRow struct {
		TeacherUUID    string
		ClassCount     int
		TotalPricePaid float64
	}

	rawSQL := `
SELECT
    teacher_uuid,
    COUNT(*)               AS class_count,
    SUM(per_class_revenue) AS total_price_paid
FROM (
    SELECT DISTINCT
        b.id AS booking_id,
        ts.teacher_uuid,
        COALESCE(NULLIF(sp.price_paid, 0), p.price) / NULLIF(p.quota, 0) AS per_class_revenue
    FROM class_histories ch
    JOIN bookings          b  ON b.id  = ch.booking_id
    JOIN teacher_schedules ts ON ts.id = b.schedule_id
    JOIN student_packages  sp ON sp.id = b.student_package_id
    JOIN packages          p  ON p.id  = sp.package_id
    WHERE ch.status = ?
      AND b.class_date >= ? AND b.class_date <= ?
) AS combined
GROUP BY teacher_uuid`

	var rows []aggRow
	err := r.db.WithContext(ctx).
		Raw(rawSQL, domain.StatusCompleted, periodStart, periodEnd).
		Scan(&rows).Error

	if err != nil {
		if telegramErr := utils.NotifyTelegram(fmt.Sprintf(
			"🔴 *Generate Payroll Guru Gagal* (%04d-%02d)\n%v", year, month, err,
		)); telegramErr != nil {
			zlog.Warn().Msg(fmt.Sprintf("failed to send telegram payroll error notif: %v", telegramErr))
		}
		return nil, fmt.Errorf("gagal menghitung kelas: %w", err)
	}

	if len(rows) == 0 {
		if telegramErr := utils.NotifyTelegram(fmt.Sprintf(
			"🟡 *Generate Payroll Guru* (%04d-%02d)\nTidak ada kelas selesai pada periode ini — tidak ada guru untuk dibayar.",
			year, month,
		)); telegramErr != nil {
			zlog.Warn().Msg(fmt.Sprintf("failed to send telegram payroll notif: %v", telegramErr))
		}
		return []domain.TeacherPaymentDetail{}, nil
	}

	teacherUUIDs := make([]string, len(rows))
	for i, row := range rows {
		teacherUUIDs[i] = row.TeacherUUID
	}

	var teachers []domain.User
	if err := r.db.WithContext(ctx).
		Where("uuid IN ? AND role = ?", teacherUUIDs, domain.RoleTeacher).
		Find(&teachers).Error; err != nil {
		return nil, fmt.Errorf("gagal memuat data guru: %w", err)
	}

	teacherMap := make(map[string]domain.User, len(teachers))
	for _, t := range teachers {
		teacherMap[t.UUID] = t
	}

	var existing []domain.TeacherPayment
	if err := r.db.WithContext(ctx).
		Where("period_start = ? AND period_end = ?", periodStart, periodEnd).
		Find(&existing).Error; err != nil {
		return nil, fmt.Errorf("gagal memeriksa data pembayaran existing: %w", err)
	}

	existingMap := make(map[string]domain.TeacherPayment, len(existing))
	for _, e := range existing {
		existingMap[e.TeacherUUID] = e
	}

	var details []domain.TeacherPaymentDetail
	var inserted, updated, skippedPaid int
	var totalPayout float64
	var zeroEarningTeachers []string

	for _, row := range rows {
		earning := row.TotalPricePaid * commissionRate
		teacher := teacherMap[row.TeacherUUID]

		if earning == 0 {
			zeroEarningTeachers = append(zeroEarningTeachers, teacher.Name)
		}
		totalPayout += earning

		details = append(details, domain.TeacherPaymentDetail{
			TeacherUUID:    row.TeacherUUID,
			TeacherName:    teacher.Name,
			TeacherPhone:   teacher.Phone,
			ClassCount:     row.ClassCount,
			TotalPricePaid: row.TotalPricePaid,
			CommissionRate: commissionRate,
			TotalEarning:   earning,
			AmountDue:      earning,
			PeriodStart:    periodStart.Format("2006-01-02"),
			PeriodEnd:      periodEnd.Format("2006-01-02"),
		})

		if prev, exists := existingMap[row.TeacherUUID]; exists {
			if prev.Status == domain.TeacherPaymentStatusPaid {
				skippedPaid++
				continue
			}

			if err := r.db.WithContext(ctx).
				Model(&domain.TeacherPayment{}).
				Where("id = ?", prev.ID).
				Updates(map[string]interface{}{
					"class_count":   row.ClassCount,
					"total_earning": earning,
					"amount_due":    earning,
				}).Error; err != nil {
				return nil, fmt.Errorf("gagal memperbarui data pembayaran untuk guru %s: %w", row.TeacherUUID, err)
			}
			updated++
			continue
		}

		record := domain.TeacherPayment{
			TeacherUUID:  row.TeacherUUID,
			PeriodStart:  periodStart,
			PeriodEnd:    periodEnd,
			ClassCount:   row.ClassCount,
			TotalEarning: earning,
			AmountDue:    earning,
			Status:       domain.TeacherPaymentStatusUnpaid,
		}

		if err := r.db.WithContext(ctx).Create(&record).Error; err != nil {
			return nil, fmt.Errorf("gagal menyimpan data pembayaran untuk guru %s: %w", row.TeacherUUID, err)
		}
		inserted++
	}

	summary := fmt.Sprintf(
		"💵 *Generate Payroll Guru* (%04d-%02d)\nGuru diproses: %d\nBaru: %d | Diperbarui: %d | Dilewati (sudah dibayar): %d\nTotal payout: Rp%.0f\nKomisi: %.0f%%",
		year, month, len(rows), inserted, updated, skippedPaid, totalPayout, commissionRate*100,
	)
	if len(zeroEarningTeachers) > 0 {
		summary += fmt.Sprintf("\n\n⚠️ Earning Rp0 untuk: %s (cek price_paid/quota)", strings.Join(zeroEarningTeachers, ", "))
	}
	if telegramErr := utils.NotifyTelegram(summary); telegramErr != nil {
		zlog.Warn().Msg(fmt.Sprintf("failed to send telegram payroll notif: %v", telegramErr))
	}

	return details, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// GetAllPayments
// ─────────────────────────────────────────────────────────────────────────────

func (r *teacherPaymentRepo) GetAllPayments(ctx context.Context, status string) ([]domain.TeacherPayment, error) {
	var payments []domain.TeacherPayment

	q := r.db.WithContext(ctx).
		Preload("Teacher").
		Preload("PaidBy").
		Order("period_start DESC, teacher_uuid ASC")

	if status != "" {
		q = q.Where("status = ?", status)
	}

	if err := q.Find(&payments).Error; err != nil {
		return nil, fmt.Errorf("gagal mengambil data pembayaran: %w", err)
	}

	return payments, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// GetPaymentsByTeacher
// ─────────────────────────────────────────────────────────────────────────────

func (r *teacherPaymentRepo) GetPaymentsByTeacher(ctx context.Context, teacherUUID string, status string) ([]domain.TeacherPayment, error) {
	var payments []domain.TeacherPayment

	q := r.db.WithContext(ctx).
		Preload("Teacher").
		Preload("PaidBy").
		Where("teacher_uuid = ?", teacherUUID)

	if status != "" {
		q = q.Where("status = ?", status)
	}

	if err := q.Order("period_start DESC").Find(&payments).Error; err != nil {
		return nil, fmt.Errorf("gagal mengambil riwayat pembayaran guru: %w", err)
	}

	return payments, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// MarkAsPaid
// ─────────────────────────────────────────────────────────────────────────────

func (r *teacherPaymentRepo) MarkAsPaid(
	ctx context.Context,
	paymentID int,
	adminUUID string,
	req domain.MarkPaidRequest,
) error {
	var payment domain.TeacherPayment
	if err := r.db.WithContext(ctx).First(&payment, paymentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("data pembayaran tidak ditemukan")
		}
		return fmt.Errorf("gagal mencari data pembayaran: %w", err)
	}

	if payment.Status == domain.TeacherPaymentStatusPaid {
		return errors.New("pembayaran ini sudah ditandai sebagai lunas")
	}

	now := time.Now()
	updates := map[string]interface{}{
		"status":          domain.TeacherPaymentStatusPaid,
		"proof_image_url": req.ProofImageURL,
		"paid_at":         now,
		"paid_by_uuid":    adminUUID,
		"notes":           req.Notes,
	}

	if err := r.db.WithContext(ctx).
		Model(&domain.TeacherPayment{}).
		Where("id = ?", paymentID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("gagal memperbarui status pembayaran: %w", err)
	}

	return nil
}
