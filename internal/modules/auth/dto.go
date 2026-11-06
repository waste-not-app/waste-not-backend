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
		f["email"] = "Periksa format email."
	}
	if n := utf8.RuneCountInString(r.Name); n < 2 || n > 60 {
		f["name"] = "Isi nama sepanjang 2–60 karakter."
	}
	if n := utf8.RuneCountInString(r.Password); n < 12 || n > 128 {
		f["password"] = "Gunakan kata sandi 12–128 karakter."
	}
	if !domain.ValidTimezone(r.Timezone) {
		f["timezone"] = "Pilih zona waktu IANA."
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

type AuthTokensRes struct {
	AccessToken      string   `json:"access_token"`
	AccessExpiresAt  string   `json:"access_expires_at"`
	RefreshToken     string   `json:"refresh_token"`
	RefreshExpiresAt string   `json:"refresh_expires_at"`
	SessionID        string   `json:"session_id"`
	User             *UserRes `json:"user,omitempty"`
}

type UserRes struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Email    string           `json:"email"`
	Timezone string           `json:"timezone"`
	Progress *domain.Progress `json:"progress,omitempty"`
}

type UserRecord struct {
	UserRes
	PasswordHash string
}

type SessionState struct {
	SessionID  string
	UserID     string
	Generation int
	ExpiresAt  any
	Used       bool
	Revoked    bool
}
