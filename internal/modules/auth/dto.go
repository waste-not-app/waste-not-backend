package auth

import (
	"strings"
	"unicode/utf8"

	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

type RegisterReq struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Timezone string `json:"timezone"`
}

func (r *RegisterReq) Validate(emailValid bool) map[string]string {
	r.Name = strings.TrimSpace(r.Name)
	f := map[string]string{}
	if !emailValid {
		f["email"] = "Check the email format."
	}
	if n := utf8.RuneCountInString(r.Name); n < domain.MinNameLength || n > domain.MaxNameLength {
		f["name"] = "Enter a name of 2 to 60 characters."
	}
	if n := utf8.RuneCountInString(r.Password); n < domain.MinPasswordLength || n > domain.MaxPasswordLength {
		f["password"] = "Use a password of 12 to 128 characters."
	}
	if !domain.ValidTimezone(r.Timezone) {
		f["timezone"] = "Choose an IANA time zone."
	}
	return f
}

type LoginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RefreshTokenReq struct {
	RefreshToken string `json:"refresh_token"`
}

type PasswordResetReq struct {
	Email string `json:"email"`
}

type PasswordResetConfirmReq struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type DeleteMeReq struct {
	Password      *string `json:"password,omitempty"`
	GoogleIDToken *string `json:"google_id_token,omitempty"`
}

type GoogleLoginReq struct {
	IDToken         string  `json:"id_token"`
	Timezone        string  `json:"timezone"`
	CurrentPassword *string `json:"current_password,omitempty"`
}

type GoogleChallengeRes struct {
	Nonce string `json:"nonce"`
}

type PasswordResetRes struct {
	Message string `json:"message"`
}

type AuthTokensRes struct {
	AccessToken      string   `json:"access_token"`
	AccessExpiresAt  string   `json:"access_expires_at"`
	RefreshToken     string   `json:"refresh_token"`
	RefreshExpiresAt string   `json:"refresh_expires_at"`
	SessionID        string   `json:"session_id"`
	User             *UserRes `json:"user,omitempty"`
}

type UserRes struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Email       string           `json:"email"`
	Timezone    string           `json:"timezone"`
	HasPassword bool             `json:"has_password"`
	Progress    *domain.Progress `json:"progress,omitempty"`
}
