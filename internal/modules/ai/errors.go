package ai

import "errors"

var (
	ErrOperationNotFound    = errors.New("AI operation not found")
	ErrAISessionExpired     = errors.New("AI session expired")
	ErrIdempotencyConflict  = errors.New("idempotency conflict")
	ErrInvalidAIReference   = errors.New("invalid AI reference")
	ErrObjectRequiresManual = errors.New("object requires manual entry")
)
