package domain

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
