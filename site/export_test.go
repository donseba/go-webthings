package site

import "time"

// SetClock replaces the clock of the service's settings cache, so tests can
// let cached settings expire without sleeping.
func SetClock(s *Service, now func() time.Time) {
	s.cache.SetClock(now)
}
