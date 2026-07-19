package service

import (
	"chronosphere/config"
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"time"

	xendit "github.com/xendit/xendit-go/v6"
	invoice "github.com/xendit/xendit-go/v6/invoice"
	"gorm.io/gorm"
)

type paymentService struct {
	paymentRepo  domain.PaymentRepository
	adminRepo    domain.AdminRepository
	xenditClient *xendit.APIClient
	db           *gorm.DB
	messenger    *config.WAManager
}

func NewPaymentService(paymentRepo domain.PaymentRepository, adminRepo domain.AdminRepository, db *gorm.DB, mgr *config.WAManager) domain.PaymentUseCase {
	apiKey := os.Getenv("XENDIT_SECRET_KEY")
	if apiKey == "" {
		log.Println("⚠️  XENDIT_SECRET_KEY not set, payment features will not work")
	}
	return &paymentService{
		paymentRepo:  paymentRepo,
		adminRepo:    adminRepo,
		xenditClient: xendit.NewClient(apiKey),
		db:           db,
		messenger:    mgr,
	}
}

func (s *paymentService) BackfillPaymentRecognitions(ctx context.Context) (*domain.BackfillResult, error) {
	result := &domain.BackfillResult{}

	recognizedPayments, err := s.adminRepo.GetExistingRecognitionSourceIDs(ctx, "payment")
	if err != nil {
		return nil, err
	}
	recognizedManual, err := s.adminRepo.GetExistingRecognitionSourceIDs(ctx, "manual_payment")
	if err != nil {
		return nil, err
	}

	pkgCache := make(map[int]*domain.Package)
	getPkg := func(id int) (*domain.Package, error) {
		if p, ok := pkgCache[id]; ok {
			return p, nil
		}
		p, err := s.adminRepo.GetPackagesByID(ctx, id)
		if err != nil {
			return nil, err
		}
		pkgCache[id] = p
		return p, nil
	}

	var newRows []domain.PaymentRecognition

	// ── Xendit payments ──────────────────────────────────────────────
	payments, err := s.paymentRepo.GetAllPaidPayments(ctx)
	if err != nil {
		return nil, err
	}
	result.PaymentsScanned = len(payments)

	for _, p := range payments {
		if recognizedPayments[p.ID] {
			result.Skipped++
			continue
		}
		if p.PaidAt == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("payment #%d: no paid_at, skipped", p.ID))
			continue
		}
		pkg, err := getPkg(p.PackageID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("payment #%d: package lookup failed: %v", p.ID, err))
			continue
		}
		months := pkg.ExpiredDays / 30
		if months <= 0 {
			months = 1
		}
		newRows = append(newRows, BuildRecognitionRows("payment", p.ID, p.StudentUUID, p.PackageID, p.Amount, months, *p.PaidAt)...)
	}

	// ── Manual payments ──────────────────────────────────────────────
	var manualPayments []domain.ManualPayment
	if err := s.db.WithContext(ctx).
		Where("status = ?", domain.ManualPaymentStatusConfirmed).
		Find(&manualPayments).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch confirmed manual payments: %w", err)
	}
	result.ManualPaymentsScanned = len(manualPayments)

	for _, mp := range manualPayments {
		if recognizedManual[mp.ID] {
			result.Skipped++
			continue
		}
		if mp.ConfirmedAt == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("manual_payment #%d: no confirmed_at, skipped", mp.ID))
			continue
		}
		pkg, err := getPkg(mp.PackageID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("manual_payment #%d: package lookup failed: %v", mp.ID, err))
			continue
		}
		months := pkg.ExpiredDays / 30
		if months <= 0 {
			months = 1
		}
		newRows = append(newRows, BuildRecognitionRows("manual_payment", mp.ID, mp.StudentUUID, mp.PackageID, mp.TotalAmount, months, *mp.ConfirmedAt)...)
	}

	if err := s.adminRepo.CreateRecognitionRows(ctx, newRows); err != nil {
		return nil, err
	}
	result.RowsCreated = len(newRows)

	return result, nil
}

func (s *paymentService) GetRecognitionRows(ctx context.Context, filter domain.RecognitionRowFilter) ([]domain.RecognitionRowDetail, int64, error) {
	return s.adminRepo.GetRecognitionRows(ctx, filter)
}

func (s *paymentService) CreateInvoice(ctx context.Context, studentUUID string, packageID int) (*domain.Payment, error) {
	student, err := s.adminRepo.GetStudentByUUID(ctx, studentUUID)
	if err != nil {
		return nil, fmt.Errorf("siswa tidak ditemukan: %w", err)
	}

	pkg, err := s.adminRepo.GetPackagesByID(ctx, packageID)
	if err != nil {
		return nil, fmt.Errorf("paket tidak ditemukan: %w", err)
	}

	if pkg.IsTrial {
		var trialCount int64
		err = s.db.WithContext(ctx).
			Table("payments").
			Joins("JOIN packages ON packages.id = payments.package_id").
			Where("payments.student_uuid = ?", studentUUID).
			Where("payments.status = ?", domain.PaymentStatusPaid).
			Where("packages.is_trial = true").
			Count(&trialCount).Error
		if err != nil {
			return nil, fmt.Errorf("gagal memeriksa riwayat paket trial: %w", err)
		}
		if trialCount > 0 {
			return nil, fmt.Errorf("kamu sudah pernah membeli paket trial, paket ini hanya bisa dibeli satu kali")
		}
	}

	setting, err := s.adminRepo.GetSetting(ctx)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil pengaturan biaya: %w", err)
	}

	// Check all three non-trial purchase sources — Xendit, manual payment, and
	// directly assigned packages. Any confirmed prior purchase means no reg fee.
	var priorNonTrialCount int64
	err = s.db.WithContext(ctx).Raw(`
    SELECT COUNT(*) FROM (
        SELECT payments.id
        FROM payments
        JOIN packages ON packages.id = payments.package_id
        WHERE payments.student_uuid = ?
          AND payments.status       = ?
          AND packages.is_trial     = false
        UNION ALL
        SELECT manual_payments.id
        FROM manual_payments
        JOIN packages ON packages.id = manual_payments.package_id
        WHERE manual_payments.student_uuid = ?
          AND manual_payments.status       = ?
          AND packages.is_trial            = false
    ) combined
`,
		studentUUID, domain.PaymentStatusPaid,
		studentUUID, domain.ManualPaymentStatusConfirmed,
	).Scan(&priorNonTrialCount).Error
	if err != nil {
		return nil, fmt.Errorf("gagal memeriksa riwayat pembayaran: %w", err)
	}

	isFirstPurchase := priorNonTrialCount == 0

	pkgPrice := pkg.Price
	if pkg.IsPromoActive && pkg.PromoPrice > 0 {
		pkgPrice = pkg.PromoPrice
	}

	var totalAmount float64
	var items []invoice.InvoiceItem

	switch {
	case pkg.IsTrial:
		totalAmount = pkgPrice
		items = []invoice.InvoiceItem{
			*invoice.NewInvoiceItem(fmt.Sprintf("Paket Trial %s", pkg.Name), float32(pkgPrice), 1),
		}
	case isFirstPurchase:
		totalAmount = setting.RegistrationFee + pkgPrice
		items = []invoice.InvoiceItem{
			*invoice.NewInvoiceItem("Biaya Pendaftaran", float32(setting.RegistrationFee), 1),
			*invoice.NewInvoiceItem(fmt.Sprintf("Paket %s (%dx pertemuan)", pkg.Name, pkg.Quota), float32(pkgPrice), 1),
		}
	default:
		totalAmount = pkgPrice
		items = []invoice.InvoiceItem{
			*invoice.NewInvoiceItem(fmt.Sprintf("Paket %s (%dx pertemuan)", pkg.Name, pkg.Quota), float32(pkgPrice), 1),
		}
	}

	shortUUID := studentUUID
	if len(shortUUID) > 8 {
		shortUUID = shortUUID[:8]
	}
	externalID := fmt.Sprintf("MDX-%s-%d", shortUUID, time.Now().UnixMilli())
	description := fmt.Sprintf("Pembayaran Paket %s - %s", pkg.Name, student.Name)

	siteURL := os.Getenv("NEXT_PUBLIC_SITE_URL")
	if siteURL == "" {
		siteURL = "http://localhost:3000"
	}
	successURL := fmt.Sprintf("%s/dashboard/panel/student/payment/success", siteURL)
	failureURL := fmt.Sprintf("%s/dashboard/panel/student/payment/failed", siteURL)

	customer := invoice.CustomerObject{}
	customer.GivenNames = *invoice.NewNullableString(&student.Name)
	customer.Email = *invoice.NewNullableString(&student.Email)
	if student.Phone != "" {
		customer.MobileNumber = *invoice.NewNullableString(&student.Phone)
	}

	currency := "IDR"
	locale := "id"
	shouldSendEmail := true
	invoiceDuration := "86400"

	createReq := *invoice.NewCreateInvoiceRequest(externalID, totalAmount)
	createReq.Description = &description
	createReq.PayerEmail = &student.Email
	createReq.Currency = &currency
	createReq.Locale = &locale
	createReq.ShouldSendEmail = &shouldSendEmail
	createReq.InvoiceDuration = &invoiceDuration
	createReq.SuccessRedirectUrl = &successURL
	createReq.FailureRedirectUrl = &failureURL
	createReq.Items = items
	createReq.Customer = &customer
	createReq.Metadata = map[string]interface{}{
		"student_uuid": studentUUID,
		"package_id":   packageID,
	}

	inv, _, xenditErr := s.xenditClient.InvoiceApi.CreateInvoice(ctx).
		CreateInvoiceRequest(createReq).
		Execute()
	if xenditErr != nil {
		return nil, fmt.Errorf("gagal membuat invoice pembayaran: %v", xenditErr)
	}

	invoiceID := ""
	if inv.Id != nil {
		invoiceID = *inv.Id
	}

	payment := &domain.Payment{
		StudentUUID:     studentUUID,
		PackageID:       packageID,
		XenditInvoiceID: invoiceID,
		ExternalID:      externalID,
		Amount:          totalAmount,
		Status:          domain.PaymentStatusPending,
		InvoiceURL:      inv.InvoiceUrl,
	}

	if err := s.paymentRepo.CreatePayment(ctx, payment); err != nil {
		return nil, fmt.Errorf("gagal menyimpan data pembayaran: %w", err)
	}

	log.Printf("✅ Invoice created: %s | Amount: %.0f | First: %v | Student: %s",
		externalID, totalAmount, isFirstPurchase, student.Name)
	return payment, nil
}

func (s *paymentService) GetPaymentByExternalID(ctx context.Context, externalID string) (*domain.Payment, error) {
	return s.paymentRepo.GetPaymentByExternalID(ctx, externalID)
}

func (s *paymentService) GetInvoicePDF(ctx context.Context, externalID string, requesterUUID string, isAdmin bool) ([]byte, error) {
	payment, err := s.paymentRepo.GetPaymentByExternalID(ctx, externalID)
	if err != nil {
		return nil, fmt.Errorf("invoice tidak ditemukan: %w", err)
	}
	if payment.Status != domain.PaymentStatusPaid {
		return nil, fmt.Errorf("invoice hanya tersedia untuk pembayaran yang sudah lunas")
	}
	if !isAdmin && payment.StudentUUID != requesterUUID {
		return nil, fmt.Errorf("kamu tidak memiliki akses ke invoice ini")
	}

	student, err := s.adminRepo.GetStudentByUUID(ctx, payment.StudentUUID)
	if err != nil {
		return nil, fmt.Errorf("data siswa tidak ditemukan: %w", err)
	}
	pkg, err := s.adminRepo.GetPackagesByID(ctx, payment.PackageID)
	if err != nil {
		return nil, fmt.Errorf("data paket tidak ditemukan: %w", err)
	}

	return GenerateInvoicePDF(payment, student, pkg)
}

func (s *paymentService) HandleWebhook(ctx context.Context, payload domain.XenditWebhookPayload) error {
	payment, err := s.paymentRepo.GetPaymentByExternalID(ctx, payload.ExternalID)
	if err != nil {
		return fmt.Errorf("payment tidak ditemukan: %w", err)
	}

	if payment.Status == domain.PaymentStatusPaid {
		return nil
	}

	switch payload.Status {
	case "PAID", "SETTLED":
		txErr := s.db.Transaction(func(tx *gorm.DB) error {
			var paidAt *time.Time
			if payload.PaidAt != "" {
				t, parseErr := time.Parse(time.RFC3339, payload.PaidAt)
				if parseErr == nil {
					paidAt = &t
				}
			}
			if paidAt == nil {
				now := time.Now()
				paidAt = &now
			}

			method := &payload.PaymentMethod
			if *method == "" {
				method = nil
			}

			if err := s.paymentRepo.UpdatePaymentStatus(ctx, payload.ExternalID, domain.PaymentStatusPaid, method, paidAt); err != nil {
				return err
			}

			if err := s.autoAssignPackage(ctx, payment.StudentUUID, payment.PackageID, payment.Amount, "payment", payment.ID); err != nil {
				log.Printf("⚠️  Webhook auto-assign failed (admin can assign manually): %v", err)
			}
			return nil
		})
		if txErr != nil {
			return txErr
		}

		log.Printf("✅ Payment completed: %s | Student: %s", payload.ExternalID, payment.StudentUUID)

		if s.messenger != nil && s.messenger.IsLoggedIn() {
			student, err := s.adminRepo.GetStudentByUUID(context.Background(), payment.StudentUUID)
			if err != nil {
				log.Printf("⚠️  WA payment notify: student lookup failed: %v", err)
				return nil
			}
			pkg, err := s.adminRepo.GetPackagesByID(context.Background(), payment.PackageID)
			if err != nil {
				log.Printf("⚠️  WA payment notify: package lookup failed: %v", err)
				return nil
			}
			s.sendPaymentSuccessNotification(student, pkg)
		}

	case "EXPIRED":
		if err := s.paymentRepo.UpdatePaymentStatus(ctx, payload.ExternalID, domain.PaymentStatusExpired, nil, nil); err != nil {
			return err
		}
		log.Printf("⏰ Payment expired: %s", payload.ExternalID)

	default:
		log.Printf("ℹ️  Webhook: unhandled status %s for %s", payload.Status, payload.ExternalID)
	}

	return nil
}

func BuildRecognitionRows(sourceType string, sourceID int, studentUUID string, packageID int,
	total float64, months int, start time.Time) []domain.PaymentRecognition {

	base := math.Floor(total / float64(months) / 1) // round down to whole rupiah
	rows := make([]domain.PaymentRecognition, months)
	running := 0.0
	for i := 0; i < months; i++ {
		d := start.AddDate(0, i, 0)
		amt := base
		if i == months-1 {
			amt = total - running // last row absorbs the rounding remainder
		}
		running += amt
		rows[i] = domain.PaymentRecognition{
			SourceType: sourceType, SourceID: sourceID,
			StudentUUID: studentUUID, PackageID: packageID,
			PeriodYear: d.Year(), PeriodMonth: int(d.Month()), Amount: amt,
		}
	}
	return rows
}

func (s *paymentService) autoAssignPackage(ctx context.Context, studentUUID string, packageID int, amount float64, sourceType string, sourceID int) error {
	if _, _, err := s.adminRepo.AssignPackageToStudent(ctx, studentUUID, packageID); err != nil {
		return fmt.Errorf("gagal mengaktifkan paket: %w", err)
	}
	log.Printf("✅ Auto-assigned package %d to student %s", packageID, studentUUID)

	pkg, err := s.adminRepo.GetPackagesByID(ctx, packageID)
	if err != nil {
		log.Printf("⚠️ recognition rows skipped, package lookup failed: %v", err)
		return nil // package is already assigned — don't fail the payment over a reporting side-effect
	}
	months := pkg.ExpiredDays / 30
	if months <= 0 {
		months = 1
	}

	rows := BuildRecognitionRows(sourceType, sourceID, studentUUID, packageID, amount, months, time.Now())
	if err := s.adminRepo.CreateRecognitionRows(ctx, rows); err != nil {
		log.Printf("⚠️ failed to write recognition rows for %s #%d: %v", sourceType, sourceID, err)
	}
	return nil
}

func (s *paymentService) GetPaymentsByStudent(ctx context.Context, studentUUID string) ([]domain.Payment, error) {
	return s.paymentRepo.GetPaymentsByStudent(ctx, studentUUID)
}

func (s *paymentService) GetTotalProfit(ctx context.Context, filter domain.ProfitFilter) (float64, error) {
	return s.paymentRepo.GetTotalProfit(ctx, filter)
}

func (s *paymentService) GetPaymentHistory(ctx context.Context, filter domain.HistoryFilter) ([]domain.Payment, int64, error) {
	return s.paymentRepo.GetPaymentHistory(ctx, filter)
}

func (s *paymentService) GetMonthlyRecognizedRevenue(ctx context.Context, filter domain.MonthlyRevenueFilter) ([]domain.MonthlyRevenue, error) {
	return s.paymentRepo.GetMonthlyRecognizedRevenue(ctx, filter)
}

func (s *paymentService) GetPackageSummary(ctx context.Context) ([]domain.PackageSummary, error) {
	return s.paymentRepo.GetPackageSummary(ctx)
}

func (s *paymentService) sendPaymentSuccessNotification(student *domain.User, pkg *domain.Package) {
	phone := utils.NormalizePhoneNumber(student.Phone)
	if phone == "" {
		return
	}

	msg := fmt.Sprintf(
		`🎉 *Halo %s!*

✅ *Pembayaran Berhasil!*
Paket *"%s"* kamu sudah aktif dan siap digunakan.

📦 *Detail Paket:*
┣ 📚 Nama Paket: %s
┣ 🎯 Jumlah Kelas: %d sesi
┗ ⏳ Masa Aktif: %d hari

✨ *Apa yang bisa kamu lakukan sekarang?*
• 📅 Pesan kelas dengan guru favoritmu
• 📖 Mulai belajar dan raih prestasi
• 🏆 Pantau progress belajarmu

🚀 *Mulai belajar sekarang:*
🔗 https://mdxmusiccourse.cloud

Terima kasih telah memilih MDX! 🌟`,
		student.Name, pkg.Name, pkg.Name, pkg.Quota, pkg.ExpiredDays,
	)

	mgr := s.messenger
	go func() {
		if err := mgr.SendMessage(phone, msg); err != nil {
			log.Printf("🔕 WA payment success to %s failed: %v", phone, err)
		} else {
			log.Printf("🔔 WA payment success sent to: %s", student.Name)
		}
	}()
}