package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestRequestIDReusesOnlyValidUUIDs(t *testing.T) {
	app := fiber.New()
	app.Use(RequestID(nil))
	app.Get("/", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })

	const supplied = "6f1c9a52-3b0e-4c41-9a7e-2d8f0c1b5e77"
	for _, tc := range []struct {
		header string
		reuse  bool
	}{{supplied, true}, {"not-a-uuid\r\nInjected: 1", false}, {"", false}} {
		req := httptest.NewRequest(fiber.MethodGet, "/", nil)
		if tc.header != "" {
			req.Header.Set("X-Request-ID", tc.header)
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		got := resp.Header.Get("X-Request-ID")
		if tc.reuse && got != supplied {
			t.Fatalf("expected supplied id, got %q", got)
		}
		if !tc.reuse && (got == "" || got == tc.header) {
			t.Fatalf("expected fresh id for %q, got %q", tc.header, got)
		}
	}
}
