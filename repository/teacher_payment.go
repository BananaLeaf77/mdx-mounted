package repository

import (
	"chronosphere/domain"
	"context"
	"errors"
	"fmt"
	"log"
	"time"

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

	// Period boundaries (full calendar month, UTC)
	periodStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0).Add(-time.Nanosecond) // last nanosecond of the month

	// ── 1. Aggregate classes per teacher for the period ──────────────────────
	// Two sources are combined via UNION ALL:
	//   a) Formally completed classes (teacher called FinishClass → class_history exists)
	//   b) Stale bookings: class_date is in the period and already past, but the teacher
	//      never finished the class (no class_history). Teacher still owes notes/docs,
	//      but should still be paid for the class.
	// Period is based on b.class_date so a late-finished Feb class still counts for Feb.
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
			-- a) Formally completed: teacher submitted notes + docs via FinishClass
			SELECT
				ts.teacher_uuid,
				COALESCE(NULLIF(sp.price_paid, 0), p.price) / NULLIF(p.quota, 0) AS per_class_revenue
			FROM class_histories ch
			JOIN bookings         b  ON b.id  = ch.booking_id
			JOIN teacher_schedules ts ON ts.id = b.schedule_id
			JOIN student_packages sp ON sp.id  = b.student_package_id
			JOIN packages         p  ON p.id   = sp.package_id
			WHERE ch.status   = ?
			  AND b.class_date >= ? AND b.class_date <= ?

			UNION ALL

			-- b) Stale: booking is still "booked", class already happened in the period,
			--    no class_history exists yet (teacher forgot to finish)
			SELECT
				ts.teacher_uuid,
				COALESCE(NULLIF(sp.price_paid, 0), p.price) / NULLIF(p.quota, 0) AS per_class_revenue
			FROM bookings          b
			JOIN teacher_schedules ts ON ts.id = b.schedule_id
			JOIN student_packages sp ON sp.id  = b.student_package_id
			JOIN packages          p  ON p.id  = sp.package_id
			WHERE b.status     = ?
			  AND b.class_date >= ? AND b.class_date <= ?
			  AND NOT EXISTS (
				  SELECT 1 FROM class_histories ch2 WHERE ch2.booking_id = b.id
			  )
		) AS combined
		GROUP BY teacher_uuid`

	var rows []aggRow
	err := r.db.WithContext(ctx).
		Raw(rawSQL,
			domain.StatusCompleted, periodStart, periodEnd, // for part (a)
			domain.StatusBooked, periodStart, periodEnd,    // for part (b)
		).
		Scan(&rows).Error

	if err != nil {
		return nil, fmt.Errorf("gagal menghitung kelas: %w", err)
	}

	if len(rows) == 0 {
		return []domain.TeacherPaymentDetail{}, nil
	}

	// ── 2. Load teacher details for response ──────────────────────────────────
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

	// ── 3. Fetch existing payment records for this period (idempotency check) ─
	var existing []domain.TeacherPayment
	if err := r.db.WithContext(ctx).
		Where("period_start = ? AND period_end = ?", periodStart, periodEnd).
		Find(&existing).Error; err != nil {
		return nil, fmt.Errorf("gagal memeriksa data pembayaran existing: %w", err)
	}

	// key: teacher_uuid → existing record (so we can check status and update if needed)
	existingMap := make(map[string]domain.TeacherPayment, len(existing))
	for _, e := range existing {
		existingMap[e.TeacherUUID] = e
	}

	// ── 4. Insert new records + build response ────────────────────────────────
	var details []domain.TeacherPaymentDetail

	for _, row := range rows {
		earning := row.TotalPricePaid * commissionRate
		teacher := teacherMap[row.TeacherUUID]

		// ── DEBUG: trace raw values so you can spot a zero ──────────────────
		log.Printf(
			"[TeacherPayment] teacher=%s classes=%d price_paid_sum=%.2f commission=%.4f => earning=%.2f",
			row.TeacherUUID, row.ClassCount, row.TotalPricePaid, commissionRate, earning,
		)

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

		// ── Upsert logic ────────────────────────────────────────────────────────
		if prev, exists := existingMap[row.TeacherUUID]; exists {
			// Already paid → never touch it; keep existing amounts in detail
			if prev.Status == domain.TeacherPaymentStatusPaid {
				log.Printf(
					"[TeacherPayment] teacher=%s already PAID — skipped",
					row.TeacherUUID,
				)
				continue
			}

			// Unpaid → re-calculate and update so stale rows are corrected
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
			log.Printf("[TeacherPayment] teacher=%s unpaid record updated id=%d", row.TeacherUUID, prev.ID)
			continue
		}

		// No record yet → insert new
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