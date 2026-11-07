package analytics

import "github.com/waste-not-app/waste-not-backend/internal/domain"

type DailyStatRes struct {
	Date           string `json:"date"`
	HandledWeightG int64  `json:"handled_weight_g"`
	HandledCount   int64  `json:"handled_count"`
	ActivityCount  int64  `json:"activity_count"`
}

type CategoryTotalRes struct {
	Category       string `json:"category"`
	HandledWeightG int64  `json:"handled_weight_g"`
	HandledCount   int64  `json:"handled_count"`
}

type StatisticsRes struct {
	From         string             `json:"from"`
	To           string             `json:"to"`
	Timezone     string             `json:"timezone"`
	Summary      domain.Summary     `json:"summary"`
	MethodCounts map[string]int64   `json:"method_counts"`
	Daily        []DailyStatRes     `json:"daily"`
	Categories   []CategoryTotalRes `json:"categories"`
}

type DashboardRes struct {
	Date       string          `json:"date"`
	Timezone   string          `json:"timezone"`
	Summary    domain.Summary  `json:"summary"`
	RecentLogs []domain.Log    `json:"recent_logs"`
	Progress   domain.Progress `json:"progress"`
}
