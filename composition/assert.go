package composition

import (
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
)

// AssertValid fails t when the composition has an error-severity finding,
// printing every one; warnings are logged and pass. It is the one call a
// product's CI makes over each shipped configuration (precedent for a
// testing.TB-taking framework helper: natsclient.NewTestClient).
func AssertValid(t testing.TB, catalog *component.Registry, cfg *config.Config) {
	t.Helper()
	result, err := Validate(catalog, cfg)
	if err != nil {
		t.Fatalf("composition validation: %v", err)
		return
	}
	for _, warning := range result.Warnings {
		t.Logf("composition warning %s on %s/%s: %s", warning.Type, warning.Component, warning.Port, warning.Message)
	}
	if len(result.Errors) == 0 {
		return
	}
	for _, finding := range result.Errors {
		t.Errorf("composition error %s on %s/%s: %s", finding.Type, finding.Component, finding.Port, finding.Message)
	}
	t.Fatalf("composition has %d error finding(s)", len(result.Errors))
}
