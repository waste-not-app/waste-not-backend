package photos

import "errors"

var (
	ErrPhotosDisabled          = errors.New("photo attachments disabled")
	ErrPhotoNotFound           = errors.New("photo not found")
	ErrPhotoInUse              = errors.New("photo is attached to an active log")
	ErrFileTooLarge            = errors.New("photo exceeds 5 MiB limit")
	ErrUnsupportedMediaType    = errors.New("unsupported media type")
	ErrInvalidImage            = errors.New("invalid image content")
	ErrImageDimensionsExceeded = errors.New("image dimensions exceed limits")
)
