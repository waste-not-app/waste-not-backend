package auth

import "errors"

var (
	ErrEmailExists     = errors.New("email already exists")
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrSessionExpired  = errors.New("session expired")
)
