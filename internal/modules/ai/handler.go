package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"strconv"

	"github.com/gofiber/fiber/v3"
	coreai "github.com/waste-not-app/waste-not-backend/internal/ai"
	"github.com/waste-not-app/waste-not-backend/internal/auth"
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

	group := app.Group("/v1/ai", authMw)
	group.Get("/objects", handler.GetObjects)
	group.Post("/detections", handler.CreateDetection)
	group.Post("/recommendations", handler.CreateRecommendation)
	group.Delete("/detections/:id", handler.DeleteDetection)
}

func writeAIFailure(c fiber.Ctx, err error) error {
	var a *coreai.Error
	if !errors.As(err, &a) {
		a = coreai.ErrInvalid
	}
	if a.RetryAfter > 0 {
		c.Set("Retry-After", strconv.Itoa(a.RetryAfter))
	}
	return response.Fail(c, a.Status, a.Code, nil)
}

func (h *Handler) mapOperationError(c fiber.Ctx, err error) error {
	var a *coreai.Error
	switch {
	case errors.Is(err, ErrOperationNotFound):
		return response.Fail(c, fiber.StatusNotFound, "NOT_FOUND", nil)
	case errors.Is(err, ErrAISessionExpired):
		return response.Fail(c, fiber.StatusGone, "AI_SESSION_EXPIRED", nil)
	case errors.Is(err, ErrIdempotencyConflict):
		return response.Fail(c, fiber.StatusConflict, "IDEMPOTENCY_CONFLICT", nil)
	case errors.Is(err, ErrInvalidAIReference):
		return response.Fail(c, fiber.StatusUnprocessableEntity, "INVALID_AI_REFERENCE", nil)
	case errors.Is(err, ErrObjectRequiresManual):
		return response.Fail(c, fiber.StatusUnprocessableEntity, "OBJECT_REQUIRES_MANUAL", nil)
	case errors.As(err, &a):
		return writeAIFailure(c, a)
	default:
		return response.InternalError(c, h.log, err)
	}
}

func (h *Handler) handleReplay(c fiber.Ctx, userID string, o Operation) error {
	o, err := h.service.WaitReplay(c.Context(), userID, o)
	if err != nil {
		return h.mapOperationError(c, err)
	}
	if o.Code != "" {
		return writeAIFailure(c, &coreai.Error{Status: o.Status, Code: o.Code, RetryAfter: o.RetryAfter})
	}
	if !json.Valid(o.Response) {
		return writeAIFailure(c, coreai.ErrInvalid)
	}
	return response.OK(c, o.Response)
}

// @Summary Get AI Object Catalog
// @Description Retrieve the catalog of waste objects detectable by the AI vision service
// @Tags AI Scan
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.SuccessDataRes[AICatalogRes]
// @Failure 401,502,503 {object} response.ErrorResponse
// @Router /v1/ai/objects [get]
func (h *Handler) GetObjects(c fiber.Ctx) error {
	if !h.service.IsAIEnabled() {
		return response.Fail(c, fiber.StatusServiceUnavailable, "AI_DISABLED", nil)
	}
	v, err := h.service.GetCatalog(c.Context(), response.RequestID(c))
	if err != nil {
		return writeAIFailure(c, err)
	}
	return response.OK(c, v)
}

// @Summary Detect Waste Object from Image
// @Description Upload a JPEG or PNG image (max 5MB) to detect waste object candidates
// @Tags AI Scan
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Param client_request_id formData string true "Idempotency UUID"
// @Param image formData file true "Waste image file (image/jpeg or image/png)"
// @Success 201 {object} response.SuccessDataRes[AIDetectionRes]
// @Success 200 {object} response.SuccessDataRes[AIDetectionRes]
// @Failure 400,401,409,413,415,422,429,502,503 {object} response.ErrorResponse
// @Router /v1/ai/detections [post]
func (h *Handler) CreateDetection(c fiber.Ctx) error {
	if !h.limiter.AcquireImageSlot(c) {
		return nil
	}
	defer h.limiter.ReleaseImageSlot()

	if !h.service.IsAIEnabled() {
		return response.Fail(c, fiber.StatusServiceUnavailable, "AI_DISABLED", nil)
	}

	mediaType, params, err := mime.ParseMediaType(c.Get(fiber.HeaderContentType))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return response.Fail(c, fiber.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", nil)
	}
	rawBody := c.Body()
	if len(rawBody) > MaxImageBytes+65536 {
		return response.Fail(c, fiber.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", nil)
	}

	reader := multipart.NewReader(bytes.NewReader(rawBody), params["boundary"])
	var imageBytes []byte
	declaredMIME := ""
	clientID := ""
	seen := map[string]bool{}

	for {
		part, e := reader.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil {
			return response.Fail(c, fiber.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", nil)
		}
		name := part.FormName()
		if seen[name] || !domain.Contains([]string{"image", "client_request_id"}, name) {
			part.Close()
			return response.Fail(c, fiber.StatusBadRequest, "INVALID_REQUEST", nil)
		}
		seen[name] = true
		if name == "image" {
			contentType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if err != nil || (contentType != "image/jpeg" && contentType != "image/png") {
				part.Close()
				return response.Fail(c, fiber.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", nil)
			}
			declaredMIME = contentType
			imageBytes, e = io.ReadAll(io.LimitReader(part, MaxImageBytes+1))
		} else {
			raw, err := io.ReadAll(io.LimitReader(part, 100))
			e = err
			clientID = string(raw)
		}
		part.Close()
		if e != nil || len(imageBytes) > MaxImageBytes {
			return response.Fail(c, fiber.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", nil)
		}
	}

	if !auth.UUID(clientID) || len(imageBytes) == 0 {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", nil)
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(imageBytes))
	if e != nil || (format != "jpeg" && format != "png") || declaredMIME != "image/"+format {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "INVALID_IMAGE", nil)
	}
	if cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 12000000 {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "IMAGE_DIMENSIONS_EXCEEDED", nil)
	}

	ident := middleware.Who(c)
	o, isOwner, err := h.service.ClaimOrReplay(c.Context(), ident.User.ID, "DETECTION", clientID, auth.Digest(imageBytes), nil)
	if err != nil {
		return h.mapOperationError(c, err)
	}
	if !isOwner {
		return h.handleReplay(c, ident.User.ID, o)
	}

	result, snapshot, execErr := h.service.ExecuteDetection(c.Context(), response.RequestID(c), o, imageBytes)
	status, compErr := h.service.CompleteOperation(c.Context(), ident.User.ID, o, result, snapshot, execErr)
	if compErr != nil {
		return h.mapOperationError(c, compErr)
	}
	return response.Data(c, status, result)
}

// @Summary Generate AI Waste Disposal Recommendation
// @Description Request an AI disposal recommendation from a detection candidate or manual object selection
// @Tags AI Scan
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param req body RecommendationReq true "Recommendation Input"
// @Success 201 {object} response.SuccessDataRes[AIRecommendationRes]
// @Success 200 {object} response.SuccessDataRes[AIRecommendationRes]
// @Failure 400,401,404,409,410,422,429,502,503 {object} response.ErrorResponse
// @Router /v1/ai/recommendations [post]
func (h *Handler) CreateRecommendation(c fiber.Ctx) error {
	if !h.service.IsRecommendationEnabled() || !h.service.IsAIEnabled() {
		return response.Fail(c, fiber.StatusServiceUnavailable, "AI_DISABLED", nil)
	}

	var in map[string]any
	if !response.DecodeJSON(c, &in) {
		return nil
	}
	if h.service.NormalizeRecommendationInput(in) != nil {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", nil)
	}
	clientID, _ := in["client_request_id"].(string)
	if !auth.UUID(clientID) {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", nil)
	}

	ident := middleware.Who(c)
	var parent *string
	var selection any
	if in["input_source"] == "DETECTION" {
		id, _ := in["detection_id"].(string)
		if !auth.UUID(id) {
			return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", nil)
		}
		var prepErr error
		parent, selection, prepErr = h.service.PrepareDetectionSelection(c.Context(), ident.User.ID, in)
		if prepErr != nil {
			return h.mapOperationError(c, prepErr)
		}
	}

	raw, _ := json.Marshal(in)
	o, isOwner, err := h.service.ClaimOrReplay(c.Context(), ident.User.ID, "RECOMMENDATION", clientID, auth.Digest(raw), parent)
	if err != nil {
		return h.mapOperationError(c, err)
	}
	if !isOwner {
		return h.handleReplay(c, ident.User.ID, o)
	}

	result, snapshot, execErr := h.service.ExecuteRecommendation(c.Context(), response.RequestID(c), o, in, selection)
	status, compErr := h.service.CompleteOperation(c.Context(), ident.User.ID, o, result, snapshot, execErr)
	if compErr != nil {
		return h.mapOperationError(c, compErr)
	}
	return response.Data(c, status, result)
}

// @Summary Delete AI Detection Session
// @Description Soft-delete an AI detection operation and its snapshot
// @Tags AI Scan
// @Security BearerAuth
// @Param id path string true "Detection UUID"
// @Success 204 "No Content"
// @Failure 401,404,503 {object} response.ErrorResponse
// @Router /v1/ai/detections/{id} [delete]
func (h *Handler) DeleteDetection(c fiber.Ctx) error {
	ident := middleware.Who(c)
	err := h.service.DeleteDetection(c.Context(), ident.User.ID, c.Params("id"))
	if err != nil {
		return h.mapOperationError(c, err)
	}
	return response.NoContent(c)
}
