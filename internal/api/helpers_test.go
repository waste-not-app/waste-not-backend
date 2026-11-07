package api

import (
	"encoding/json"
	"net/http"
	"time"

	aiMod "github.com/waste-not-app/waste-not-backend/internal/modules/ai"
)

const maxImage = aiMod.MaxImageBytes

func mustDate(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		_ = json.NewEncoder(w).Encode(value)
	}
}
