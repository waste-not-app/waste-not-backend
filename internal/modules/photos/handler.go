package photos

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"

	"github.com/gofiber/fiber/v3"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/middleware"
	"github.com/waste-not-app/waste-not-backend/internal/shared/ratelimit"
	"github.com/waste-not-app/waste-not-backend/internal/shared/response"
)

type Handler struct {
	service Service
	limiter *ratelimit.Limiter
	log     *slog.Logger
}

func RegisterHTTPRoutes(app *fiber.App, service Service, limiter *ratelimit.Limiter, log *slog.Logger, authMw fiber.Handler) {
	handler := &Handler{
		service: service,
		limiter: limiter,
		log:     log,
	}

	group := app.Group("/v1/photos", authMw)
	group.Post("", handler.UploadPhoto)
	group.Get("/:id", handler.GetPhoto)
	group.Delete("/:id", handler.DeletePhoto)
}

// @Summary Upload Private Photo Attachment
// @Description Upload a validated JPEG, PNG, or WebP image (max 5 MiB) for attaching to a waste log
// @Tags Photos
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Param image formData file true "Image file (image/jpeg, image/png, image/webp)"
// @Success 201 {object} response.SuccessDataRes[PhotoRes]
// @Failure 400,401,413,415,422,503 {object} response.ErrorResponse
// @Router /v1/photos [post]
func (h *Handler) UploadPhoto(c fiber.Ctx) error {
	if !h.limiter.AcquireImageSlot(c) {
		return nil
	}
	defer h.limiter.ReleaseImageSlot()

	if !h.service.Enabled() {
		return response.Fail(c, fiber.StatusServiceUnavailable, "UNAVAILABLE", nil)
	}

	mediaType, params, err := mime.ParseMediaType(c.Get(fiber.HeaderContentType))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return response.Fail(c, fiber.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", nil)
	}
	rawBody := c.Body()
	if len(rawBody) > MaxPhotoBytes+65536 {
		return response.Fail(c, fiber.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", nil)
	}

	reader := multipart.NewReader(bytes.NewReader(rawBody), params["boundary"])
	var imageBytes []byte
	declaredMIME := ""
	seenImage := false

	for {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return response.Fail(c, fiber.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", nil)
		}
		name := part.FormName()
		if name != "image" || seenImage {
			part.Close()
			return response.Fail(c, fiber.StatusBadRequest, "INVALID_REQUEST", nil)
		}
		seenImage = true
		contentType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil || !domain.Contains([]string{"image/jpeg", "image/png", "image/webp"}, contentType) {
			part.Close()
			return response.Fail(c, fiber.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", nil)
		}
		declaredMIME = contentType
		imageBytes, e = io.ReadAll(io.LimitReader(part, MaxPhotoBytes+1))
		part.Close()
		if e != nil || len(imageBytes) > MaxPhotoBytes {
			return response.Fail(c, fiber.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", nil)
		}
	}

	if !seenImage || len(imageBytes) == 0 {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", map[string]string{
			"image": "Pilih gambar maksimal 5 MB.",
		})
	}

	ident := middleware.Who(c)
	res, err := h.service.Upload(c.Context(), ident.User.ID, declaredMIME, imageBytes)
	if err != nil {
		switch {
		case errors.Is(err, ErrPhotosDisabled):
			return response.Fail(c, fiber.StatusServiceUnavailable, "UNAVAILABLE", nil)
		case errors.Is(err, ErrFileTooLarge):
			return response.Fail(c, fiber.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", nil)
		case errors.Is(err, ErrUnsupportedMediaType):
			return response.Fail(c, fiber.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", nil)
		case errors.Is(err, ErrInvalidImage):
			return response.Fail(c, fiber.StatusUnprocessableEntity, "INVALID_IMAGE", nil)
		case errors.Is(err, ErrImageDimensionsExceeded):
			return response.Fail(c, fiber.StatusUnprocessableEntity, "IMAGE_DIMENSIONS_EXCEEDED", nil)
		default:
			return response.InternalError(c, h.log, err)
		}
	}
	return response.Created(c, res)
}

// @Summary Download Private Photo Attachment
// @Description Download an authenticated user's private photo attachment
// @Tags Photos
// @Produce octet-stream
// @Security BearerAuth
// @Param id path string true "Photo UUID"
// @Success 200 {file} binary
// @Failure 401,404,503 {object} response.ErrorResponse
// @Router /v1/photos/{id} [get]
func (h *Handler) GetPhoto(c fiber.Ctx) error {
	if !h.service.Enabled() {
		return response.Fail(c, fiber.StatusServiceUnavailable, "UNAVAILABLE", nil)
	}
	ident := middleware.Who(c)
	data, mimeType, err := h.service.GetContent(c.Context(), ident.User.ID, c.Params("id"))
	if err != nil {
		switch {
		case errors.Is(err, ErrPhotosDisabled):
			return response.Fail(c, fiber.StatusServiceUnavailable, "UNAVAILABLE", nil)
		case errors.Is(err, ErrPhotoNotFound):
			return response.Fail(c, fiber.StatusNotFound, "NOT_FOUND", nil)
		default:
			return response.InternalError(c, h.log, err)
		}
	}
	c.Set("Content-Type", mimeType)
	c.Set("Cache-Control", "private, no-store")
	c.Set("X-Content-Type-Options", "nosniff")
	return c.Status(fiber.StatusOK).Send(data)
}

// @Summary Delete Unused Photo Attachment
// @Description Delete a staged photo attachment owned by the authenticated user
// @Tags Photos
// @Security BearerAuth
// @Param id path string true "Photo UUID"
// @Success 204 "No Content"
// @Failure 401,404,409,503 {object} response.ErrorResponse
// @Router /v1/photos/{id} [delete]
func (h *Handler) DeletePhoto(c fiber.Ctx) error {
	if !h.service.Enabled() {
		return response.Fail(c, fiber.StatusServiceUnavailable, "UNAVAILABLE", nil)
	}
	ident := middleware.Who(c)
	err := h.service.Delete(c.Context(), ident.User.ID, c.Params("id"))
	if err != nil {
		switch {
		case errors.Is(err, ErrPhotosDisabled):
			return response.Fail(c, fiber.StatusServiceUnavailable, "UNAVAILABLE", nil)
		case errors.Is(err, ErrPhotoNotFound):
			return response.Fail(c, fiber.StatusNotFound, "NOT_FOUND", nil)
		case errors.Is(err, ErrPhotoInUse):
			return response.Fail(c, fiber.StatusConflict, "PHOTO_IN_USE", nil)
		default:
			return response.InternalError(c, h.log, err)
		}
	}
	return response.NoContent(c)
}
