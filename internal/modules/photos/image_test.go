package photos

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func makeJPEGBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 25, G: 91, B: 61, A: 255})
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	return buf.Bytes()
}

func injectJPEGExif(raw []byte, markerPayload string) []byte {
	// Insert an APP1 (0xFFE1) segment right after SOI (0xFFD8).
	payload := append([]byte("Exif\x00\x00"), []byte(markerPayload)...)
	segLen := len(payload) + 2
	var out bytes.Buffer
	out.Write(raw[:2])
	out.Write([]byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen & 0xFF)})
	out.Write(payload)
	out.Write(raw[2:])
	return out.Bytes()
}

func makePNGBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestNormalizeAndStripImage(t *testing.T) {
	t.Run("strips EXIF from JPEG", func(t *testing.T) {
		raw := injectJPEGExif(makeJPEGBytes(16, 16), "SECRET_GPS_TAG")
		if !bytes.Contains(raw, []byte("SECRET_GPS_TAG")) {
			t.Fatal("fixture did not contain EXIF marker")
		}
		clean, mimeType, ext, err := NormalizeAndStripImage("image/jpeg", raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mimeType != "image/jpeg" || ext != "jpg" {
			t.Fatalf("got mime=%s ext=%s", mimeType, ext)
		}
		if bytes.Contains(clean, []byte("SECRET_GPS_TAG")) || bytes.Contains(clean, []byte("Exif")) {
			t.Fatal("EXIF metadata was not stripped")
		}
	})

	t.Run("validates PNG", func(t *testing.T) {
		raw := makePNGBytes(12, 12)
		clean, mimeType, ext, err := NormalizeAndStripImage("image/png", raw)
		if err != nil || mimeType != "image/png" || ext != "png" || len(clean) == 0 {
			t.Fatalf("png failed: err=%v mime=%s ext=%s", err, mimeType, ext)
		}
	})

	t.Run("rejects spoofed MIME", func(t *testing.T) {
		raw := makePNGBytes(8, 8)
		_, _, _, err := NormalizeAndStripImage("image/jpeg", raw)
		if !errors.Is(err, ErrInvalidImage) {
			t.Fatalf("expected ErrInvalidImage, got %v", err)
		}
	})

	t.Run("rejects oversized file", func(t *testing.T) {
		raw := make([]byte, MaxPhotoBytes+1)
		_, _, _, err := NormalizeAndStripImage("image/jpeg", raw)
		if !errors.Is(err, ErrFileTooLarge) {
			t.Fatalf("expected ErrFileTooLarge, got %v", err)
		}
	})

	t.Run("rejects oversized dimensions", func(t *testing.T) {
		raw := makePNGBytes(MaxPhotoDim+1, 8)
		_, _, _, err := NormalizeAndStripImage("image/png", raw)
		if !errors.Is(err, ErrImageDimensionsExceeded) {
			t.Fatalf("expected ErrImageDimensionsExceeded, got %v", err)
		}
	})
}
