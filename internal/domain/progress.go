package domain

import "time"

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
	xp := count * 10
	p := Progress{
		ActiveLogCount: count,
		XP:             xp,
		Level:          count/10 + 1,
		XPIntoLevel:    xp % 100,
		XPToNextLevel:  100 - xp%100,
	}
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
