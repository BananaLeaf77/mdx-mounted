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
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"
	"github.com/xuri/excelize/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type adminService struct {
	db        *gorm.DB
	adminRepo domain.AdminRepository
	messenger *config.WAManager
}

func NewAdminService(adminRepo domain.AdminRepository, mgr *config.WAManager, database *gorm.DB) domain.AdminUseCase {
	return &adminService{
		adminRepo: adminRepo,
		messenger: mgr,
		db:        database,
	}
}

func (s *adminService) ExportRecognitionRows(ctx context.Context, filter domain.RecognitionRowFilter) ([]byte, error) {
	rows, err := s.adminRepo.GetAllRecognitionRowsForExport(ctx, filter)
	if err != nil {
		return nil, err
	}

	f := excelize.NewFile()
	defer f.Close()

	sheet := "Pengakuan Pendapatan"
	f.SetSheetName("Sheet1", sheet)

	headers := []string{"Siswa", "Paket", "Periode", "Amount", "Metode Pembayaran", "Source", "Paket Aktif"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
	}
	headerStyle, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	f.SetCellStyle(sheet, "A1", "G1", headerStyle)

	for i, row := range rows {
		r := i + 2
		period := fmt.Sprintf("%04d-%02d", row.PeriodYear, row.PeriodMonth)
		source := "MANUAL"
		if row.SourceType == "payment" {
			source = "XENDIT"
		}

		f.SetCellValue(sheet, fmt.Sprintf("A%d", r), row.StudentName)
		f.SetCellValue(sheet, fmt.Sprintf("B%d", r), row.PackageName)
		f.SetCellValue(sheet, fmt.Sprintf("C%d", r), period)
		f.SetCellValue(sheet, fmt.Sprintf("D%d", r), row.Amount)
		f.SetCellValue(sheet, fmt.Sprintf("E%d", r), row.PaymentMethod)
		f.SetCellValue(sheet, fmt.Sprintf("F%d", r), source)
		f.SetCellValue(sheet, fmt.Sprintf("G%d", r), row.CreatedAt.Format("02 Jan 2006"))
	}

	f.SetColWidth(sheet, "A", "B", 25)
	f.SetColWidth(sheet, "C", "C", 12)
	f.SetColWidth(sheet, "D", "D", 15)
	f.SetColWidth(sheet, "E", "G", 18)

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("gagal membuat file excel: %w", err)
	}

	return buf.Bytes(), nil
}

func (s *adminService) GetWhatsAppWarmupStatus(ctx context.Context, userUUID string) (bool, error) {
	var user domain.User
	if err := s.db.WithContext(ctx).
		Select("phone").
		Where("uuid = ?", userUUID).
		First(&user).Error; err != nil {
		return false, err
	}

	normalized := utils.NormalizePhoneNumber(user.Phone)
	if normalized == "" {
		return false, errors.New("nomor telepon tidak valid")
	}

	warmed, err := s.messenger.HasPrivacyToken(normalized)
	if err != nil {
		return false, err
	}

	if warmed {
		now := time.Now()
		_ = s.db.WithContext(ctx).Model(&domain.User{}).
			Where("uuid = ? AND whatsapp_warmed_at IS NULL", userUUID).
			Update("whatsapp_warmed_at", &now).Error
	}

	return warmed, nil
}

func (s *adminService) TogglePackageActive(ctx context.Context, id int, isActive bool) error {
	if id <= 0 {
		return fmt.Errorf("ID paket tidak valid")
	}
	return s.adminRepo.TogglePackageActive(ctx, id, isActive)
}

func (s *adminService) AssignPackageToStudentManual(ctx context.Context, studentUUID string, packageID int, recordData *bool, proofImageURL *string, notes *string) (*domain.User, *domain.Package, error) {
	return s.adminRepo.AssignPackageToStudentManual(ctx, studentUUID, packageID, recordData, proofImageURL, notes)
}

func (s *adminService) GetTeacherAllClasses(ctx context.Context, teacherUUID string, f domain.PaginationFilter) (*[]domain.Booking, error) {
	if teacherUUID == "" {
		return nil, fmt.Errorf("UUID guru tidak boleh kosong")
	}
	return s.adminRepo.GetTeacherAllClasses(ctx, teacherUUID, f)
}

func (s *adminService) GetBookedClasses(ctx context.Context) ([]domain.Booking, error) {
	return s.adminRepo.GetBookedClasses(ctx)
}

func (s *adminService) UpdateAdmin(ctx context.Context, payload domain.User) error {
	if err := s.adminRepo.UpdateAdmin(ctx, payload); err != nil {
		return errors.New(utils.TranslateDBError(err))
	}
	return nil
}

func (s *adminService) ClearUserDeletedAt(ctx context.Context, userUUID string) error {
	return s.adminRepo.ClearUserDeletedAt(ctx, userUUID)
}

func (s *adminService) GetAllClassHistories(ctx context.Context) (*[]domain.ClassHistory, error) {
	return s.adminRepo.GetAllClassHistories(ctx)
}

func (s *adminService) GetAllManagers(ctx context.Context) ([]domain.User, error) {
	return s.adminRepo.GetAllManagers(ctx)
}

func (s *adminService) CreateManager(ctx context.Context, user *domain.User) (*domain.User, error) {
	if user.Name == "" || user.Email == "" || user.Phone == "" || user.Password == "" {
		return nil, errors.New("semua field wajib diisi")
	}
	user.Role = domain.RoleManagement

	hashed, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.New("gagal mengenkripsi password")
	}
	user.Password = string(hashed)

	created, err := s.adminRepo.CreateManager(ctx, user)
	if err != nil {
		return nil, errors.New(utils.TranslateDBError(err))
	}
	return created, nil
}

func (s *adminService) UpdateManager(ctx context.Context, user *domain.User) error {
	if user.UUID == "" {
		return errors.New("uuid tidak boleh kosong")
	}
	if err := s.adminRepo.UpdateManager(ctx, user); err != nil {
		return errors.New(utils.TranslateDBError(err))
	}
	return nil
}

func (s *adminService) GetAllManager(ctx context.Context) ([]domain.User, error) {
	return s.adminRepo.GetAllManagers(ctx)
}

func (s *adminService) GetManagerByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	if uuid == "" {
		return nil, errors.New("uuid tidak boleh kosong")
	}
	return s.adminRepo.GetManagerByUUID(ctx, uuid)
}

func (s *adminService) GetPackagesByID(ctx context.Context, id int) (*domain.Package, error) {
	if id <= 0 {
		return nil, errors.New("ID paket tidak valid")
	}
	return s.adminRepo.GetPackagesByID(ctx, id)
}

func (s *adminService) AssignPackageToStudent(ctx context.Context, studentUUID string, packageID int) error {
	if studentUUID == "" {
		return errors.New("UUID siswa wajib diisi")
	}
	if packageID <= 0 {
		return errors.New("ID paket tidak valid")
	}

	dataStudent, dataPackage, err := s.adminRepo.AssignPackageToStudent(ctx, studentUUID, packageID)
	if err != nil {
		return err
	}

	instrumentName := "-"
	if dataPackage.Instrument != nil {
		instrumentName = dataPackage.Instrument.Name
	}

	if s.messenger == nil || !s.messenger.IsLoggedIn() {
		msg := fmt.Sprintf(
			"🔕 *Paket Diaktifkan*\nSiswa: %s\nPaket: %s\n\nNotifikasi WA dilewati (tidak terhubung)",
			dataStudent.Name, dataPackage.Name,
		)
		if telegramErr := utils.NotifyTelegram(msg); telegramErr != nil {
			zlog.Warn().Msg(fmt.Sprintf("failed to send telegram assign-package notif: %v", telegramErr))
		}
		return nil
	}

	phoneNormalized := utils.NormalizePhoneNumber(dataStudent.Phone)
	if phoneNormalized == "" {
		zlog.Warn().Msg(fmt.Sprintf("AssignPackageToStudent: invalid phone for student %s, skipping WA", dataStudent.Name))
		return nil
	}

	msgToStudent := fmt.Sprintf(
		"Halo %s,\n\nPaket les Anda telah aktif!\n• Paket: %s (%s)\n• Instrument: %s\n• Kuota: %d sesi\n\nSilakan login ke aplikasi untuk mulai booking sesi.\n\nTerima kasih,\n\n🌐 Website: %s\n🔔 %s Notification System\n",
		dataStudent.Name,
		dataPackage.Name,
		dataPackage.Description,
		instrumentName,
		dataPackage.Quota,
		"https://www.mdxmusiccourse.cloud/",
		os.Getenv("APP_NAME"),
	)

	go func() {
		waStatus := "✅ Terkirim"
		if err := s.messenger.SendMessage(phoneNormalized, msgToStudent); err != nil {
			zlog.Warn().Msg(fmt.Sprintf("WA to student %s failed: %v", phoneNormalized, err))
			waStatus = fmt.Sprintf("❌ Gagal: %v", err)
		}

		summary := fmt.Sprintf(
			"📦 *Paket Diaktifkan*\nSiswa: %s\nPaket: %s\nWA: %s",
			dataStudent.Name, dataPackage.Name, waStatus,
		)
		if telegramErr := utils.NotifyTelegram(summary); telegramErr != nil {
			zlog.Warn().Msg(fmt.Sprintf("failed to send telegram assign-package notif: %v", telegramErr))
		}
	}()

	return nil
}
func (s *adminService) CreatePackage(ctx context.Context, pkg *domain.Package) (*domain.Package, error) {
	if pkg == nil {
		return nil, errors.New("paket tidak boleh kosong")
	}
	return s.adminRepo.CreatePackage(ctx, pkg)
}

func (s *adminService) UpdatePackage(ctx context.Context, pkg *domain.Package) error {
	if pkg == nil {
		return errors.New("paket tidak boleh kosong")
	}
	return s.adminRepo.UpdatePackage(ctx, pkg)
}

func (s *adminService) DeletePackage(ctx context.Context, id int) error {
	if id <= 0 {
		return errors.New("ID paket tidak valid")
	}
	return s.adminRepo.DeletePackage(ctx, id)
}

func (s *adminService) CreateInstrument(ctx context.Context, instrument *domain.Instrument) (*domain.Instrument, error) {
	if instrument == nil {
		return nil, errors.New("instrumen tidak boleh kosong")
	}
	if instrument.Name == "" {
		return nil, errors.New("nama instrumen tidak boleh kosong")
	}
	return s.adminRepo.CreateInstrument(ctx, instrument)
}

func (s *adminService) UpdateInstrument(ctx context.Context, instrument *domain.Instrument) error {
	if instrument == nil {
		return errors.New("instrumen tidak boleh kosong")
	}
	return s.adminRepo.UpdateInstrument(ctx, instrument)
}

func (s *adminService) DeleteInstrument(ctx context.Context, id int) error {
	if id <= 0 {
		return errors.New("ID instrumen tidak valid")
	}
	return s.adminRepo.DeleteInstrument(ctx, id)
}

func (s *adminService) GetAllPackages(ctx context.Context) ([]domain.Package, error) {
	return s.adminRepo.GetAllPackages(ctx)
}

func (s *adminService) GetAllInstruments(ctx context.Context) ([]domain.Instrument, error) {
	return s.adminRepo.GetAllInstruments(ctx)
}

func (s *adminService) GetAllStudents(ctx context.Context) ([]domain.User, error) {
	return s.adminRepo.GetAllStudents(ctx)
}

func (s *adminService) GetFilteredStudents(ctx context.Context, filter domain.StudentActivityFilter) ([]domain.User, error) {
	return s.adminRepo.GetFilteredStudents(ctx, filter)
}

func (s *adminService) GetAllUsers(ctx context.Context) ([]domain.User, error) {
	return s.adminRepo.GetAllUsers(ctx)
}

func (s *adminService) GetStudentByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	if uuid == "" {
		return nil, errors.New("UUID wajib diisi")
	}
	return s.adminRepo.GetStudentByUUID(ctx, uuid)
}

func (s *adminService) CreateStudent(ctx context.Context, user *domain.User) (*domain.User, error) {
	if user.Name == "" || user.Email == "" || user.Phone == "" || user.Password == "" {
		return nil, errors.New("semua field wajib diisi")
	}
	user.Role = domain.RoleStudent

	hashed, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.New("gagal mengenkripsi password")
	}
	user.Password = string(hashed)

	created, err := s.adminRepo.CreateStudent(ctx, user)
	if err != nil {
		return nil, errors.New(utils.TranslateDBError(err))
	}
	return created, nil
}

func (s *adminService) CreateTeacher(ctx context.Context, user *domain.User, instrumentIDs []int) (*domain.User, error) {
	if user.Name == "" || user.Email == "" || user.Phone == "" || user.Password == "" {
		return nil, errors.New("semua field wajib diisi")
	}
	user.Role = domain.RoleTeacher

	hashed, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.New("gagal mengenkripsi password")
	}
	user.Password = string(hashed)

	created, err := s.adminRepo.CreateTeacher(ctx, user, instrumentIDs)
	if err != nil {
		return nil, errors.New(utils.TranslateDBError(err))
	}
	return created, nil
}

func (s *adminService) UpdateTeacher(ctx context.Context, user *domain.User, instrumentIDs []int) error {
	if user.UUID == "" {
		return errors.New("uuid teacher tidak boleh kosong")
	}
	if err := s.adminRepo.UpdateTeacher(ctx, user, instrumentIDs); err != nil {
		return errors.New(utils.TranslateDBError(err))
	}
	return nil
}

func (s *adminService) GetAllTeachers(ctx context.Context) ([]domain.User, error) {
	teachers, err := s.adminRepo.GetAllTeachers(ctx)
	if err != nil {
		return nil, errors.New(utils.TranslateDBError(err))
	}
	return teachers, nil
}

func (s *adminService) GetTeacherByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	if uuid == "" {
		return nil, errors.New("uuid tidak boleh kosong")
	}
	return s.adminRepo.GetTeacherByUUID(ctx, uuid)
}

func (s *adminService) DeleteUser(ctx context.Context, uuid string) error {
	if uuid == "" {
		return errors.New("uuid tidak boleh kosong")
	}
	if err := s.adminRepo.DeleteUser(ctx, uuid); err != nil {
		return errors.New(utils.TranslateDBError(err))
	}
	return nil
}

func (s *adminService) GetSetting(ctx context.Context) (*domain.Setting, error) {
	return s.adminRepo.GetSetting(ctx)
}

func (s *adminService) UpdateSetting(ctx context.Context, setting *domain.Setting) error {
	if setting == nil {
		return errors.New("pengaturan tidak valid")
	}
	return s.adminRepo.UpdateSetting(ctx, setting)
}

// ─── WhatsApp management ─────────────────────────────────────────────────────

// GetWhatsAppStatus improved version
func (s *adminService) GetWhatsAppStatus(_ context.Context) (map[string]interface{}, error) {
	if s.messenger == nil {
		return map[string]interface{}{
			"status": "not_initialized",
			"error":  "WhatsApp manager not configured",
		}, nil
	}

	status := s.messenger.GetStatus()
	result := map[string]interface{}{
		"status": string(status),
		"jid":    s.messenger.GetJID(),
	}

	// Only include QR code when actively waiting for pairing
	if status == config.WAStatusWaitingQR {
		if qr := s.messenger.GetQRCode(); qr != "" {
			result["qr_code"] = qr
			log.Println("📱 QR Code length:", len(qr))
		} else {
			result["message"] = "Waiting for QR code generation..."
		}
	}

	return result, nil
}

// ConnectWhatsApp improved version
func (s *adminService) ConnectWhatsApp(_ context.Context) (map[string]interface{}, error) {
	if s.messenger == nil {
		return nil, errors.New("whatsapp manager tidak diinisialisasi")
	}

	status := s.messenger.GetStatus()

	switch status {
	case config.WAStatusConnected:
		return map[string]interface{}{
			"status":  "already_connected",
			"jid":     s.messenger.GetJID(),
			"message": "WhatsApp sudah terhubung",
		}, nil
	case config.WAStatusConnecting, config.WAStatusWaitingQR:
		return map[string]interface{}{
			"status":  "connecting",
			"message": "Koneksi WhatsApp sedang berlangsung, silakan cek status",
		}, nil
	default:
		// Start new connection
		go func() {
			if err := s.messenger.Connect(); err != nil {
				log.Printf("⚠️ WhatsApp Connect() error: %v", err)
			}
		}()

		return map[string]interface{}{
			"status":  "connecting",
			"message": "Memulai koneksi WhatsApp. Poll status untuk mendapatkan QR code.",
		}, nil
	}
}

// DisconnectWhatsApp clears the session (full logout) then immediately
// starts a fresh connect so a new QR is ready without a second API call.
// admin.go - DisconnectWhatsApp
func (s *adminService) DisconnectWhatsApp(ctx context.Context) error {
	if s.messenger == nil {
		return errors.New("whatsapp manager tidak diinisialisasi")
	}

	// Call Logout which properly clears the session from database
	if err := s.messenger.Logout(ctx); err != nil {
		log.Printf("⚠️ WhatsApp logout warning: %v", err)
		// Continue even if logout fails - we want to force disconnect
	}

	// Don't auto-reconnect! Let user manually connect when ready
	return nil
}

func (s *adminService) PingWhatsApp(_ context.Context, phone string) error {
	if s.messenger.GetStatus() != config.WAStatusConnected {
		return errors.New("whatsapp tidak terhubung")
	}
	normalized := utils.NormalizePhoneNumber(phone)
	if normalized == "" {
		return errors.New("nomor telepon tidak valid")
	}
	return s.messenger.SendMessage(normalized, "Ping dari sistem MDX. Tes koneksi berhasil.")
}

func (s *adminService) GetAdminWhatsAppNumber(_ context.Context) (*domain.WANumberInfo, error) {
	loggedIn := s.messenger.IsLoggedIn()
	if !loggedIn {
		// give a brief self-heal window before giving up — mirrors the
		// reconnect-wait behavior in SendMessage, since IsLoggedIn() can
		// trip on a momentary stale-state blip that resolves itself.
		for i := 0; i < 10; i++ {
			time.Sleep(200 * time.Millisecond)
			if s.messenger.IsLoggedIn() {
				loggedIn = true
				break
			}
		}
	}
	if !loggedIn {
		return nil, errors.New("whatsapp admin belum login")
	}

	jid := s.messenger.GetJID()
	if jid == "" {
		return nil, errors.New("nomor whatsapp admin tidak ditemukan")
	}

	raw := jid
	if i := strings.Index(raw, "@"); i != -1 {
		raw = raw[:i]
	}
	if i := strings.Index(raw, ":"); i != -1 {
		raw = raw[:i]
	}
	if raw == "" {
		return nil, errors.New("nomor whatsapp admin tidak valid")
	}

	local := raw
	if strings.HasPrefix(raw, "62") {
		local = "0" + raw[2:]
	}

	return &domain.WANumberInfo{
		Raw:    raw,
		Local:  local,
		WALink: fmt.Sprintf("https://wa.me/%s", raw),
	}, nil
}

func (s *adminService) GetWhatsAppWarmupInfo(ctx context.Context, userUUID string) (*domain.WAWarmupInfo, error) {
	var user domain.User
	if err := s.db.WithContext(ctx).
		Select("phone").
		Where("uuid = ?", userUUID).
		First(&user).Error; err != nil {
		return nil, err
	}

	normalized := utils.NormalizePhoneNumber(user.Phone)
	if normalized == "" {
		return nil, errors.New("nomor telepon tidak valid")
	}

	warmed, err := s.messenger.HasPrivacyToken(normalized)
	if err != nil {
		return nil, err
	}

	info := &domain.WAWarmupInfo{Warmed: warmed}

	adminNumber, err := s.GetAdminWhatsAppNumber(ctx)
	if err != nil {
		log.Printf("⚠️ GetWhatsAppWarmupInfo: failed to fetch admin number: %v", err)
	} else {
		info.AdminNumber = adminNumber
	}

	if warmed {
		now := time.Now()
		_ = s.db.WithContext(ctx).Model(&domain.User{}).
			Where("uuid = ? AND whatsapp_warmed_at IS NULL", userUUID).
			Update("whatsapp_warmed_at", &now).Error
	}

	return info, nil
}
