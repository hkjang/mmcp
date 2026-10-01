package server

import (
	"sync/atomic"
	"time"
	_ "time/tzdata" // the distroless image has no zoneinfo; embed it
)

var displayZone atomic.Pointer[time.Location]

// setDisplayZone sets the zone used to interpret user dates (since=2026-10-01)
// and to render timestamps, independent of the container's TZ (usually UTC).
func setDisplayZone(name string) {
	loc, err := time.LoadLocation(name)
	if err != nil || name == "" {
		loc, _ = time.LoadLocation("Asia/Seoul")
	}
	displayZone.Store(loc)
}

func zone() *time.Location {
	if l := displayZone.Load(); l != nil {
		return l
	}
	return time.Local
}
