package system

import (
	"github.com/gofiber/fiber/v3"

	"github.com/waste-not-app/waste-not-backend/internal/shared/response"
)

type Handler struct {
	service Service
}

func RegisterHTTPRoutes(app *fiber.App, service Service, authMw fiber.Handler) {
	handler := &Handler{service: service}

	app.Get("/health/live", handler.HealthLive)
	app.Get("/health/ready", handler.HealthReady)
	app.Get("/v1/metadata", authMw, handler.GetMetadata)
}

// @Summary Liveness Probe
// @Description Check if the API process is alive
// @Tags System
// @Produce json
// @Success 200 {object} HealthLiveRes
// @Router /health/live [get]
func (h *Handler) HealthLive(c fiber.Ctx) error {
	return response.Raw(c, fiber.StatusOK, HealthLiveRes{Live: true})
}

// @Summary Readiness Probe
// @Description Check if PostgreSQL database and core schema migrations are ready
// @Tags System
// @Produce json
// @Success 200 {object} HealthReadyRes
// @Failure 503 {object} response.ErrorResponse
// @Router /health/ready [get]
func (h *Handler) HealthReady(c fiber.Ctx) error {
	if rel := h.service.ReleaseVersion(); rel != "" {
		c.Set("X-WasteNot-Release", rel)
	}
	if err := h.service.CheckReadiness(c.Context()); err != nil {
		return response.Fail(c, fiber.StatusServiceUnavailable, "UNAVAILABLE", nil)
	}
	return response.Raw(c, fiber.StatusOK, HealthReadyRes{Ready: true})
}

// @Summary Get Client Metadata & Capabilities
// @Description Retrieve supported waste categories, methods, validation limits, and feature capabilities
// @Tags System
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.SuccessDataRes[MetadataRes]
// @Failure 401,503 {object} response.ErrorResponse
// @Router /v1/metadata [get]
func (h *Handler) GetMetadata(c fiber.Ctx) error {
	res := h.service.GetMetadata(c.Context(), response.RequestID(c))
	return response.OK(c, res)
}
