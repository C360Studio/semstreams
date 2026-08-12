package ops

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpsComposeUsesE2EBinaryForLessonCurationControl(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../.."))
	data, err := os.ReadFile(filepath.Join(root, "docker/compose/ops.yml"))
	if err != nil {
		t.Fatalf("read ops compose file: %v", err)
	}

	var compose struct {
		Services map[string]struct {
			Image string `yaml:"image"`
			Build struct {
				Target string `yaml:"target"`
			} `yaml:"build"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatalf("decode ops compose file: %v", err)
	}

	semstreams, ok := compose.Services["semstreams"]
	if !ok {
		t.Fatal("ops compose file does not define the semstreams service")
	}
	if semstreams.Build.Target != "e2e" {
		t.Errorf("ops semstreams build target = %q, want e2e so the lesson-curation control responder is present", semstreams.Build.Target)
	}
	if semstreams.Image != "c360studio/semstreams:e2e-test" {
		t.Errorf("ops semstreams image = %q, want per-target tag c360studio/semstreams:e2e-test", semstreams.Image)
	}

	e2eMain, err := os.ReadFile(filepath.Join(root, "cmd/e2e-semstreams/main.go"))
	if err != nil {
		t.Fatalf("read E2E composition root: %v", err)
	}
	if !strings.Contains(string(e2eMain), `persona.LoadFromDirectory(ctx, "configs/personas/fragments", personaMgr, logger)`) {
		t.Error("E2E composition root must load checked-in persona fragments before the ops loop starts")
	}
}
