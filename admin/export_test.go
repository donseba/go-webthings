package admin

import (
	"testing"
	"time"
)

// SetPanelTimeout shortens how long dashboard panels and the navigation's
// badges wait for their sections, for the duration of a test.
func SetPanelTimeout(t testing.TB, timeout time.Duration) {
	panel, badge := panelTimeout, badgeTimeout
	panelTimeout, badgeTimeout = timeout, timeout

	t.Cleanup(func() {
		panelTimeout, badgeTimeout = panel, badge
	})
}
