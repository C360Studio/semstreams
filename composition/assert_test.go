package composition_test

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/composition"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/types"
)

// recordingTB records failures instead of stopping the test.
type recordingTB struct {
	testing.TB
	failures []string
	logs     []string
}

func (r *recordingTB) Helper()                           {}
func (r *recordingTB) Errorf(format string, args ...any) { r.failures = append(r.failures, sprintf(format, args...)) }
func (r *recordingTB) Fatalf(format string, args ...any) { r.failures = append(r.failures, sprintf(format, args...)) }
func (r *recordingTB) Logf(format string, args ...any)   { r.logs = append(r.logs, sprintf(format, args...)) }
func (r *recordingTB) Fail()                             { r.failures = append(r.failures, "Fail") }
func (r *recordingTB) FailNow()                          { r.failures = append(r.failures, "FailNow") }

func sprintf(format string, args ...any) string {
	return strings.TrimSpace(strings.ReplaceAll(fmtSprintf(format, args...), "\n", " "))
}

func TestAssertValidFailsOnErrorFinding(t *testing.T) {
	registry := fakeRegistry(t,
		fakeSpec{name: "needy", typ: "processor", inputs: []component.PortDefinition{jetStreamIn("in", "NOBODY", "nobody.streams", true)}},
		fakeSpec{name: "lonely", typ: "processor", inputs: []component.PortDefinition{natsIn("in", "nobody.publishes", false, nil)}},
	)
	withError := compositionOf(config.ComponentConfigs{"n": instance("needy", types.ComponentTypeProcessor)})
	warningsOnly := compositionOf(config.ComponentConfigs{"l": instance("lonely", types.ComponentTypeProcessor)})

	failing := &recordingTB{TB: t}
	composition.AssertValid(failing, registry, withError)
	if len(failing.failures) == 0 {
		t.Fatal("AssertValid recorded no failure for a composition with an error finding")
	}
	joined := strings.Join(failing.failures, " | ")
	if !strings.Contains(joined, composition.TypeOrphanedPort) || !strings.Contains(joined, "n") {
		t.Fatalf("failure text %q does not name the finding type and component", joined)
	}

	passing := &recordingTB{TB: t}
	composition.AssertValid(passing, registry, warningsOnly)
	if len(passing.failures) != 0 {
		t.Fatalf("AssertValid recorded failures for a warnings-only composition: %v", passing.failures)
	}
}
