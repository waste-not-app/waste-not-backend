package achievements

import "time"

type AchievementRes struct {
	ID              string     `json:"id"`
	Code            string     `json:"code"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	RequirementType string     `json:"requirement_type"`
	CurrentValue    int64      `json:"current_value"`
	TargetValue     int64      `json:"target_value"`
	IsUnlocked      bool       `json:"is_unlocked"`
	UnlockedAt      *time.Time `json:"unlocked_at"`
}
