package api

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

func progress(ctx context.Context, q querier, uid, today string) (domain.Progress, error) {
	var count int64
	var dates []string
	err := q.QueryRow(ctx, `SELECT count(*),COALESCE(array_agg(DISTINCT activity_date::text ORDER BY activity_date::text),ARRAY[]::text[]) FROM waste_logs WHERE user_id=$1 AND deleted_at IS NULL`, uid).Scan(&count, &dates)
	return domain.MakeProgress(count, dates, today), err
}

type daily struct {
	Date           string `json:"date"`
	HandledWeightG int64  `json:"handled_weight_g"`
	HandledCount   int64  `json:"handled_count"`
	ActivityCount  int64  `json:"activity_count"`
}
type categoryTotal struct {
	Category       string `json:"category"`
	HandledWeightG int64  `json:"handled_weight_g"`
	HandledCount   int64  `json:"handled_count"`
}
type stats struct {
	From         string           `json:"from"`
	To           string           `json:"to"`
	Timezone     string           `json:"timezone"`
	Summary      domain.Summary   `json:"summary"`
	MethodCounts map[string]int64 `json:"method_counts"`
	Daily        []daily          `json:"daily"`
	Categories   []categoryTotal  `json:"categories"`
}

func aggregate(ctx context.Context, q querier, uid, from, to, zone string) (stats, error) {
	result := stats{From: from, To: to, Timezone: zone, MethodCounts: map[string]int64{}, Daily: []daily{}, Categories: []categoryTotal{}}
	byDay := map[string]int{}
	byCategory := map[string]int{}
	for t := mustDate(from); !t.After(mustDate(to)); t = t.AddDate(0, 0, 1) {
		date := t.Format(time.DateOnly)
		byDay[date] = len(result.Daily)
		result.Daily = append(result.Daily, daily{Date: date})
	}
	for _, c := range domain.Categories {
		byCategory[c] = len(result.Categories)
		result.Categories = append(result.Categories, categoryTotal{Category: c})
	}
	for _, m := range domain.Methods {
		result.MethodCounts[m] = 0
	}
	rows, err := q.Query(ctx, `SELECT activity_date::text,category,method,count(*),COALESCE(sum(weight_g),0),COALESCE(sum(item_count),0) FROM waste_logs WHERE user_id=$1 AND deleted_at IS NULL AND activity_date BETWEEN $2::date AND $3::date GROUP BY activity_date,category,method`, uid, from, to)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var date, category, method string
		var count, weight, items int64
		if err = rows.Scan(&date, &category, &method, &count, &weight, &items); err != nil {
			return result, err
		}
		result.Summary.ActivityCount += count
		result.MethodCounts[method] += count
		day := &result.Daily[byDay[date]]
		day.ActivityCount += count
		if method == "REDUCE" {
			result.Summary.AvoidedWeightG += weight
			result.Summary.AvoidedCount += items
		} else {
			result.Summary.HandledActivityCount += count
			result.Summary.TotalHandledWeightG += weight
			result.Summary.TotalHandledCount += items
			day.HandledWeightG += weight
			day.HandledCount += items
			c := &result.Categories[byCategory[category]]
			c.HandledWeightG += weight
			c.HandledCount += items
		}
		if method == "RECYCLE" {
			result.Summary.RecycledActivityCount += count
		}
	}
	result.Summary.Finish()
	return result, rows.Err()
}
func (s *Server) statistics(w http.ResponseWriter, r *http.Request) {
	u := who(r).User
	today := domain.Today(s.Now(), u.Timezone)
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if from == "" {
		from = mustDate(today).AddDate(0, 0, -6).Format(time.DateOnly)
	}
	if to == "" {
		to = today
	}
	if !validDates(from, to, today) {
		fail(w, r, 422, "VALIDATION_ERROR", map[string]string{"from": "Gunakan rentang tanggal hingga 366 hari tanpa tanggal mendatang."})
		return
	}
	result, err := aggregate(r.Context(), s.DB, u.ID, from, to, u.Timezone)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	data(w, r, 200, result)
}
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	u := who(r).User
	today := domain.Today(s.Now(), u.Timezone)
	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer tx.Rollback(ctx)
	result, err := aggregate(ctx, tx, u.ID, today, today, u.Timezone)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	p, err := progress(ctx, tx, u.ID, today)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	rows, err := tx.Query(ctx, `SELECT `+logColumns+` FROM waste_logs WHERE user_id=$1 AND deleted_at IS NULL ORDER BY activity_date DESC,id DESC LIMIT 3`, u.ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	logs := []domain.Log{}
	for rows.Next() {
		l, e := scanLog(rows)
		if e != nil {
			rows.Close()
			s.internal(w, r, e)
			return
		}
		logs = append(logs, l)
	}
	err = rows.Err()
	rows.Close()
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	data(w, r, 200, map[string]any{"date": today, "timezone": u.Timezone, "summary": result.Summary, "recent_logs": logs, "progress": p})
}
