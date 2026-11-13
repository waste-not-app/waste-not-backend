package photos

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"

	"github.com/disintegration/imaging"
	"golang.org/x/image/webp"
)

const (
	MaxPhotoBytes  = 5 * 1024 * 1024
	MaxPhotoDim    = 4096
	MaxPhotoPixels = 12000000
)

func NormalizeAndStripImage(declaredMIME string, raw []byte) ([]byte, string, string, error) {
	if len(raw) == 0 {
		return nil, "", "", ErrInvalidImage
	}
	if len(raw) > MaxPhotoBytes {
		return nil, "", "", ErrFileTooLarge
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", "", ErrInvalidImage
	}
	if !matchesDeclaredMIME(declaredMIME, format) {
		return nil, "", "", ErrInvalidImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxPhotoDim || cfg.Height > MaxPhotoDim || int64(cfg.Width)*int64(cfg.Height) > MaxPhotoPixels {
		return nil, "", "", ErrImageDimensionsExceeded
	}

	var out bytes.Buffer
	switch format {
	case "jpeg":
		img, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
		if err != nil {
			return nil, "", "", ErrInvalidImage
		}
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 90}); err != nil {
			return nil, "", "", ErrInvalidImage
		}
		if out.Len() > MaxPhotoBytes {
			return nil, "", "", ErrFileTooLarge
		}
		return out.Bytes(), "image/jpeg", "jpg", nil
	case "png":
		img, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			return nil, "", "", ErrInvalidImage
		}
		if err := png.Encode(&out, img); err != nil {
			return nil, "", "", ErrInvalidImage
		}
		if out.Len() > MaxPhotoBytes {
			return nil, "", "", ErrFileTooLarge
		}
		return out.Bytes(), "image/png", "png", nil
	case "webp":
		img, err := webp.Decode(bytes.NewReader(raw))
		if err != nil {
			return nil, "", "", ErrInvalidImage
		}
		if err := png.Encode(&out, img); err != nil {
			return nil, "", "", ErrInvalidImage
		}
		if out.Len() <= MaxPhotoBytes {
			return out.Bytes(), "image/png", "png", nil
		}
		out.Reset()
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 90}); err != nil {
			return nil, "", "", ErrInvalidImage
		}
		if out.Len() > MaxPhotoBytes {
			return nil, "", "", ErrFileTooLarge
		}
		return out.Bytes(), "image/jpeg", "jpg", nil
	default:
		return nil, "", "", ErrUnsupportedMediaType
	}
}

func matchesDeclaredMIME(declared, format string) bool {
	switch declared {
	case "image/jpeg":
		return format == "jpeg"
	case "image/png":
		return format == "png"
	case "image/webp":
		return format == "webp"
	default:
		return false
	}
}
