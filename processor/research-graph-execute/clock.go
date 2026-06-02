package researchexecute

import "time"

// nowDefault is the production wallclock implementation. Tests
// override via the package-level nowFunc variable when they need
// deterministic temporal-window outputs.
func nowDefault() time.Time {
	return time.Now().UTC()
}

// nowRFC3339Offset returns the current wallclock (per nowFunc)
// shifted by offsetHours and rendered as RFC3339. Used by the
// decompose materializer's time-axis template.
func nowRFC3339Offset(offsetHours int) string {
	return nowFunc().Add(time.Duration(offsetHours) * time.Hour).Format(time.RFC3339)
}
