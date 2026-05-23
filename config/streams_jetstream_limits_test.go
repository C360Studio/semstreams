package config

import "testing"

// TestJetStreamLimitExceeds exercises the gap-detection predicate
// behind VerifyJetStreamLimits's per-field Warn branch (#101). Pure
// function over int64 pairs so the full truth table is unit-testable
// without booting a NATS server.
func TestJetStreamLimitExceeds(t *testing.T) {
	cases := []struct {
		name        string
		configured  int64
		serverLimit int64
		want        bool
	}{
		// "Not configured" branches — operator hasn't set the field;
		// nothing to verify against.
		{"zero configured", 0, 1024, false},
		{"negative configured", -1, 1024, false},
		// "Unlimited server" branch — -1 sentinel means no gap is
		// possible regardless of operator intent.
		{"unlimited server, configured nonzero", 1024 * 1024, -1, false},
		{"unlimited server, configured tiny", 1, -1, false},
		// "Within budget" branch — operator's hint matches or is
		// strictly less than the server's actual limit.
		{"configured at exact server limit", 1024, 1024, false},
		{"configured below server limit", 512, 1024, false},
		// "Gap" branch — the only true case. Triggers the Warn.
		{"configured exceeds server limit by 1", 1025, 1024, true},
		{"configured 1PiB vs 10GB server", 1024 * 1024 * 1024 * 1024 * 1024, 10 * 1024 * 1024 * 1024, true},
		// Edge: server reports 0 (zero finite limit, not -1
		// unlimited). Any positive configured value exceeds it.
		{"server zero (finite), configured positive", 1, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := jetStreamLimitExceeds(tc.configured, tc.serverLimit)
			if got != tc.want {
				t.Errorf("jetStreamLimitExceeds(%d, %d) = %v, want %v", tc.configured, tc.serverLimit, got, tc.want)
			}
		})
	}
}
