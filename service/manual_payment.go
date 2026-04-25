package service

import (
	"chronosphere/config"
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/gorm"
)

type manualPaymentSvc struct {
	repo      domain.ManualPaymentRepository
	adminRepo domain.AdminRepository
	db        *gorm.DB
	messenger *config.WAManager
}

// compile-time assertion
var _ domain.ManualPaymentUseCase = (*manualPaymentSvc)(nil)

func NewManualPaymentService(
	repo domain.ManualPaymentRepository,
	adminRepo domain.AdminRepository,
	db *gorm.DB,
	mgr *config.WAManager,
) domain.ManualPaymentUseCase {
	return &manualPaymentSvc{
		repo:      repo,
		adminRepo: adminRepo,
		db:        db,
		messenger: mgr,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RequestManualPayment
// ─────────────────────────────────────────────────────────────────────────────

func (s *manualPaymentSvc) RequestManualPayment(
	ctx context.Context,
	studentUUID string,
	packageID int,
) (*domain.ManualPayment, error) {

	student, err := s.adminRepo.GetStudentByUUID(ctx, studentUUID)
	if err != nil {
		return nil, fmt.Errorf("siswa tidak ditemukan: %w", err)
	}

	pkg, err := s.adminRepo.GetPackagesByID(ctx, packageID)
	if err != nil {
		return nil, fmt.Errorf("paket tidak ditemukan: %w", err)
	}

	// ── Trial guard ───────────────────────────────────────────────────────────
	if pkg.IsTrial {
		var trialXendit int64
		_ = s.db.WithContext(ctx).
			Table("payments").
			Joins("JOIN packages ON packages.id = payments.package_id").
			Where("payments.student_uuid = ? AND payments.status = ? AND packages.is_trial = true",
				studentUUID, domain.PaymentStatusPaid).
			Count(&trialXendit)

		var trialManual int64
		_ = s.db.WithContext(ctx).
			Table("manual_payments").
			Joins("JOIN packages ON packages.id = manual_payments.package_id").
			Where("manual_payments.student_uuid = ? AND manual_payments.status = ? AND packages.is_trial = true",
				studentUUID, domain.ManualPaymentStatusConfirmed).
			Count(&trialManual)

		if trialXendit > 0 || trialManual > 0 {
			return nil, errors.New("kamu sudah pernah membeli paket trial, paket ini hanya bisa dibeli satu kali")
		}
	}

	// ── Duplicate pending guard ───────────────────────────────────────────────
	hasPending, err := s.repo.HasPendingForPackage(ctx, studentUUID, packageID)
	if err != nil {
		return nil, err
	}
	if hasPending {
		return nil, errors.New("kamu sudah memiliki permintaan pembayaran yang sedang menunggu konfirmasi untuk paket ini")
	}

	// ── First-purchase check ──────────────────────────────────────────────────
	setting, err := s.adminRepo.GetSetting(ctx)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil pengaturan biaya: %w", err)
	}

	var priorXendit int64
	_ = s.db.WithContext(ctx).
		Table("payments").
		Joins("JOIN packages ON packages.id = payments.package_id").
		Where("payments.student_uuid = ? AND payments.status = ? AND packages.is_trial = false",
			studentUUID, domain.PaymentStatusPaid).
		Count(&priorXendit)

	var priorManual int64
	_ = s.db.WithContext(ctx).
		Table("manual_payments").
		Joins("JOIN packages ON packages.id = manual_payments.package_id").
		Where("manual_payments.student_uuid = ? AND manual_payments.status = ? AND packages.is_trial = false",
			studentUUID, domain.ManualPaymentStatusConfirmed).
		Count(&priorManual)

	var existingPkg int64
	_ = s.db.WithContext(ctx).
		Table("student_packages").
		Joins("JOIN packages ON packages.id = student_packages.package_id").
		Where("student_packages.student_uuid = ? AND packages.is_trial = false", studentUUID).
		Count(&existingPkg)

	isFirstPurchase := priorXendit == 0 && priorManual == 0 && existingPkg == 0

	// ── Pricing ───────────────────────────────────────────────────────────────
	pkgPrice := pkg.Price
	if pkg.IsPromoActive && pkg.PromoPrice > 0 {
		pkgPrice = pkg.PromoPrice
	}

	regFee := 0.0
	if isFirstPurchase && !pkg.IsTrial {
		regFee = setting.RegistrationFee
	}

	// ── Persist ───────────────────────────────────────────────────────────────
	mp := &domain.ManualPayment{
		StudentUUID:     studentUUID,
		PackageID:       packageID,
		Status:          domain.ManualPaymentStatusPending,
		TotalAmount:     pkgPrice + regFee,
		RegistrationFee: regFee,
		PackagePrice:    pkgPrice,
		IsFirstPurchase: isFirstPurchase,
	}

	if err := s.repo.Create(ctx, mp); err != nil {
		return nil, err
	}

	// ── Notifications (fire-and-forget) ───────────────────────────────────────
	mpCopy := *mp
	studentCopy := *student
	pkgCopy := *pkg

	go s.notifyAdmin(&studentCopy, &pkgCopy, &mpCopy)
	go s.notifyStudentPending(&studentCopy, &pkgCopy, &mpCopy)

	return mp, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// GetAllManualPayments
// ─────────────────────────────────────────────────────────────────────────────

func (s *manualPaymentSvc) GetAllManualPayments(ctx context.Context, status string) ([]domain.ManualPayment, error) {
	if status != "" &&
		status != domain.ManualPaymentStatusPending &&
		status != domain.ManualPaymentStatusConfirmed &&
		status != domain.ManualPaymentStatusRejected {
		return nil, fmt.Errorf("status tidak valid, gunakan: pending, confirmed, atau rejected")
	}
	return s.repo.GetAll(ctx, status)
}

// ─────────────────────────────────────────────────────────────────────────────
// GetMyManualPayments
// ─────────────────────────────────────────────────────────────────────────────

func (s *manualPaymentSvc) GetMyManualPayments(ctx context.Context, studentUUID string) ([]domain.ManualPayment, error) {
	return s.repo.GetByStudent(ctx, studentUUID)
}

// ─────────────────────────────────────────────────────────────────────────────
// ConfirmManualPayment
// ─────────────────────────────────────────────────────────────────────────────

func (s *manualPaymentSvc) ConfirmManualPayment(
	ctx context.Context,
	paymentID int,
	adminUUID string,
	req domain.ManualPaymentConfirmRequest,
) error {
	mp, err := s.repo.GetByID(ctx, paymentID)
	if err != nil {
		return err
	}
	if mp.Status != domain.ManualPaymentStatusPending {
		return fmt.Errorf("permintaan pembayaran ini sudah berstatus '%s'", mp.Status)
	}

	now := time.Now()
	if err := s.repo.UpdateStatus(ctx, paymentID, domain.ManualPaymentStatusConfirmed, adminUUID, req.ProofImageURL, req.Notes, &now); err != nil {
		return err
	}

	// Auto-assign package — mirrors Xendit webhook behaviour
	if _, _, err := s.adminRepo.AssignPackageToStudent(ctx, mp.StudentUUID, mp.PackageID); err != nil {
		log.Printf("⚠️  ManualPayment #%d confirm: auto-assign failed (admin can assign manually): %v", paymentID, err)
	}

	mpCopy := *mp
	go func() {
		bgCtx := context.Background()
		student, err := s.adminRepo.GetStudentByUUID(bgCtx, mpCopy.StudentUUID)
		if err != nil {
			return
		}
		pkg, err := s.adminRepo.GetPackagesByID(bgCtx, mpCopy.PackageID)
		if err != nil {
			return
		}
		s.notifyStudentConfirmed(student, pkg, &mpCopy)
	}()

	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// RejectManualPayment
// ─────────────────────────────────────────────────────────────────────────────

func (s *manualPaymentSvc) RejectManualPayment(
	ctx context.Context,
	paymentID int,
	adminUUID string,
	req domain.ManualPaymentRejectRequest,
) error {
	mp, err := s.repo.GetByID(ctx, paymentID)
	if err != nil {
		return err
	}
	if mp.Status != domain.ManualPaymentStatusPending {
		return fmt.Errorf("permintaan pembayaran ini sudah berstatus '%s'", mp.Status)
	}

	now := time.Now()
	if err := s.repo.UpdateStatus(ctx, paymentID, domain.ManualPaymentStatusRejected, adminUUID, nil, req.Notes, &now); err != nil {
		return err
	}

	mpCopy := *mp
	go func() {
		bgCtx := context.Background()
		student, err := s.adminRepo.GetStudentByUUID(bgCtx, mpCopy.StudentUUID)
		if err != nil {
			return
		}
		pkg, err := s.adminRepo.GetPackagesByID(bgCtx, mpCopy.PackageID)
		if err != nil {
			return
		}
		s.notifyStudentRejected(student, pkg, &mpCopy, req.Notes)
	}()

	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// WhatsApp helpers
// ─────────────────────────────────────────────────────────────────────────────

func (s *manualPaymentSvc) notifyAdmin(student *domain.User, pkg *domain.Package, mp *domain.ManualPayment) {
	if s.messenger == nil || !s.messenger.IsLoggedIn() {
		log.Println("🔕 WhatsApp not connected, skipping admin manual-payment notification")
		return
	}

	adminPhone := os.Getenv("ADMIN_WHATSAPP_NUMBER")
	if adminPhone == "" {
		log.Println("⚠️ ADMIN_WHATSAPP_NUMBER not set")
		return
	}
	normalized := utils.NormalizePhoneNumber(adminPhone)
	if normalized == "" {
		return
	}

	instrumentName := "-"
	if pkg.Instrument != nil {
		instrumentName = pkg.Instrument.Name
	}

	regFeeStr := "Rp0 (bukan pembelian pertama)"
	if mp.IsFirstPurchase {
		regFeeStr = fmt.Sprintf("Rp%.0f", mp.RegistrationFee)
	}

	msg := fmt.Sprintf(
		"Halo Admin %s,\n\n"+
			"Saya ingin membeli paket les musik dengan detail berikut:\n\n"+
			"📋 *ID Permintaan: #%d*\n\n"+
			"👤 *Data Siswa:*\n"+
			"- Nama: %s\n"+
			"- Email: %s\n"+
			"- No. HP: %s\n\n"+
			"📦 *Paket Dipilih:*\n"+
			"- Nama Paket: %s\n"+
			"- Instrumen: %s\n"+
			"- Durasi: %d Menit\n"+
			"- Kuota: %d Sesi\n"+
			"- Masa Berlaku: %s\n\n"+
			"💰 *Rincian Biaya:*\n"+
			"- Biaya Pendaftaran: %s\n"+
			"- Harga Paket: Rp%.0f\n"+
			"- *Total: Rp%.0f*\n\n"+
			"Mohon informasi cara pembayaran dan aktivasi paket saya. Terima kasih 🙏\n\n"+
			"🌐 %s\n"+
			"🔔 %s Notification System",
		os.Getenv("APP_NAME"),
		mp.ID,
		student.Name, student.Email, student.Phone,
		pkg.Name, instrumentName, pkg.Duration, pkg.Quota, manualFormatExpired(pkg.ExpiredDuration),
		regFeeStr, mp.PackagePrice, mp.TotalAmount,
		"https://www.mdxmusiccourse.cloud/",
		os.Getenv("APP_NAME"),
	)

	if err := s.messenger.SendMessage(normalized, msg); err != nil {
		log.Printf("⚠️ WA admin manual-payment notify failed: %v", err)
	}
}

func (s *manualPaymentSvc) notifyStudentPending(student *domain.User, pkg *domain.Package, mp *domain.ManualPayment) {
	if s.messenger == nil || !s.messenger.IsLoggedIn() {
		return
	}
	normalized := utils.NormalizePhoneNumber(student.Phone)
	if normalized == "" {
		return
	}

	instrumentName := "-"
	if pkg.Instrument != nil {
		instrumentName = pkg.Instrument.Name
	}
	regFeeStr := "Rp0"
	if mp.IsFirstPurchase {
		regFeeStr = fmt.Sprintf("Rp%.0f", mp.RegistrationFee)
	}

	msg := fmt.Sprintf(
		"Halo %s! 👋\n\n"+
			"Permintaan pembelian paket les Anda telah kami terima! ✅\n\n"+
			"📋 *ID Permintaan: #%d*\n\n"+
			"📦 *Detail Paket:*\n"+
			"- Nama Paket: %s\n"+
			"- Instrumen: %s\n"+
			"- Durasi: %d Menit\n"+
			"- Kuota: %d Sesi\n"+
			"- Masa Berlaku: %s\n\n"+
			"💰 *Rincian Biaya:*\n"+
			"- Biaya Pendaftaran: %s\n"+
			"- Harga Paket: Rp%.0f\n"+
			"- *Total: Rp%.0f*\n\n"+
			"⏳ *Status: Menunggu Konfirmasi*\n\n"+
			"Tim admin akan segera menghubungi Anda untuk informasi pembayaran. "+
			"Setelah pembayaran dikonfirmasi, paket Anda akan langsung aktif.\n\n"+
			"🌐 %s\n"+
			"🔔 %s Notification System",
		student.Name, mp.ID,
		pkg.Name, instrumentName, pkg.Duration, pkg.Quota, manualFormatExpired(pkg.ExpiredDuration),
		regFeeStr, mp.PackagePrice, mp.TotalAmount,
		"https://www.mdxmusiccourse.cloud/",
		os.Getenv("APP_NAME"),
	)

	if err := s.messenger.SendMessage(normalized, msg); err != nil {
		log.Printf("⚠️ WA student pending notify failed: %v", err)
	}
}

func (s *manualPaymentSvc) notifyStudentConfirmed(student *domain.User, pkg *domain.Package, mp *domain.ManualPayment) {
	if s.messenger == nil || !s.messenger.IsLoggedIn() {
		return
	}
	normalized := utils.NormalizePhoneNumber(student.Phone)
	if normalized == "" {
		return
	}

	msg := fmt.Sprintf(
		"🎉 Halo %s!\n\n"+
			"✅ *Pembayaran Dikonfirmasi!*\n\n"+
			"Pembayaran untuk paket *\"%s\"* (ID: #%d) telah dikonfirmasi oleh admin.\n\n"+
			"Paket Anda sudah aktif dan siap digunakan! Silakan login ke aplikasi untuk mulai booking sesi.\n\n"+
			"📅 Kuota: %d sesi\n"+
			"💰 Total Dibayar: Rp%.0f\n\n"+
			"Selamat belajar! 🎵\n\n"+
			"🌐 %s\n"+
			"🔔 %s Notification System",
		student.Name, pkg.Name, mp.ID, pkg.Quota, mp.TotalAmount,
		"https://www.mdxmusiccourse.cloud/",
		os.Getenv("APP_NAME"),
	)

	if err := s.messenger.SendMessage(normalized, msg); err != nil {
		log.Printf("⚠️ WA student confirm notify failed: %v", err)
	}
}

func (s *manualPaymentSvc) notifyStudentRejected(student *domain.User, pkg *domain.Package, mp *domain.ManualPayment, notes *string) {
	if s.messenger == nil || !s.messenger.IsLoggedIn() {
		return
	}
	normalized := utils.NormalizePhoneNumber(student.Phone)
	if normalized == "" {
		return
	}

	reason := "Tidak ada alasan diberikan"
	if notes != nil && *notes != "" {
		reason = *notes
	}

	msg := fmt.Sprintf(
		"Halo %s,\n\n"+
			"❌ *Permintaan Pembayaran Ditolak*\n\n"+
			"Permintaan Anda untuk paket *\"%s\"* (ID: #%d) tidak dapat dikonfirmasi.\n\n"+
			"*Alasan:* %s\n\n"+
			"Jika ada pertanyaan, silakan hubungi admin kami.\n\n"+
			"🌐 %s\n"+
			"🔔 %s Notification System",
		student.Name, pkg.Name, mp.ID, reason,
		"https://www.mdxmusiccourse.cloud/",
		os.Getenv("APP_NAME"),
	)

	if err := s.messenger.SendMessage(normalized, msg); err != nil {
		log.Printf("⚠️ WA student reject notify failed: %v", err)
	}
}

// manualFormatExpired converts raw days to a human-readable string.
// Named with prefix to avoid collision with payment.go's formatExpired if ever merged.
func manualFormatExpired(days int) string {
	if days <= 0 {
		return "-"
	}
	if days < 30 {
		return fmt.Sprintf("%d Hari", days)
	}
	return fmt.Sprintf("%d Bulan", days/30)
}