package auth

import "time"

// SetClock replaces the service's clock, so tests can let links and sessions
// expire.
func SetClock(s *Service, now func() time.Time) {
	s.now = now
}
