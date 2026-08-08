// domain/auth.go
package domain

import (
	"chronosphere/utils"
	"context"
)

type AuthUseCase interface {
	ChangeEmail(ctx context.Context, userUUID, newEmail, password string) error
	Me(ctx context.Context, userUUID string) (*User, error)
	GetAccessTokenManager() *utils.JWTManager
	GetRefreshTokenManager() *utils.JWTManager // 🔥 add this line
	Register(ctx context.Context, email string, name string, telephone string, countryCode string, password string, gender string) error
	VerifyOTP(ctx context.Context, email, otp string) error
	Login(ctx context.Context, email, password string) (*AuthTokens, error)
	ForgotPassword(ctx context.Context, email string) error
	ResetPassword(ctx context.Context, email, otp, newPassword string) error
	ChangePassword(ctx context.Context, userUUID, oldPassword, newPassword string) error
	ResendOTP(ctx context.Context, email string) error
}

type AuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}
