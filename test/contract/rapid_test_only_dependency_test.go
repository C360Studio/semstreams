package contract_test

import (
	"strings"
	"testing"
)

// rapidModulePath is the property-testing library whose licence condition this
// test enforces.
const rapidModulePath = "pgregory.net/rapid"

// TestRapidStaysIsolatedToTestDependencies enforces the owner's conditional
// acknowledgment of rapid's MPL-2.0 licence (PR #1213, 2026-08-31): "i am okay
// with the MPL 2.0 license on rapid as long as it stays isolated to our test
// suite deps."
//
// That condition is STANDING, not one-time. MPL-2.0 is file-level copyleft — it
// reaches modified rapid source, not code that merely imports it — so the
// acknowledgment holds precisely while rapid links into no artifact we ship.
// It breaks the moment a non-test file imports rapid, because the dependency
// then enters a shipped binary.
//
// `go list -deps` reports the closure of NON-TEST imports only (test-only
// imports require -test), so a package appearing here is one that ships.
// Without this test the condition is verified only in a PR comment, and the
// first non-test import lands silently.
func TestRapidStaysIsolatedToTestDependencies(t *testing.T) {
	dependencies := listDependencies(t, "./cmd/...")

	for _, line := range strings.Split(dependencies, "\n") {
		if strings.TrimSpace(line) == rapidModulePath {
			t.Fatalf("%s is in the dependency closure of ./cmd/... — it must stay a test-only "+
				"dependency, because the owner's MPL-2.0 acknowledgment is conditional on rapid "+
				"linking into no shipped artifact", rapidModulePath)
		}
	}
}
