package logs

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/middleware"
	"github.com/waste-not-app/waste-not-backend/internal/shared/cursor"
	"github.com/waste-not-app/waste-not-backend/internal/shared/response"
)

type Handler struct {
	service Service
	log     *slog.Logger
}

func RegisterHTTPRoutes(app *fiber.App, service Service, log *slog.Logger, authMw fiber.Handler) {
	handler := &Handler{
		service: service,
		log:     log,
	}

	group := app.Group("/v1/waste-logs", authMw)
	group.Get("", handler.ListLogs)
	group.Post("", handler.CreateLog)
	group.Get("/:id", handler.GetLog)
	group.Patch("/:id", handler.UpdateLog)
	group.Delete("/:id", handler.DeleteLog)
}

// @Summary Create Waste Log
// @Description Record a new waste management activity with idempotency support via client_request_id
// @Tags Waste Logs
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param req body domain.LogInput true "Waste Log Input"
// @Success 201 {object} response.SuccessDataRes[LogWithProgressRes]
// @Success 200 {object} response.SuccessDataRes[LogWithProgressRes]
// @Failure 400,401,409,410,422,503 {object} response.ErrorResponse
// @Router /v1/waste-logs [post]
func (h *Handler) CreateLog(c fiber.Ctx) error {
	var in domain.LogInput
	if !response.DecodeJSON(c, &in) {
		return nil
	}

	ident := middleware.Who(c)
	res, created, err := h.service.CreateLog(c.Context(), ident.User, in)
	if err != nil {
		var valErr *ValidationError
		switch {
		case errors.As(err, &valErr):
			return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", valErr.Fields)
		case errors.Is(err, ErrIdempotencyConflict):
			return response.Fail(c, fiber.StatusConflict, "IDEMPOTENCY_CONFLICT", nil)
		case errors.Is(err, ErrLogDeleted):
			return response.Fail(c, fiber.StatusGone, "LOG_DELETED", nil)
		case errors.Is(err, ErrAIReferenceInvalid):
			return response.Fail(c, fiber.StatusUnprocessableEntity, "AI_REFERENCE_INVALID", nil)
		case errors.Is(err, ErrAISessionExpired):
			return response.Fail(c, fiber.StatusGone, "AI_SESSION_EXPIRED", nil)
		case errors.Is(err, ErrAIAlreadyLinked):
			return response.Fail(c, fiber.StatusConflict, "AI_ALREADY_LINKED", nil)
		case errors.Is(err, ErrPhotoAlreadyLinked):
			return response.Fail(c, fiber.StatusConflict, "PHOTO_ALREADY_LINKED", nil)
		default:
			return response.InternalError(c, h.log, err)
		}
	}

	if created {
		return response.Created(c, res)
	}
	return response.OK(c, res)
}

// @Summary Get Waste Log by ID
// @Description Retrieve a specific waste log entry belonging to the authenticated user
// @Tags Waste Logs
// @Produce json
// @Security BearerAuth
// @Param id path string true "Waste Log UUID"
// @Success 200 {object} response.SuccessDataRes[domain.Log]
// @Failure 401,404,503 {object} response.ErrorResponse
// @Router /v1/waste-logs/{id} [get]
func (h *Handler) GetLog(c fiber.Ctx) error {
	ident := middleware.Who(c)
	l, err := h.service.GetLog(c.Context(), ident.User.ID, c.Params("id"))
	if errors.Is(err, ErrLogNotFound) {
		return response.Fail(c, fiber.StatusNotFound, "NOT_FOUND", nil)
	}
	if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.OK(c, l)
}

// @Summary List Waste Logs
// @Description Retrieve cursor-paginated waste logs filtered by category, method, and date range
// @Tags Waste Logs
// @Produce json
// @Security BearerAuth
// @Param category query string false "Waste Category (ORGANIC, PLASTIC, PAPER, GLASS, METAL, ELECTRONIC, OTHER)"
// @Param method query string false "Handling Method (REDUCE, REUSE, RECYCLE, TRASH)"
// @Param from query string false "Start Date (YYYY-MM-DD)"
// @Param to query string false "End Date (YYYY-MM-DD)"
// @Param limit query int false "Page size (1-100, default 20)"
// @Param cursor query string false "Opaque pagination cursor"
// @Success 200 {object} response.PaginatedDataRes[domain.Log]
// @Failure 400,401,422,503 {object} response.ErrorResponse
// @Router /v1/waste-logs [get]
func (h *Handler) ListLogs(c fiber.Ctx) error {
	ident := middleware.Who(c)
	category := c.Query("category")
	method := c.Query("method")
	from := c.Query("from")
	to := c.Query("to")
	today := domain.Today(h.service.Now(), ident.User.Timezone)

	if !ValidateFilters(category, method, from, to, today) {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", map[string]string{
			"filters": "Periksa kategori, metode, dan rentang tanggal.",
		})
	}

	n, ok := cursor.ParseLimit(c.Query("limit"))
	if !ok {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", map[string]string{
			"limit": "Gunakan 1–100.",
		})
	}

	sc := cursor.BuildScope("logs", ident.User.ID, string(c.Request().URI().QueryString()))
	datePos, idPos := "", ""
	if raw := c.Query("cursor"); raw != "" {
		tok, e := h.service.DecodeCursor(raw, sc)
		if e != nil {
			return response.Fail(c, fiber.StatusBadRequest, "INVALID_REQUEST", nil)
		}
		if _, e = time.Parse(time.DateOnly, tok.Position); e != nil {
			return response.Fail(c, fiber.StatusBadRequest, "INVALID_REQUEST", nil)
		}
		datePos, idPos = tok.Position, tok.ID
	}

	res, err := h.service.ListLogs(c.Context(), ident.User.ID, sc, ListLogsQuery{
		Category: category,
		Method:   method,
		From:     from,
		To:       to,
		Limit:    n,
		DatePos:  datePos,
		IDPos:    idPos,
	})
	if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.Paginated(c, res.Items, res.HasMore, res.NextCursor)
}

// @Summary Update Waste Log
// @Description Partially update an existing waste log using optimistic concurrency control (version)
// @Tags Waste Logs
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Waste Log UUID"
// @Param req body UpdateLogReq true "Waste Log Patch Payload"
// @Success 200 {object} response.SuccessDataRes[LogWithProgressRes]
// @Failure 400,401,404,409,422,503 {object} response.ErrorResponse
// @Router /v1/waste-logs/{id} [patch]
func (h *Handler) UpdateLog(c fiber.Ctx) error {
	id := c.Params("id")
	if !domain.ValidUUID(id) {
		return response.Fail(c, fiber.StatusNotFound, "NOT_FOUND", nil)
	}
	var patch map[string]json.RawMessage
	if !response.DecodeJSON(c, &patch) {
		return nil
	}
	version := 0
	if e := json.Unmarshal(patch["version"], &version); e != nil || version < 1 {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", map[string]string{
			"version": "Muat versi catatan terlebih dahulu.",
		})
	}
	for key := range patch {
		if !domain.Contains([]string{"version", "category", "method", "quantity_kind", "weight_g", "item_count", "activity_date", "notes", "photo_id"}, key) {
			return response.Fail(c, fiber.StatusBadRequest, "INVALID_REQUEST", nil)
		}
	}

	ident := middleware.Who(c)
	res, err := h.service.UpdateLog(c.Context(), ident.User, id, version, patch)
	if err != nil {
		var valErr *ValidationError
		switch {
		case errors.Is(err, ErrLogNotFound):
			return response.Fail(c, fiber.StatusNotFound, "NOT_FOUND", nil)
		case errors.Is(err, ErrVersionConflict):
			return response.Fail(c, fiber.StatusConflict, "VERSION_CONFLICT", nil)
		case errors.Is(err, ErrPhotoAlreadyLinked):
			return response.Fail(c, fiber.StatusConflict, "PHOTO_ALREADY_LINKED", nil)
		case errors.Is(err, ErrInvalidPatchRequest):
			return response.Fail(c, fiber.StatusBadRequest, "INVALID_REQUEST", nil)
		case errors.As(err, &valErr):
			return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", valErr.Fields)
		default:
			return response.InternalError(c, h.log, err)
		}
	}
	return response.OK(c, res)
}

// @Summary Delete Waste Log
// @Description Soft-delete a waste log entry with If-Match version precondition
// @Tags Waste Logs
// @Security BearerAuth
// @Param id path string true "Waste Log UUID"
// @Param If-Match header string true "Quoted version number (e.g. \"1\")"
// @Success 204 "No Content"
// @Failure 400,401,404,409,428,503 {object} response.ErrorResponse
// @Router /v1/waste-logs/{id} [delete]
func (h *Handler) DeleteLog(c fiber.Ctx) error {
	id := c.Params("id")
	if !domain.ValidUUID(id) {
		return response.Fail(c, fiber.StatusNotFound, "NOT_FOUND", nil)
	}
	header := c.Get("If-Match")
	if header == "" {
		return response.Fail(c, fiber.StatusPreconditionRequired, "PRECONDITION_REQUIRED", nil)
	}
	version, e := strconv.Atoi(strings.Trim(header, "\""))
	if e != nil || version < 1 || header != "\""+strconv.Itoa(version)+"\"" {
		return response.Fail(c, fiber.StatusBadRequest, "INVALID_REQUEST", nil)
	}

	ident := middleware.Who(c)
	err := h.service.DeleteLog(c.Context(), ident.User, id, version)
	if errors.Is(err, ErrLogNotFound) {
		return response.Fail(c, fiber.StatusNotFound, "NOT_FOUND", nil)
	}
	if errors.Is(err, ErrVersionConflict) {
		return response.Fail(c, fiber.StatusConflict, "VERSION_CONFLICT", nil)
	}
	if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.NoContent(c)
}
