package guides

import "errors"

var (
	ErrGuideNotFound = errors.New("waste guide not found")
	ErrInvalidCursor = errors.New("invalid cursor")
)
