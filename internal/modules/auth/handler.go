package auth

import (
	"errors"
	"log/slog"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"

	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/middleware"
	"github.com/waste-not-app/waste-not-backend/internal/security"
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

	v1Auth := app.Group("/v1/auth")
	v1Auth.Post("/register", handler.Register)
	v1Auth.Post("/login", handler.Login)
	v1Auth.Post("/refresh", handler.RefreshToken)
	v1Auth.Post("/logout", authMw, handler.Logout)
	v1Auth.Post("/password-reset", handler.RequestPasswordReset)
	v1Auth.Post("/password-reset/confirm", handler.ConfirmPasswordReset)

	app.Get("/v1/me", authMw, handler.GetMe)
	app.Delete("/v1/me", authMw, handler.DeleteMe)
}

func (h *Handler) checkAuthRateLimit(c fiber.Ctx, key string) bool {
	ok, err := h.limiter.CheckAuthLimit(c.Context(), c.IP(), key)
	if err != nil {
		_ = response.InternalError(c, h.log, err)
		return false
	}
	if !ok {
		_ = response.RateLimited(c, 900)
		return false
	}
	return true
}

// @Summary Register User
// @Description Register a new WasteNot user account and issue initial session tokens
// @Tags Auth
// @Accept json
// @Produce json
// @Param req body RegisterReq true "Registration Data"
// @Success 201 {object} response.SuccessDataRes[AuthTokensRes]
// @Failure 400,409,422,429,503 {object} response.ErrorResponse
// @Router /v1/auth/register [post]
func (h *Handler) Register(c fiber.Ctx) error {
	var req RegisterReq
	if !response.DecodeJSON(c, &req) {
		return nil
	}
	email, validEmail := domain.Email(req.Email)
	if !h.checkAuthRateLimit(c, email) {
		return nil
	}
	if fields := req.Validate(validEmail); len(fields) > 0 {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", fields)
	}
	if !h.limiter.AcquireHashSlot(c) {
		return nil
	}
	defer h.limiter.ReleaseHashSlot()

	tokens, err := h.service.Register(c.Context(), req, email)

	if errors.Is(err, ErrEmailExists) {
		return response.Fail(c, fiber.StatusConflict, "EMAIL_EXISTS", map[string]string{"email": "Email sudah digunakan."})
	}
	if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.Created(c, tokens)
}

// @Summary Login User
// @Description Authenticate with email and password to receive access and refresh tokens
// @Tags Auth
// @Accept json
// @Produce json
// @Param req body LoginReq true "Login Credentials"
// @Success 200 {object} response.SuccessDataRes[AuthTokensRes]
// @Failure 400,401,429,503 {object} response.ErrorResponse
// @Router /v1/auth/login [post]
func (h *Handler) Login(c fiber.Ctx) error {
	var req LoginReq
	if !response.DecodeJSON(c, &req) {
		return nil
	}
	email, validEmail := domain.Email(req.Email)
	if !h.checkAuthRateLimit(c, email) {
		return nil
	}
	if !validEmail || utf8.RuneCountInString(req.Password) > domain.MaxPasswordLength {
		return response.Fail(c, fiber.StatusUnauthorized, "UNAUTHENTICATED", nil)
	}
	if !h.limiter.AcquireHashSlot(c) {
		return nil
	}
	defer h.limiter.ReleaseHashSlot()

	tokens, err := h.service.Login(c.Context(), email, req.Password)
	if errors.Is(err, ErrUnauthenticated) {
		return response.Fail(c, fiber.StatusUnauthorized, "UNAUTHENTICATED", nil)
	}
	if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.OK(c, tokens)
}

// @Summary Refresh Access Token
// @Description Rotate refresh token and issue a new access/refresh token pair
// @Tags Auth
// @Accept json
// @Produce json
// @Param req body RefreshTokenReq true "Refresh Token Payload"
// @Success 200 {object} response.SuccessDataRes[AuthTokensRes]
// @Failure 400,401,429,503 {object} response.ErrorResponse
// @Router /v1/auth/refresh [post]
func (h *Handler) RefreshToken(c fiber.Ctx) error {
	var req RefreshTokenReq
	if !response.DecodeJSON(c, &req) {
		return nil
	}
	if !h.checkAuthRateLimit(c, "refresh:"+security.Digest([]byte(req.RefreshToken))) {
		return nil
	}
	tokens, err := h.service.RefreshToken(c.Context(), req.RefreshToken)
	if errors.Is(err, ErrSessionExpired) {
		return response.Fail(c, fiber.StatusUnauthorized, "SESSION_EXPIRED", nil)
	}
	if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.OK(c, tokens)
}

// @Summary Logout Session
// @Description Revoke the current authenticated session and refresh token chain
// @Tags Auth
// @Security BearerAuth
// @Success 204 "No Content"
// @Failure 401,403,503 {object} response.ErrorResponse
// @Router /v1/auth/logout [post]
func (h *Handler) Logout(c fiber.Ctx) error {
	ident := middleware.Who(c)
	if err := h.service.Logout(c.Context(), ident.SessionID, ident.User.ID); err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.NoContent(c)
}

// @Summary Get Current User Profile
// @Description Retrieve the authenticated user's profile and gamification progress
// @Tags Auth
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.SuccessDataRes[UserRes]
// @Failure 401,503 {object} response.ErrorResponse
// @Router /v1/me [get]
func (h *Handler) GetMe(c fiber.Ctx) error {
	ident := middleware.Who(c)
	res, err := h.service.GetMe(c.Context(), ident.User)
	if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.OK(c, res)
}
