package auth

import (
	"errors"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"

	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/middleware"
	"github.com/waste-not-app/waste-not-backend/internal/response"
	"github.com/waste-not-app/waste-not-backend/internal/security"
)

// @Summary Request password reset
// @Description Accept a conditional recovery request. A code is emailed only for an existing account, never returned here.
// @Tags Auth
// @Accept json
// @Produce json
// @Param req body PasswordResetReq true "Account email"
// @Success 202 {object} response.SuccessDataRes[PasswordResetRes]
// @Failure 400,413,415,422,429,503 {object} response.ErrorResponse
// @Router /v1/auth/password-reset [post]
func (h *Handler) RequestPasswordReset(c fiber.Ctx) error {
	var req PasswordResetReq
	if !response.DecodeJSON(c, &req) {
		return nil
	}
	email, valid := domain.Email(req.Email)
	if !h.checkAuthRateLimit(c, "password-reset:"+email) {
		return nil
	}
	if !valid {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", map[string]string{"email": "Periksa format email."})
	}
	if err := h.service.RequestPasswordReset(c.Context(), email); errors.Is(err, ErrMailUnavailable) {
		return response.Fail(c, fiber.StatusServiceUnavailable, "UNAVAILABLE", nil)
	} else if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.Data(c, fiber.StatusAccepted, PasswordResetRes{Message: "Jika email terdaftar, permintaan pengaturan ulang kata sandi akan diproses. Periksa email untuk kode pemulihan."})
}

// @Summary Confirm password reset
// @Description Consume a single-use recovery code, replace the password and revoke every session atomically.
// @Tags Auth
// @Accept json
// @Param req body PasswordResetConfirmReq true "Recovery code and new password"
// @Success 204 "No Content"
// @Failure 400,413,415,422,429,503 {object} response.ErrorResponse
// @Router /v1/auth/password-reset/confirm [post]
func (h *Handler) ConfirmPasswordReset(c fiber.Ctx) error {
	var req PasswordResetConfirmReq
	if !response.DecodeJSON(c, &req) {
		return nil
	}
	if !h.checkAuthRateLimit(c, "password-reset-confirm:"+security.Digest([]byte(req.Token))) {
		return nil
	}
	fields := map[string]string{}
	if len(req.Token) != 43 {
		fields["token"] = "Kode pemulihan tidak valid atau sudah kedaluwarsa."
	}
	if n := utf8.RuneCountInString(req.Password); n < domain.MinPasswordLength || n > domain.MaxPasswordLength {
		fields["password"] = "Gunakan kata sandi 12–128 karakter."
	}
	if len(fields) > 0 {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", fields)
	}
	if !h.limiter.HashSlots.TryAcquire() {
		return response.Busy(c, "UNAVAILABLE")
	}
	defer h.limiter.HashSlots.Release()
	err := h.service.ConfirmPasswordReset(c.Context(), req.Token, req.Password)
	if errors.Is(err, ErrInvalidResetToken) {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "INVALID_RESET_TOKEN", map[string]string{"token": "Kode pemulihan tidak valid atau sudah kedaluwarsa."})
	}
	if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.NoContent(c)
}

// @Summary Delete current account
// @Description Verify the current password, cascade owned data and remove private photo files. Storage failures return 503 with durable cleanup pending, never a confirmed purge.
// @Tags Auth
// @Security BearerAuth
// @Accept json
// @Param req body DeleteMeReq true "Current password"
// @Success 204 "No Content"
// @Failure 400,401,403,413,415,422,429,503 {object} response.ErrorResponse
// @Router /v1/me [delete]
func (h *Handler) DeleteMe(c fiber.Ctx) error {
	var req DeleteMeReq
	if !response.DecodeJSON(c, &req) {
		return nil
	}
	ident := middleware.Who(c)
	if !h.checkAuthRateLimit(c, "delete-me:"+ident.User.ID) {
		return nil
	}
	if n := utf8.RuneCountInString(req.Password); n < 1 || n > domain.MaxPasswordLength {
		return response.Fail(c, fiber.StatusUnprocessableEntity, "VALIDATION_ERROR", map[string]string{"password": "Masukkan kata sandi saat ini."})
	}
	if !h.limiter.HashSlots.TryAcquire() {
		return response.Busy(c, "UNAVAILABLE")
	}
	defer h.limiter.HashSlots.Release()
	err := h.service.DeleteMe(c.Context(), ident.User.ID, req.Password)
	if errors.Is(err, ErrWrongPassword) {
		return response.Fail(c, fiber.StatusForbidden, "WRONG_PASSWORD", map[string]string{"password": "Kata sandi saat ini tidak sesuai."})
	}
	if errors.Is(err, ErrSessionExpired) {
		return response.Fail(c, fiber.StatusUnauthorized, "SESSION_EXPIRED", nil)
	}
	if errors.Is(err, ErrDeletionPending) {
		return response.Fail(c, fiber.StatusServiceUnavailable, "ACCOUNT_DELETION_PENDING", nil)
	}
	if err != nil {
		return response.InternalError(c, h.log, err)
	}
	return response.NoContent(c)
}
