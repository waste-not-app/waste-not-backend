package auth

import "errors"

var (
	ErrEmailExists       = errors.New("email already exists")
	ErrUnauthenticated   = errors.New("unauthenticated")
	ErrSessionExpired    = errors.New("session expired")
	ErrInvalidResetToken = errors.New("invalid or expired password reset token")
	ErrWrongPassword     = errors.New("wrong current password")
	ErrMailUnavailable   = errors.New("password reset mail unavailable")
	ErrDeletionPending   = errors.New("account deleted; private photo cleanup pending")
)
