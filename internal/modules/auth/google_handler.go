package auth

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/response"
)

// @Summary Issue a Google sign-in challenge
// @Description Return a five-minute single-use nonce. Pass it to Google when requesting an ID token for sign-in or account deletion.
// @Tags Auth
// @Produce json
// @Success 200 {object} response.SuccessDataRes[GoogleChallengeRes]
// @Failure 400,429,503 {object} response.ErrorResponse
// @Router /v1/auth/google/challenge [post]
func (h *Handler) IssueGoogleChallenge(c fiber.Ctx) error {
	if !h.service.GoogleEnabled() {
		return response.Busy(c, "GOOGLE_AUTH_UNAVAILABLE")
	}
	if len(c.Body()) != 0 {
		return response.Fail(c, fiber.StatusBadRequest, "INVALID_REQUEST", nil)
	}
	ok, err := h.limiter.CheckAuthIPLimit(c.Context(), c.IP())
	if err != nil {
		response.LogRedactedError(c, h.log, err)
		return response.Busy(c, "GOOGLE_AUTH_UNAVAILABLE")
	}
	if !ok {
		return response.RateLimited(c, 900)
	}
	challenge, err := h.service.GoogleChallenge(c.Context())
	if err != nil {
		response.LogRedactedError(c, h.log, err)
		return response.Busy(c, "GOOGLE_AUTH_UNAVAILABLE")
	}
	return response.OK(c, challenge)
}

// @Summary Sign in with Google
// @Description Exchange a verified Google ID token and single-use nonce for a WasteNot session. Linking an existing password account also requires its current password.
// @Tags Auth
// @Accept json
// @Produce json
// @Param req body GoogleLoginReq true "Google ID token, time zone, and optional WasteNot linking proof"
// @Success 200 {object} response.SuccessDataRes[AuthTokensRes]
// @Failure 400,401,409,413,415,422,429,503 {object} response.ErrorResponse
// @Router /v1/auth/google [post]
func (h *Handler) GoogleLogin(c fiber.Ctx) error {
	if !h.service.GoogleEnabled() {
		return response.Busy(c, "GOOGLE_AUTH_UNAVAILABLE")
	}
	ok, err := h.limiter.CheckAuthIPLimit(c.Context(), c.IP())
	if !h.allowAuthRateLimit(c, ok, err) {
		return nil
	}
	var req GoogleLoginReq
	if !response.DecodeJSON(c, &req) {
		return nil
	}
	if !domain.ValidTimezone(req.Timezone) {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", map[string]string{"timezone": "Choose an IANA time zone."})
	}
	tokens, err := h.service.GoogleLogin(c.Context(), req)
	switch {
	case err == nil:
		return response.OK(c, tokens)
	case errors.Is(err, ErrUnauthenticated):
		return response.Fail(c, fiber.StatusUnauthorized, "UNAUTHENTICATED", nil)
	case errors.Is(err, ErrGoogleAuthUnavailable):
		return response.Busy(c, "GOOGLE_AUTH_UNAVAILABLE")
	case errors.Is(err, ErrGoogleLinkRequired):
		return response.Fail(c, fiber.StatusConflict, "GOOGLE_LINK_REQUIRED", nil)
	case errors.Is(err, ErrGoogleIdentityConflict):
		return response.Fail(c, fiber.StatusConflict, "GOOGLE_IDENTITY_CONFLICT", nil)
	case errors.Is(err, ErrGoogleRateLimited):
		return response.RateLimited(c, 900)
	case errors.Is(err, ErrGoogleHashBusy):
		return response.Busy(c, "UNAVAILABLE")
	default:
		return response.InternalErrorRedacted(c, h.log, err)
	}
}
