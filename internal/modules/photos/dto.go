package photos

import "time"

type PhotoRes struct {
	ID         string    `json:"id"`
	MimeType   string    `json:"mime_type"`
	SizeBytes  int       `json:"size_bytes"`
	Status     string    `json:"status"`
	PreviewURL string    `json:"preview_url"`
	CreatedAt  time.Time `json:"created_at"`
}
