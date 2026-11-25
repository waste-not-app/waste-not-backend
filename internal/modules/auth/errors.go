package auth

import "errors"

var (
	ErrEmailExists            = errors.New("email already exists")
	ErrUnauthenticated        = errors.New("unauthenticated")
	ErrSessionExpired         = errors.New("session expired")
	ErrInvalidResetToken      = errors.New("invalid or expired password reset token")
	ErrWrongPassword          = errors.New("wrong current password")
	ErrMailUnavailable        = errors.New("password reset mail unavailable")
	ErrDeletionPending        = errors.New("account deleted; private photo cleanup pending")
	ErrGoogleAuthUnavailable  = errors.New("authentication with Google unavailable")
	ErrGoogleLinkRequired     = errors.New("account requires proof of its current password")
	ErrGoogleIdentityConflict = errors.New("account already linked to a different Google identity")
	ErrGoogleReauthFailed     = errors.New("reauthentication with Google failed")
	ErrGoogleRateLimited      = errors.New("authentication with Google rate limited")
	ErrGoogleHashBusy         = errors.New("password validation busy")
)
