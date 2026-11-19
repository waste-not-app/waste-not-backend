package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type Log struct {
	ID               string    `json:"id"`
	Category         string    `json:"category"`
	Method           string    `json:"method"`
	QuantityKind     string    `json:"quantity_kind"`
	WeightG          *int      `json:"weight_g"`
	ItemCount        *int      `json:"item_count"`
	ActivityDate     string    `json:"activity_date"`
	ActivityTimezone string    `json:"activity_timezone"`
	Notes            string    `json:"notes"`
	PhotoID          *string   `json:"photo_id,omitempty"`
	Version          int       `json:"version"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Provenance struct {
	RecommendationID    string `json:"recommendation_id"`
	UserConfirmedObject bool   `json:"user_confirmed_object"`
	UserConfirmedMethod bool   `json:"user_confirmed_method"`
}

type LogInput struct {
	ClientRequestID string      `json:"client_request_id"`
	Category        string      `json:"category"`
	Method          string      `json:"method"`
	QuantityKind    string      `json:"quantity_kind"`
	WeightG         *int        `json:"weight_g"`
	ItemCount       *int        `json:"item_count"`
	ActivityDate    string      `json:"activity_date"`
	Notes           string      `json:"notes"`
	PhotoID         *string     `json:"photo_id"`
	AIProvenance    *Provenance `json:"ai_provenance,omitempty"`
}

func (i *LogInput) Validate(today, oldDate string) map[string]string {
	i.Notes = strings.TrimSpace(i.Notes)
	f := map[string]string{}
	if !slices.Contains(Categories, i.Category) {
		f["category"] = "Pilih kategori yang tersedia."
	}
	if !slices.Contains(Methods, i.Method) {
		f["method"] = "Pilih metode yang tersedia."
	}
	switch i.QuantityKind {
	case "WEIGHT":
		if i.WeightG == nil || *i.WeightG < MinWeightG || *i.WeightG > MaxWeightG || i.ItemCount != nil {
			f["weight_g"] = "Gunakan 1–100000 gram tanpa jumlah benda."
		}
	case "COUNT":
		if i.ItemCount == nil || *i.ItemCount < MinItemCount || *i.ItemCount > MaxItemCount || i.WeightG != nil {
			f["item_count"] = "Gunakan 1–10000 benda tanpa berat."
		}
	default:
		f["quantity_kind"] = "Pilih WEIGHT atau COUNT."
	}
	d, e := time.Parse(time.DateOnly, i.ActivityDate)
	t, _ := time.Parse(time.DateOnly, today)
	if e != nil || (i.ActivityDate != oldDate && (d.After(t) || d.Before(t.AddDate(0, 0, -ActivityDatePastDays)))) {
		f["activity_date"] = "Pilih tanggal dalam 30 hari terakhir."
	}
	if utf8.RuneCountInString(i.Notes) > MaxNotesLength {
		f["notes"] = "Catatan maksimal 500 karakter."
	}
	if i.PhotoID != nil {
		if _, err := uuid.Parse(*i.PhotoID); err != nil {
			f["photo_id"] = "Pilih foto lampiran yang valid."
		}
	}
	return f
}
