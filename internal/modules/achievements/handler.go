package achievements

import (
	"log/slog"

	"github.com/gofiber/fiber/v3"

	"github.com/waste-not-app/waste-not-backend/internal/middleware"
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

	app.Get("/v1/achievements", authMw, handler.ListAchievements)
}

// @Summary List User Achievements
// @Description Retrieve achievement definitions and derived unlock/progress state for the authenticated user
// @Tags Achievements
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.SuccessDataRes[[]AchievementRes]
// @Failure 401,503 {object} response.ErrorResponse
// @Router /v1/achievements [get]
func (h *Handler) ListAchievements(c fiber.Ctx) error {
	ident := middleware.Who(c)
	items, err := h.service.ListAchievements(c.Context(), ident.User)
	if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.OK(c, items)
}
