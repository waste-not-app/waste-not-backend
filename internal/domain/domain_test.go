package domain

import (
	"testing"
	"time"
)

func TestValidationBoundaries(t *testing.T) {
	one, max, bad := 1, 100000, 100001
	count, maxCount := 10000, 10001
	cases := []struct {
		name, kind    string
		weight, items *int
		date, old     string
		valid         bool
	}{
		{"weight minimum", "WEIGHT", &one, nil, "2026-11-04", "", true}, {"weight maximum", "WEIGHT", &max, nil, "2026-11-04", "", true}, {"weight overflow", "WEIGHT", &bad, nil, "2026-11-04", "", false},
		{"count maximum", "COUNT", nil, &count, "2026-11-04", "", true}, {"count overflow", "COUNT", nil, &maxCount, "2026-11-04", "", false}, {"xor", "WEIGHT", &one, &one, "2026-11-04", "", false}, {"null weight", "WEIGHT", nil, nil, "2026-11-04", "", false},
		{"old edit", "WEIGHT", &one, nil, "2026-01-01", "2026-01-01", true}, {"old create", "WEIGHT", &one, nil, "2026-01-01", "", false}, {"30 day edge", "WEIGHT", &one, nil, "2026-10-05", "", true}, {"31 days", "WEIGHT", &one, nil, "2026-10-04", "", false}, {"future", "WEIGHT", &one, nil, "2026-11-05", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := LogInput{Category: "PLASTIC", Method: "REDUCE", QuantityKind: c.kind, WeightG: c.weight, ItemCount: c.items, ActivityDate: c.date}
			if got := len(in.Validate("2026-11-04", c.old)) == 0; got != c.valid {
				t.Fatalf("valid=%v", got)
			}
		})
	}
}
func TestProgressAndTimezone(t *testing.T) {
	for _, c := range []struct {
		dates         []string
		today         string
		current, best int
		logged        bool
	}{{[]string{}, "2026-11-04", 0, 0, false}, {[]string{"2026-11-01", "2026-11-02", "2026-11-03"}, "2026-11-04", 3, 3, false}, {[]string{"2026-11-01", "2026-11-02"}, "2026-11-04", 0, 2, false}, {[]string{"2026-11-03", "2026-11-04"}, "2026-11-04", 2, 2, true}} {
		p := MakeProgress(5, c.dates, c.today)
		if p.XP != 50 || p.XPToNextLevel != 50 || p.CurrentStreak != c.current || p.BestStreak != c.best || p.HasLoggedToday != c.logged {
			t.Fatalf("progress %+v", p)
		}
	}
	for _, c := range []struct{ at, zone, want string }{{"2026-11-03T18:00:00Z", "Asia/Jakarta", "2026-11-04"}, {"2026-11-04T01:00:00Z", "America/Los_Angeles", "2026-11-03"}, {"2026-11-01T08:30:00Z", "America/Los_Angeles", "2026-11-01"}} {
		at, _ := time.Parse(time.RFC3339, c.at)
		if Today(at, c.zone) != c.want {
			t.Fatal(c)
		}
	}
}
