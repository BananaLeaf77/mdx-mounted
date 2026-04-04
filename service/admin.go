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

	"golang.org/x/crypto/bcrypt"
)

type adminService struct {
	adminRepo domain.AdminRepository
	messenger *config.WAManager
}

func NewAdminService(adminRepo domain.AdminRepository, mgr *config.WAManager) domain.AdminUseCase {
	return &adminService{
		adminRepo: adminRepo,
		messenger: mgr,
	}
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

	if s.messenger != nil && s.messenger.IsLoggedIn() {
		phoneNormalized := utils.NormalizePhoneNumber(dataStudent.Phone)
		if phoneNormalized != "" {
			msgToStudent := fmt.Sprintf(
				"Halo %s,\n\nPaket les Anda telah aktif!\n• Paket: %s (%s)\n• Instrument: %s\n• Kuota: %d sesi\n\nSilakan login ke aplikasi untuk mulai booking sesi.\n\nTerima kasih,\n\n🌐 Website: %s\n🔔 %s Notification System\n",
				dataStudent.Name,
				dataPackage.Name,
				dataPackage.Description,
				dataPackage.Instrument.Name,
				dataPackage.Quota,
				"https://www.madeu.app",
				os.Getenv("APP_NAME"),
			)
			if !s.messenger.IsLoggedIn() {
				log.Printf("🔕 WhatsApp not connected, skipping cancel notification")
				return nil
			}
			go func() {
				if err := s.messenger.SendMessage(phoneNormalized, msgToStudent); err != nil {
					log.Printf("🔕 WA to student %s failed: %v", phoneNormalized, err)
				}
			}()
		}
	}
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
	if !s.messenger.IsLoggedIn() {
		return errors.New("whatsapp tidak terhubung")
	}
	normalized := utils.NormalizePhoneNumber(phone)
	if normalized == "" {
		return errors.New("nomor telepon tidak valid")
	}
	return s.messenger.SendMessage(normalized, "Ping dari sistem MadEU. Tes koneksi berhasil.")
}
