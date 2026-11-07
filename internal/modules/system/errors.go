package system

import "errors"

var (
	ErrDatabaseNotReady = errors.New("database schema migration not ready")
)
