package main

import "testing"

// gh#1117 D2: a non-empty E2E_PATH_ONLY turns the semantic path-only run on,
// the E2E_VARIANT sibling pattern (no ParseBool); unset or empty leaves the
// full variant.
func TestEnvOverridesPathOnlyFromNonEmptyEnv(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "unset", env: map[string]string{}, want: false},
		{name: "empty", env: map[string]string{"E2E_PATH_ONLY": ""}, want: false},
		{name: "one", env: map[string]string{"E2E_PATH_ONLY": "1"}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := &cliFlags{}
			applyEnvOverrides(flags, func(k string) string { return tc.env[k] })
			if flags.pathOnly != tc.want {
				t.Errorf("pathOnly = %v, want %v for env %v", flags.pathOnly, tc.want, tc.env)
			}
		})
	}
}
