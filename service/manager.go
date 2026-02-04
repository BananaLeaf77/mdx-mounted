package service

import (
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"fmt"
	"os"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

func NewManagerService(managerRepo domain.ManagerRepository, meow *whatsmeow.Client) domain.ManagerUseCase {
	return &managerService{
		managerRepo: managerRepo,
		messenger:   meow,
	}
}

type managerService struct {
	managerRepo domain.ManagerRepository
	messenger   *whatsmeow.Client
}

// Students =====================================================================================================
// ✅ Get All Students
func (s *managerService) GetAllStudents(ctx context.Context) ([]domain.User, error) {
	data, err := s.managerRepo.GetAllStudents(ctx)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (s *managerService) UpdateManager(ctx context.Context, manager *domain.User) error {
	return s.managerRepo.UpdateManager(ctx, manager)
}

// ✅ Get Student by UUID
func (s *managerService) GetStudentByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	data, err := s.managerRepo.GetStudentByUUID(ctx, uuid)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// ✅ Modify Student Package Quota
func (s *managerService) ModifyStudentPackageQuota(ctx context.Context, studentUUID string, packageID int, incomingQuota int) error {
	data, err := s.managerRepo.ModifyStudentPackageQuota(ctx, studentUUID, packageID, incomingQuota)
	if err != nil {
		return err
	}

	// Send notification to student
	phoneNormalized := utils.NormalizePhoneNumber(data.Phone)
	if phoneNormalized != "" && s.messenger != nil {
		msgToStudent := fmt.Sprintf(
			`*NOTIFIKASI PENYESUAIAN KUOTA*

Halo %s,

Telah dilakukan penyesuaian kuota paket les Anda:
📊 Kuota saat ini: %d sesi
Kuota yang telah dikembalikan dapat segera digunakan untuk penjadwalan sesi berikutnya.

Terima kasih atas pengertiannya.

🌐 Website: %s
🔔 %s Notification System
`,
			data.Name,
			incomingQuota,
			os.Getenv("TARGETED_DOMAIN"),
			os.Getenv("APP_NAME"),
		)

		jid := types.NewJID(phoneNormalized, types.DefaultUserServer)
		waMessage := &waE2E.Message{
			Conversation: &msgToStudent,
		}

		go s.messenger.SendMessage(context.Background(), jid, waMessage)
	}

	return nil
}
