package middleware

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/waste-not-app/waste-not-backend/internal/database/db"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/ratelimit"
	"github.com/waste-not-app/waste-not-backend/internal/response"
	"github.com/waste-not-app/waste-not-backend/internal/security"
)

type Identity struct {
	User       domain.User
	SessionID  string
	Generation int
}

func Protected(dbPool *pgxpool.Pool, signer security.Signer, limiter *ratelimit.Limiter, log *slog.Logger) fiber.Handler {
	q := db.New(dbPool)

	return func(c fiber.Ctx) error {
		raw := c.Get(fiber.HeaderAuthorization)
		if !strings.HasPrefix(raw, "Bearer ") || len(raw) > 4096 {
			return response.Fail(c, fiber.StatusUnauthorized, "UNAUTHENTICATED", nil)
		}
		claims, err := signer.Parse(strings.TrimPrefix(raw, "Bearer "))
		if err != nil {
			return response.Fail(c, fiber.StatusUnauthorized, "SESSION_EXPIRED", nil)
		}

		row, err := q.GetSessionAndUser(c.Context(), db.GetSessionAndUserParams{
			ID:     claims.SessionID,
			UserID: claims.Subject,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return response.Fail(c, fiber.StatusUnauthorized, "SESSION_EXPIRED", nil)
		}
		if err != nil {
			return response.InternalError(c, log, err)
		}

		now := limiter.Now()
		if !row.ExpiresAt.After(now) || ((row.Revoked || row.Generation != claims.Generation) && c.Path() != "/v1/auth/logout") {
			return response.Fail(c, fiber.StatusUnauthorized, "SESSION_EXPIRED", nil)
		}

		if ok, err := limiter.Allow(c.Context(), "api:"+row.ID, 300, time.Minute); err != nil {
			return response.InternalError(c, log, err)
		} else if !ok {
			return response.RateLimited(c, 60)
		}

		u := domain.User{
			ID:       row.ID,
			Name:     row.Name,
			Email:    row.NormalizedEmail,
			Timezone: row.Timezone,
		}
		ident := Identity{
			User:       u,
			SessionID:  claims.SessionID,
			Generation: claims.Generation,
		}
		c.Locals("identity", ident)
		c.Locals("user_id", u.ID)
		return c.Next()
	}
}

func Who(c fiber.Ctx) Identity {
	if ident, ok := c.Locals("identity").(Identity); ok {
		return ident
	}
	return Identity{}
}
