package service

import (
	"chronosphere/config"
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"fmt"
	"log"
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

	var priorPaidCount int64
	err = s.db.WithContext(ctx).
		Table("payments").
		Joins("JOIN packages ON packages.id = payments.package_id").
		Where("payments.student_uuid = ?", studentUUID).
		Where("payments.status = ?", domain.PaymentStatusPaid).
		Where("packages.is_trial = false").
		Count(&priorPaidCount).Error
	if err != nil {
		return nil, fmt.Errorf("gagal memeriksa riwayat pembayaran: %w", err)
	}

	isFirstPurchase := priorPaidCount == 0

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
	externalID := fmt.Sprintf("MADEU-%s-%d", shortUUID, time.Now().UnixMilli())
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

			if err := s.autoAssignPackage(ctx, payment.StudentUUID, payment.PackageID); err != nil {
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

func (s *paymentService) autoAssignPackage(ctx context.Context, studentUUID string, packageID int) error {
	_, _, err := s.adminRepo.AssignPackageToStudent(ctx, studentUUID, packageID)
	if err != nil {
		return fmt.Errorf("gagal mengaktifkan paket: %w", err)
	}
	log.Printf("✅ Auto-assigned package %d to student %s", packageID, studentUUID)
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
🔗 https://madeu.app

Terima kasih telah memilih MadEU! 🌟`,
		student.Name, pkg.Name, pkg.Name, pkg.Quota, pkg.ExpiredDuration,
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
