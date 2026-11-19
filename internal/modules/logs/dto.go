package logs

import (
	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

type LogWithProgressRes struct {
	domain.Log
	Progress domain.Progress `json:"progress"`
}

type UpdateLogReq struct {
	Version      int     `json:"version"`
	Category     *string `json:"category,omitempty"`
	Method       *string `json:"method,omitempty"`
	QuantityKind *string `json:"quantity_kind,omitempty"`
	WeightG      *int    `json:"weight_g,omitempty"`
	ItemCount    *int    `json:"item_count,omitempty"`
	ActivityDate *string `json:"activity_date,omitempty"`
	Notes        *string `json:"notes,omitempty"`
	PhotoID      *string `json:"photo_id,omitempty"`
}

type ListLogsParams struct {
	Category string
	Method   string
	From     string
	To       string
	Limit    string
	Cursor   string
}

type ListLogsQuery struct {
	Category string
	Method   string
	From     string
	To       string
	Limit    int
	DatePos  string
	IDPos    string
}

type ListLogsRes struct {
	Items      []domain.Log
	HasMore    bool
	NextCursor any
}
