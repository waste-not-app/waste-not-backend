package guides

import (
	"errors"
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"

	"github.com/waste-not-app/waste-not-backend/internal/cursor"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/middleware"
	"github.com/waste-not-app/waste-not-backend/internal/response"
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

	group := app.Group("/v1/waste-guide", authMw)
	group.Get("", handler.ListGuides)
	group.Get("/:id", handler.GetGuide)
}

// @Summary Get Waste Guide by ID
// @Description Retrieve detailed instructions and tips for a specific waste guide entry
// @Tags Guides
// @Produce json
// @Security BearerAuth
// @Param id path string true "Guide ID"
// @Success 200 {object} response.SuccessDataRes[GuideRes]
// @Failure 401,404,503 {object} response.ErrorResponse
// @Router /v1/waste-guide/{id} [get]
func (h *Handler) GetGuide(c fiber.Ctx) error {
	g, err := h.service.GetGuide(c.Context(), c.Params("id"))
	if errors.Is(err, ErrGuideNotFound) {
		return response.Fail(c, fiber.StatusNotFound, "NOT_FOUND", nil)
	}
	if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.OK(c, g)
}

// @Summary List Waste Guides
// @Description Search and filter waste disposal guides with cursor pagination
// @Tags Guides
// @Produce json
// @Security BearerAuth
// @Param q query string false "Search query (max 100 chars)"
// @Param category query string false "Waste Category (ORGANIC, PLASTIC, PAPER, GLASS, METAL, ELECTRONIC, OTHER)"
// @Param limit query int false "Page size (1-100, default 20)"
// @Param cursor query string false "Opaque pagination cursor"
// @Success 200 {object} response.PaginatedDataRes[GuideRes]
// @Failure 400,401,422,503 {object} response.ErrorResponse
// @Router /v1/waste-guide [get]
func (h *Handler) ListGuides(c fiber.Ctx) error {
	query := strings.TrimSpace(c.Query("q"))
	category := c.Query("category")
	n, ok := cursor.ParseLimit(c.Query("limit"))
	if !ok || utf8.RuneCountInString(query) > 100 || (category != "" && !slices.Contains(domain.Categories, category)) {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", map[string]string{
			"q": "Periksa pencarian, kategori, dan batas halaman.",
		})
	}

	ident := middleware.Who(c)
	sc := cursor.BuildScope("guides", ident.User.ID, string(c.Request().URI().QueryString()))
	res, err := h.service.ListGuides(c.Context(), sc, ListGuidesQuery{
		Query:    query,
		Category: category,
		Limit:    n,
		Cursor:   c.Query("cursor"),
	})
	if errors.Is(err, ErrInvalidCursor) {
		return response.Fail(c, fiber.StatusBadRequest, "INVALID_REQUEST", nil)
	}
	if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.Paginated(c, res.Items, res.HasMore, res.NextCursor)
}
