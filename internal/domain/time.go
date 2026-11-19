package domain

import (
	"sync"
	"time"
)

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
