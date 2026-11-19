package domain

import (
	"net/mail"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	MinWeightG           = 1
	MaxWeightG           = 100000
	MinItemCount         = 1
	MaxItemCount         = 10000
	MinNameLength        = 2
	MaxNameLength        = 60
	MaxEmailLength       = 254
	MinPasswordLength    = 12
	MaxPasswordLength    = 128
	MaxNotesLength       = 500
	ActivityDatePastDays = 30
	KgMaxDecimalPlaces   = 3
)

var Categories = []string{"ORGANIC", "PLASTIC", "PAPER", "GLASS", "METAL", "ELECTRONIC", "OTHER"}
var Methods = []string{"REDUCE", "REUSE", "RECYCLE", "TRASH"}

func Contains(values []string, s string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}
func Email(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	a, e := mail.ParseAddress(s)
	return s, e == nil && a.Address == s && len(s) <= MaxEmailLength && strings.Contains(s, "@") && !strings.ContainsAny(s, "\r\n ")
}
func ValidTimezone(s string) bool {
	if s == "" || s == "Local" {
		return false
	}
	_, e := time.LoadLocation(s)
	return e == nil
}

func ValidUUID(s string) bool {
	u, err := uuid.Parse(s)
	return err == nil && u != uuid.Nil && u.String() == s
}

var locations sync.Map

func Location(zone string) *time.Location {
	if cached, ok := locations.Load(zone); ok {
		return cached.(*time.Location)
	}
	loc, err := time.LoadLocation(zone)
	if err != nil || loc == nil || zone == "" || zone == "Local" {
		return time.UTC
	}
	locations.Store(zone, loc)
	return loc
}

func Today(now time.Time, zone string) string {
	return now.In(Location(zone)).Format(time.DateOnly)
}

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
	if !Contains(Categories, i.Category) {
		f["category"] = "Pilih kategori yang tersedia."
	}
	if !Contains(Methods, i.Method) {
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

type Progress struct {
	ActiveLogCount int64 `json:"active_log_count"`
	XP             int64 `json:"xp"`
	Level          int64 `json:"level"`
	XPIntoLevel    int64 `json:"xp_into_level"`
	XPToNextLevel  int64 `json:"xp_to_next_level"`
	CurrentStreak  int   `json:"current_streak"`
	BestStreak     int   `json:"best_streak"`
	HasLoggedToday bool  `json:"has_logged_today"`
}

func MakeProgress(count int64, dates []string, today string) Progress {
	p := Progress{ActiveLogCount: count, XP: count * 10, Level: count/10 + 1, XPIntoLevel: count * 10 % 100, XPToNextLevel: 100 - count*10%100}
	t, _ := time.Parse(time.DateOnly, today)
	prev := ""
	run := 0
	for _, date := range dates {
		d, _ := time.Parse(time.DateOnly, date)
		if prev == d.AddDate(0, 0, -1).Format(time.DateOnly) {
			run++
		} else {
			run = 1
		}
		if run > p.BestStreak {
			p.BestStreak = run
		}
		prev = date
		if date == today {
			p.HasLoggedToday = true
			p.CurrentStreak = run
		} else if date == t.AddDate(0, 0, -1).Format(time.DateOnly) {
			p.CurrentStreak = run
		}
	}
	return p
}

type Summary struct {
	TotalHandledWeightG   int64    `json:"total_handled_weight_g"`
	TotalHandledCount     int64    `json:"total_handled_count"`
	AvoidedWeightG        int64    `json:"avoided_weight_g"`
	AvoidedCount          int64    `json:"avoided_count"`
	ActivityCount         int64    `json:"activity_count"`
	HandledActivityCount  int64    `json:"handled_activity_count"`
	RecycledActivityCount int64    `json:"recycled_activity_count"`
	RecyclingRatePercent  *float64 `json:"recycling_rate_percent"`
	RecyclingRateBasis    string   `json:"recycling_rate_basis"`
}

func (s *Summary) Finish() {
	s.RecyclingRateBasis = "HANDLED_ACTIVITY_COUNT"
	if s.HandledActivityCount > 0 {
		v := float64(s.RecycledActivityCount) * 100 / float64(s.HandledActivityCount)
		s.RecyclingRatePercent = &v
	}
}
