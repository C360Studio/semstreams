package rule

import "testing"

func mustTestConfig(t testing.TB, packID string) Config {
	t.Helper()
	config, err := NewConfig(packID)
	if err != nil {
		t.Fatalf("NewConfig(%q): %v", packID, err)
	}
	return config
}
