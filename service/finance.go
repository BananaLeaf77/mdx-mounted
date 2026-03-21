package service

import (
	"chronosphere/domain"
	"chronosphere/utils"
	"context"
	"errors"

	"golang.org/x/crypto/bcrypt"
)

type financeService struct {
	repo domain.FinanceRepository
}

func NewFinanceService(repo domain.FinanceRepository) domain.FinanceUseCase {
	return &financeService{repo: repo}
}

func (s *financeService) CreateFinance(ctx context.Context, user *domain.User) (*domain.User, error) {
	if user.Name == "" || user.Email == "" || user.Phone == "" || user.Password == "" {
		return nil, errors.New("nama, email, telepon, dan password wajib diisi")
	}

	user.Role = domain.RoleFinance

	hashed, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.New("gagal mengenkripsi password")
	}
	user.Password = string(hashed)

	created, err := s.repo.CreateFinance(ctx, user)
	if err != nil {
		return nil, errors.New(utils.TranslateDBError(err))
	}
	return created, nil
}

func (s *financeService) GetAllFinance(ctx context.Context) ([]domain.User, error) {
	return s.repo.GetAllFinance(ctx)
}

func (s *financeService) GetFinanceByUUID(ctx context.Context, uuid string) (*domain.User, error) {
	if uuid == "" {
		return nil, errors.New("UUID tidak boleh kosong")
	}
	return s.repo.GetFinanceByUUID(ctx, uuid)
}

func (s *financeService) UpdateFinance(ctx context.Context, user *domain.User) error {
	if user.UUID == "" {
		return errors.New("UUID tidak boleh kosong")
	}
	return s.repo.UpdateFinance(ctx, user)
}

func (s *financeService) DeleteFinance(ctx context.Context, uuid string) error {
	if uuid == "" {
		return errors.New("UUID tidak boleh kosong")
	}
	return s.repo.DeleteFinance(ctx, uuid)
}
