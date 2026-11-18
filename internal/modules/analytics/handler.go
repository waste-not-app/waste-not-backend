package analytics

import (
	"errors"
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

	app.Get("/v1/dashboard", authMw, handler.GetDashboard)
	app.Get("/v1/statistics", authMw, handler.GetStatistics)
}

// @Summary Get Waste Statistics
// @Description Retrieve aggregated waste statistics, daily trends, and category breakdown for a date range
// @Tags Analytics
// @Produce json
// @Security BearerAuth
// @Param from query string false "Start Date (YYYY-MM-DD, defaults to 6 days before 'to')"
// @Param to query string false "End Date (YYYY-MM-DD, defaults to user's today)"
// @Success 200 {object} response.SuccessDataRes[StatisticsRes]
// @Failure 401,422,503 {object} response.ErrorResponse
// @Router /v1/statistics [get]
func (h *Handler) GetStatistics(c fiber.Ctx) error {
	ident := middleware.Who(c)
	res, err := h.service.GetStatistics(c.Context(), ident.User, c.Query("from"), c.Query("to"))
	if errors.Is(err, ErrInvalidDateRange) {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", map[string]string{
			"from": "Gunakan rentang tanggal hingga 366 hari tanpa tanggal mendatang.",
		})
	}
	if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.OK(c, res)
}

// @Summary Get Today's Dashboard
// @Description Retrieve today's waste summary, recent logs, and gamification progress
// @Tags Analytics
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.SuccessDataRes[DashboardRes]
// @Failure 401,503 {object} response.ErrorResponse
// @Router /v1/dashboard [get]
func (h *Handler) GetDashboard(c fiber.Ctx) error {
	ident := middleware.Who(c)
	res, err := h.service.GetDashboard(c.Context(), ident.User)
	if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.OK(c, res)
}
