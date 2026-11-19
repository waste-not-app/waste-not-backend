package achievements

import (
	"context"

	"github.com/waste-not-app/waste-not-backend/internal/database/db"
	"github.com/waste-not-app/waste-not-backend/internal/domain"
)

func SyncUser(ctx context.Context, qtx *db.Queries, userID, today string) ([]AchievementRes, error) {
	if err := qtx.AcquireLogIdempotencyLock(ctx, "ach:"+userID); err != nil {
		return nil, err
	}
	metrics, err := qtx.GetAchievementMetrics(ctx, userID)
	if err != nil {
		return nil, err
	}
	progress := domain.MakeProgress(metrics.ActiveLogCount, metrics.ActivityDates, today)
	rows, err := qtx.ListUserAchievements(ctx, userID)
	if err != nil {
		return nil, err
	}

	changed := false
	for _, row := range rows {
		raw := metricForRequirement(row.RequirementType, metrics.ActiveLogCount, metrics.RecycleLogCount, int64(progress.BestStreak))
		shouldUnlock := raw >= int64(row.RequirementValue)
		if shouldUnlock && row.UnlockedAt == nil {
			if err := qtx.UnlockUserAchievement(ctx, db.UnlockUserAchievementParams{
				UserID:        userID,
				AchievementID: row.ID,
			}); err != nil {
				return nil, err
			}
			changed = true
		} else if !shouldUnlock && row.UnlockedAt != nil {
			if err := qtx.RelockUserAchievement(ctx, db.RelockUserAchievementParams{
				UserID:        userID,
				AchievementID: row.ID,
			}); err != nil {
				return nil, err
			}
			changed = true
		}
	}

	if changed {
		rows, err = qtx.ListUserAchievements(ctx, userID)
		if err != nil {
			return nil, err
		}
	}

	out := make([]AchievementRes, 0, len(rows))
	for _, row := range rows {
		target := int64(row.RequirementValue)
		current := metricForRequirement(row.RequirementType, metrics.ActiveLogCount, metrics.RecycleLogCount, int64(progress.BestStreak))
		if current > target {
			current = target
		}
		out = append(out, AchievementRes{
			ID:              row.ID,
			Code:            row.Code,
			Title:           row.Title,
			Description:     row.Description,
			RequirementType: row.RequirementType,
			CurrentValue:    current,
			TargetValue:     target,
			IsUnlocked:      row.UnlockedAt != nil,
			UnlockedAt:      row.UnlockedAt,
		})
	}
	return out, nil
}

func metricForRequirement(reqType string, activeCount, recycleCount, bestStreak int64) int64 {
	switch reqType {
	case "ACTIVE_LOG_COUNT":
		return activeCount
	case "RECYCLE_LOG_COUNT":
		return recycleCount
	case "BEST_STREAK":
		return bestStreak
	default:
		return 0
	}
}
