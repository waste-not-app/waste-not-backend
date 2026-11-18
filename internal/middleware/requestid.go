package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/waste-not-app/waste-not-backend/internal/auth"
	"github.com/waste-not-app/waste-not-backend/internal/shared/response"
)

func RequestID(log *slog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		id := c.Get("X-Request-ID")
		if !auth.UUID(id) {
			id = uuid.NewString()
		}
		c.Locals("request_id", id)
		c.Set("X-Request-ID", id)
		c.Set("Cache-Control", "no-store")
		c.Set("X-Content-Type-Options", "nosniff")

		started := time.Now()
		ctx, cancel := context.WithTimeout(c.Context(), 35*time.Second)
		defer cancel()
		c.SetContext(ctx)

		defer func() {
			if recover() != nil {
				if len(c.Response().Body()) == 0 {
					_ = response.Fail(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", nil)
				}
				if log != nil {
					log.Error("request panic", "request_id", id)
				}
			}
			if log != nil {
				log.Info("request", "request_id", id, "status", c.Response().StatusCode(), "duration_ms", time.Since(started).Milliseconds())
			}
		}()

		return c.Next()
	}
}
