package analytics

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/waste-not-app/waste-not-backend/internal/domain"
	"github.com/waste-not-app/waste-not-backend/internal/shared/db"
)

type Repository interface {
	GetStatistics(ctx context.Context, userID, from, to, timezone string) (StatisticsRes, error)
	GetDashboard(ctx context.Context, userID, today, timezone string) (DashboardRes, error)
}

type repository struct {
	dbPool *pgxpool.Pool
	q      *db.Queries
}

func NewRepository(dbPool *pgxpool.Pool) Repository {
	return &repository{
		dbPool: dbPool,
		q:      db.New(dbPool),
	}
}

func mustDate(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

func aggregate(ctx context.Context, q *db.Queries, uid, from, to, zone string) (StatisticsRes, error) {
	result := StatisticsRes{
		From:         from,
		To:           to,
		Timezone:     zone,
		MethodCounts: map[string]int64{},
		Daily:        []DailyStatRes{},
		Categories:   []CategoryTotalRes{},
	}
	byDay := map[string]int{}
	byCategory := map[string]int{}

	for t := mustDate(from); !t.After(mustDate(to)); t = t.AddDate(0, 0, 1) {
		date := t.Format(time.DateOnly)
		byDay[date] = len(result.Daily)
		result.Daily = append(result.Daily, DailyStatRes{Date: date})
	}
	for _, c := range domain.Categories {
		byCategory[c] = len(result.Categories)
		result.Categories = append(result.Categories, CategoryTotalRes{Category: c})
	}
	for _, m := range domain.Methods {
		result.MethodCounts[m] = 0
	}

	rows, err := q.AggregateWasteLogsByDateRange(ctx, db.AggregateWasteLogsByDateRangeParams{
		UserID:   uid,
		FromDate: from,
		ToDate:   to,
	})
	if err != nil {
		return result, err
	}

	for _, row := range rows {
		result.Summary.ActivityCount += row.ActivityCount
		result.MethodCounts[row.Method] += row.ActivityCount
		day := &result.Daily[byDay[row.ActivityDate]]
		day.ActivityCount += row.ActivityCount
		if row.Method == "REDUCE" {
			result.Summary.AvoidedWeightG += row.TotalWeightG
			result.Summary.AvoidedCount += row.TotalItemCount
		} else {
			result.Summary.HandledActivityCount += row.ActivityCount
			result.Summary.TotalHandledWeightG += row.TotalWeightG
			result.Summary.TotalHandledCount += row.TotalItemCount
			day.HandledWeightG += row.TotalWeightG
			day.HandledCount += row.TotalItemCount
			cat := &result.Categories[byCategory[row.Category]]
			cat.HandledWeightG += row.TotalWeightG
			cat.HandledCount += row.TotalItemCount
		}
		if row.Method == "RECYCLE" {
			result.Summary.RecycledActivityCount += row.ActivityCount
		}
	}
	result.Summary.Finish()
	return result, nil
}

func (r *repository) GetStatistics(ctx context.Context, userID, from, to, timezone string) (StatisticsRes, error) {
	return aggregate(ctx, r.q, userID, from, to, timezone)
}

func (r *repository) GetDashboard(ctx context.Context, userID, today, timezone string) (DashboardRes, error) {
	tx, err := r.dbPool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DashboardRes{}, err
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)
	stat, err := aggregate(ctx, qtx, userID, today, today, timezone)
	if err != nil {
		return DashboardRes{}, err
	}
	pStats, err := qtx.GetUserProgressStats(ctx, userID)
	if err != nil {
		return DashboardRes{}, err
	}
	prog := domain.MakeProgress(pStats.Count, pStats.ActivityDates, today)

	recentRows, err := qtx.ListRecentActiveLogsWithPhoto(ctx, userID)
	if err != nil {
		return DashboardRes{}, err
	}
	logs := make([]domain.Log, 0, len(recentRows))
	for _, row := range recentRows {
		logs = append(logs, domain.Log{
			ID:               row.ID,
			Category:         row.Category,
			Method:           row.Method,
			QuantityKind:     row.QuantityKind,
			WeightG:          row.WeightG,
			ItemCount:        row.ItemCount,
			ActivityDate:     row.ActivityDate,
			ActivityTimezone: row.ActivityTimezone,
			Notes:            row.Notes,
			PhotoID:          row.PhotoID,
			Version:          row.Version,
			CreatedAt:        row.CreatedAt,
			UpdatedAt:        row.UpdatedAt,
		})
	}

	if err = tx.Commit(ctx); err != nil {
		return DashboardRes{}, err
	}

	return DashboardRes{
		Date:       today,
		Timezone:   timezone,
		Summary:    stat.Summary,
		RecentLogs: logs,
		Progress:   prog,
	}, nil
}
