package logs

import "errors"

var (
	ErrLogNotFound         = errors.New("waste log not found")
	ErrLogDeleted          = errors.New("waste log has been deleted")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	ErrVersionConflict     = errors.New("version conflict")
	ErrAIReferenceInvalid  = errors.New("invalid AI provenance reference")
	ErrAIAlreadyLinked     = errors.New("AI recommendation already linked")
	ErrAISessionExpired    = errors.New("AI session expired")
	ErrPhotoAlreadyLinked  = errors.New("photo already linked to another log")
	ErrInvalidPatchRequest = errors.New("invalid patch request")
	ErrInvalidCursor       = errors.New("invalid cursor")
)

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return "validation error"
}
