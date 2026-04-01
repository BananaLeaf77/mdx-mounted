package service

import (
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"errors"
	"fmt"
	"os"

	"go.mau.fi/whatsmeow"
	"golang.org/x/crypto/bcrypt"
)

type adminService struct {
	adminRepo domain.AdminRepository
	messenger *whatsmeow.Client
}

func NewAdminService(adminRepo domain.AdminRepository, meow *whatsmeow.Client) domain.AdminUseCase {
	return &adminService{
		adminRepo: adminRepo,
		messenger: meow,
	}
}

func (s *adminService) UpdateAdmin(ctx context.Context, payload domain.User) error {
	if err := s.adminRepo.UpdateAdmin(ctx, payload); err != nil {
		return errors.New(utils.TranslateDBError(err))
	}
	return nil
}

func (s *adminService) ClearUserDeletedAt(ctx context.Context, userUUID string) error {
	err := s.adminRepo.ClearUserDeletedAt(ctx, userUUID)
	if err != nil {
		return err
	}

	return nil
}

func (s *adminService) GetAllClassHistories(ctx context.Context) (*[]domain.ClassHistory, error) {
	data, err := s.adminRepo.GetAllClassHistories(ctx)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// Managers =====================================================================================================
// TEACHER MANAGEMENT
func (s *adminService) GetAllManagers(ctx context.Context) ([]domain.User, error) {
	data, err := s.adminRepo.GetAllManagers(ctx)
	if err != nil {
		return nil, err
	}

	return data, nil
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

// ✅ Update Teacher Profile
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
	teachers, err := s.adminRepo.GetAllManagers(ctx)
	if err != nil {
		return nil, errors.New(utils.TranslateDBError(err))
	}
	return teachers, nil
}

func (s *adminService) GetManagerByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	if uuid == "" {
		return nil, errors.New("uuid tidak boleh kosong")
	}

	teacher, err := s.adminRepo.GetManagerByUUID(ctx, uuid)
	if err != nil {
		return nil, errors.New(utils.TranslateDBError(err))
	}
	return teacher, nil
}

func (s *adminService) GetPackagesByID(ctx context.Context, id int) (*domain.Package, error) {
	if id <= 0 {
		return nil, errors.New("ID paket tidak valid")
	}
	return s.adminRepo.GetPackagesByID(ctx, id)
}

// AssignPackageToStudent assigns a package to a student
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

	if s.messenger != nil {
		// Send WhatsApp notification to student
		phoneNormalized := utils.NormalizePhoneNumber(dataStudent.Phone)
		if phoneNormalized != "" && s.messenger != nil {
			msgToStudent := fmt.Sprintf(
				`Halo %s,

Paket les Anda telah aktif!
• Paket: %s (%s)
• Instrument: %s
• Kuota: %d sesi

Silakan login ke aplikasi untuk mulai booking sesi.

Terima kasih,

🌐 Website: %s
🔔 %s Notification System
`,
				dataStudent.Name,
				dataPackage.Name,
				dataPackage.Description,
				dataPackage.Instrument.Name,
				dataPackage.Quota,
				"https://www.madeu.app",
				os.Getenv("APP_NAME"),
			)

			go func() {
				utils.SendWhatsAppMessage(s.messenger, phoneNormalized, msgToStudent)
			}()
		}
	}

	return nil
}

// CreatePackage creates a package
func (s *adminService) CreatePackage(ctx context.Context, pkg *domain.Package) (*domain.Package, error) {
	if pkg == nil {
		return nil, errors.New("paket tidak boleh kosong")
	}
	created, err := s.adminRepo.CreatePackage(ctx, pkg)
	if err != nil {
		return nil, err
	}
	return created, nil
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

// CreateInstrument creates a new instrument (note: accepts *domain.Instrument)
func (s *adminService) CreateInstrument(ctx context.Context, instrument *domain.Instrument) (*domain.Instrument, error) {
	if instrument == nil {
		return nil, errors.New("instrumen tidak boleh kosong")
	}
	if instrument.Name == "" {
		return nil, errors.New("nama instrumen tidak boleh kosong")
	}
	created, err := s.adminRepo.CreateInstrument(ctx, instrument)
	if err != nil {
		return nil, err
	}
	return created, nil
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

// GetAllPackages returns all packages
func (s *adminService) GetAllPackages(ctx context.Context) ([]domain.Package, error) {
	return s.adminRepo.GetAllPackages(ctx)
}

// GetAllInstruments returns all instruments
func (s *adminService) GetAllInstruments(ctx context.Context) ([]domain.Instrument, error) {
	return s.adminRepo.GetAllInstruments(ctx)
}

// GetAllStudents returns all students
func (s *adminService) GetAllStudents(ctx context.Context) ([]domain.User, error) {
	return s.adminRepo.GetAllStudents(ctx)
}

// GetFilteredStudents returns students filtered by activity status
func (s *adminService) GetFilteredStudents(ctx context.Context, filter domain.StudentActivityFilter) ([]domain.User, error) {
	return s.adminRepo.GetFilteredStudents(ctx, filter)
}



// GetAllUsers returns all users
func (s *adminService) GetAllUsers(ctx context.Context) ([]domain.User, error) {
	return s.adminRepo.GetAllUsers(ctx)
}

// GetStudentByUUID fetches a student by UUID
func (s *adminService) GetStudentByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	if uuid == "" {
		return nil, errors.New("UUID wajib diisi")
	}
	return s.adminRepo.GetStudentByUUID(ctx, uuid)
}

// TEACHER MANAGEMENT
func (s *adminService) CreateTeacher(ctx context.Context, user *domain.User, instrumentIDs []int) (*domain.User, error) {
	// Business validation
	if user.Name == "" || user.Email == "" || user.Phone == "" || user.Password == "" {
		return nil, errors.New("semua field wajib diisi")
	}

	user.Role = domain.RoleTeacher // enforce role

	// Hash password sebelum disimpan
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

// ✅ Update Teacher Profile
func (s *adminService) UpdateTeacher(ctx context.Context, user *domain.User, instrumentIDs []int) error {
	if user.UUID == "" {
		return errors.New("uuid teacher tidak boleh kosong")
	}

	if err := s.adminRepo.UpdateTeacher(ctx, user, instrumentIDs); err != nil {
		return errors.New(utils.TranslateDBError(err))
	}
	return nil
}

// ✅ Get All Teachers
func (s *adminService) GetAllTeachers(ctx context.Context) ([]domain.User, error) {
	teachers, err := s.adminRepo.GetAllTeachers(ctx)
	if err != nil {
		return nil, errors.New(utils.TranslateDBError(err))
	}
	return teachers, nil
}

// ✅ Get Teacher by UUID
func (s *adminService) GetTeacherByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	if uuid == "" {
		return nil, errors.New("uuid tidak boleh kosong")
	}

	teacher, err := s.adminRepo.GetTeacherByUUID(ctx, uuid)
	if err != nil {
		return nil, errors.New(utils.TranslateDBError(err))
	}
	return teacher, nil
}

// ✅ Delete Teacher
func (s *adminService) DeleteUser(ctx context.Context, uuid string) error {
	if uuid == "" {
		return errors.New("uuid tidak boleh kosong")
	}

	if err := s.adminRepo.DeleteUser(ctx, uuid); err != nil {
		return errors.New(utils.TranslateDBError(err))
	}
	return nil
}

// Setting
func (s *adminService) GetSetting(ctx context.Context) (*domain.Setting, error) {
	return s.adminRepo.GetSetting(ctx)
}

func (s *adminService) UpdateSetting(ctx context.Context, setting *domain.Setting) error {
	if setting == nil {
		return errors.New("pengaturan tidak valid")
	}
	return s.adminRepo.UpdateSetting(ctx, setting)
}

// WhatsApp Management =========================================================

func (s *adminService) GetWhatsAppStatus(ctx context.Context) (map[string]interface{}, error) {
	if s.messenger == nil {
		return map[string]interface{}{"status": "not_initialized"}, nil
	}
	if s.messenger.IsConnected() {
		return map[string]interface{}{"status": "connected", "jid": s.messenger.Store.ID.String()}, nil
	}
	if s.messenger.Store.ID != nil {
		return map[string]interface{}{"status": "disconnected_but_linked"}, nil
	}
	return map[string]interface{}{"status": "not_linked"}, nil
}

func (s *adminService) ConnectWhatsApp(ctx context.Context) (map[string]interface{}, error) {
	if s.messenger == nil {
		return nil, errors.New("whatsapp client is not initialized")
	}

	if s.messenger.IsConnected() {
		return map[string]interface{}{"status": "already_connected"}, nil
	}

	if s.messenger.Store.ID != nil {
		err := s.messenger.Connect()
		if err != nil {
			return nil, fmt.Errorf("gagal menyambungkan: %v", err)
		}
		return map[string]interface{}{"status": "connected"}, nil
	}

	// Device not linked. We must generate a QR code!
	qrChan, _ := s.messenger.GetQRChannel(context.Background())
	err := s.messenger.Connect()
	if err != nil {
		return nil, fmt.Errorf("gagal menyambungkan device baru: %w", err)
	}

	for evt := range qrChan {
		if evt.Event == "code" {
			return map[string]interface{}{
				"status": "qr_code_generated",
				"qr_code": evt.Code,
			}, nil
		} else if evt.Event == "success" {
			return map[string]interface{}{"status": "connected"}, nil
		} else if evt.Event == "timeout" {
			break
		}
	}
	
	s.messenger.Disconnect()
	return nil, errors.New("gagal mendapatkan qr code")
}

func (s *adminService) DisconnectWhatsApp(ctx context.Context) error {
	if s.messenger == nil {
		return errors.New("whatsapp client not initialized")
	}

	// Try to connect first before logging out if not connected
	if !s.messenger.IsConnected() {
		// Ignore error since we just want to ensure it connects if it can
		_ = s.messenger.Connect()
	}

	var err error
	if s.messenger.IsConnected() {
		err = s.messenger.Logout(ctx)
	} else {
		// If it's still not connected, force delete the device from local store
		// to make sure it's untethered locally.
		if s.messenger.Store != nil {
			err = s.messenger.Store.Delete(ctx)
		}
	}

	s.messenger.Disconnect()
	return err
}

func (s *adminService) PingWhatsApp(ctx context.Context, phone string) error {
	if s.messenger == nil || !s.messenger.IsConnected() {
		return errors.New("whatsapp is not connected")
	}
	phoneNormalized := utils.NormalizePhoneNumber(phone)
	if phoneNormalized == "" {
		return errors.New("nomor telepon tidak valid")
	}
	msg := "Ping dari sistem MadEU. Tes koneksi berhasil."
	return utils.SendWhatsAppMessage(s.messenger, phoneNormalized, msg)
}
