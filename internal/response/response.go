package response

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
)

const (
	APIVersion  = "1.0.0"
	MaxJSONBody = 16384
)

type SuccessDataRes[T any] struct {
	Data       T      `json:"data"`
	RequestID  string `json:"request_id"`
	APIVersion string `json:"api_version"`
}

type PageInfo struct {
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

type PaginatedDataRes[T any] struct {
	Data []T      `json:"data"`
	Page PageInfo `json:"page"`
}

type ErrorDetail struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields"`
	Retryable bool              `json:"retryable"`
}

type ErrorResponse struct {
	APIVersion string      `json:"api_version"`
	RequestID  string      `json:"request_id"`
	Error      ErrorDetail `json:"error"`
}

func RequestID(c fiber.Ctx) string {
	if id, ok := c.Locals("request_id").(string); ok && id != "" {
		return id
	}
	return c.GetRespHeader("X-Request-ID")
}

func Raw(c fiber.Ctx, status int, value any) error {
	c.Set(fiber.HeaderContentType, "application/json; charset=utf-8")
	if status == fiber.StatusNoContent {
		return c.SendStatus(status)
	}
	return c.Status(status).JSON(value)
}

func Data(c fiber.Ctx, status int, v any) error {
	return Raw(c, status, fiber.Map{
		"data":        v,
		"request_id":  RequestID(c),
		"api_version": APIVersion,
	})
}

func OK(c fiber.Ctx, v any) error {
	return Data(c, fiber.StatusOK, v)
}

func Created(c fiber.Ctx, v any) error {
	return Data(c, fiber.StatusCreated, v)
}

func Paginated(c fiber.Ctx, items any, hasMore bool, nextCursor any) error {
	return Raw(c, fiber.StatusOK, fiber.Map{
		"data": items,
		"page": fiber.Map{
			"has_more":    hasMore,
			"next_cursor": nextCursor,
		},
	})
}

func NoContent(c fiber.Ctx) error {
	return c.SendStatus(fiber.StatusNoContent)
}

func Fail(c fiber.Ctx, status int, code string, fields map[string]string) error {
	if fields == nil {
		fields = map[string]string{}
	}
	message := "We couldn't process this request."
	switch {
	case status == fiber.StatusUnauthorized:
		message = "Please sign in again."
	case status == fiber.StatusNotFound:
		message = "Not found."
	case status == fiber.StatusUnprocessableEntity:
		message = "Check your input."
	case status >= fiber.StatusInternalServerError:
		message = "The service is unavailable. Try again later."
	}
	return Raw(c, status, fiber.Map{
		"api_version": APIVersion,
		"request_id":  RequestID(c),
		"error": fiber.Map{
			"code":      code,
			"message":   message,
			"fields":    fields,
			"retryable": status == fiber.StatusTooManyRequests || status >= fiber.StatusInternalServerError,
		},
	})
}

func RateLimited(c fiber.Ctx, seconds int) error {
	c.Set("Retry-After", strconv.Itoa(seconds))
	return Fail(c, fiber.StatusTooManyRequests, "RATE_LIMITED", nil)
}

func Busy(c fiber.Ctx, code string) error {
	c.Set(fiber.HeaderRetryAfter, "1")
	return Fail(c, fiber.StatusServiceUnavailable, code, nil)
}

func InternalError(c fiber.Ctx, log *slog.Logger, err error) error {
	if log != nil {
		log.Error("request failed", "request_id", RequestID(c), "detail", err)
	}
	return Fail(c, fiber.StatusServiceUnavailable, "UNAVAILABLE", nil)
}

func InternalErrorRedacted(c fiber.Ctx, log *slog.Logger, err error) error {
	LogRedactedError(c, log, err)
	return Fail(c, fiber.StatusServiceUnavailable, "UNAVAILABLE", nil)
}

func LogRedactedError(c fiber.Ctx, log *slog.Logger, err error) {
	if log != nil {
		log.Error("authentication request failed", "request_id", RequestID(c), "error_type", fmt.Sprintf("%T", err))
	}
}

func DecodeJSON(c fiber.Ctx, v any) bool {
	media, _, err := mime.ParseMediaType(c.Get(fiber.HeaderContentType))
	if err != nil || media != "application/json" {
		_ = Fail(c, fiber.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", nil)
		return false
	}
	body := c.Body()
	if len(body) > MaxJSONBody {
		_ = Fail(c, fiber.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", nil)
		return false
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	d.DisallowUnknownFields()
	err = d.Decode(v)
	if err == nil {
		if e := d.Decode(new(any)); e != io.EOF {
			err = errors.New("trailing JSON")
		}
	}
	if err != nil {
		_ = Fail(c, fiber.StatusBadRequest, "INVALID_REQUEST", nil)
		return false
	}
	return true
}

func ErrorHandler(log *slog.Logger) fiber.ErrorHandler {
	return func(c fiber.Ctx, err error) error {
		if len(c.Response().Body()) > 0 {
			return nil
		}
		var fiberErr *fiber.Error
		if errors.As(err, &fiberErr) {
			switch fiberErr.Code {
			case fiber.StatusMethodNotAllowed:
				return Fail(c, fiber.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", nil)
			case fiber.StatusNotFound:
				return Fail(c, fiber.StatusNotFound, "NOT_FOUND", nil)
			case fiber.StatusRequestEntityTooLarge:
				return Fail(c, fiber.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", nil)
			}
		}
		path := strings.TrimSuffix(c.Path(), "/")
		const authPrefix = "/v1/auth/"
		if strings.EqualFold(path, "/v1/me") || (len(path) >= len(authPrefix) && strings.EqualFold(path[:len(authPrefix)], authPrefix)) {
			LogRedactedError(c, log, err)
		} else if log != nil {
			log.Error("unhandled error", "request_id", RequestID(c), "detail", err)
		}
		return Fail(c, fiber.StatusInternalServerError, "INTERNAL_ERROR", nil)
	}
}
