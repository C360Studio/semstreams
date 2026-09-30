//go:build unix

package testinfra_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type earlyCIJob struct {
	Needs           []string `yaml:"needs"`
	If              string   `yaml:"if"`
	ContinueOnError any      `yaml:"continue-on-error"`
	Steps []struct {
		Uses            string            `yaml:"uses"`
		Run             string            `yaml:"run"`
		With            map[string]string `yaml:"with"`
		If              any               `yaml:"if"`
		ContinueOnError any               `yaml:"continue-on-error"`
	} `yaml:"steps"`
}

func earlyCIJobs(t *testing.T) map[string]earlyCIJob {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(findRepoRoot(t), ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]earlyCIJob `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow.Jobs
}

func TestEarlyCIValidationAdmission(t *testing.T) {
	jobs := earlyCIJobs(t)
	validator, ok := jobs["openspec"]
	if !ok {
		t.Fatal("dedicated OpenSpec prerequisite is absent")
	}
	if len(validator.Needs) != 0 || validator.If != "" || validator.ContinueOnError != nil {
		t.Fatalf("OpenSpec validation must be an unconditional independent prerequisite: needs=%q if=%q continue-on-error=%v", validator.Needs, validator.If, validator.ContinueOnError)
	}
	if len(validator.Steps) != 4 ||
		validator.Steps[0].Uses != "actions/checkout@v5" ||
		validator.Steps[1].Uses != "actions/setup-node@v4" ||
		validator.Steps[1].With["node-version"] != "22" ||
		strings.TrimSpace(validator.Steps[2].Run) != "npm install -g @fission-ai/openspec@1.7.0" ||
		strings.TrimSpace(validator.Steps[3].Run) != "openspec validate --all --strict --no-interactive" {
		t.Fatalf("OpenSpec prerequisite no longer runs the existing pinned strict validator: %+v", validator.Steps)
	}
	for _, step := range validator.Steps {
		if step.If != nil || step.ContinueOnError != nil {
			t.Fatalf("OpenSpec prerequisite cannot conditionally skip or ignore failure: %+v", step)
		}
	}
	if !reflect.DeepEqual(jobs["test"].Needs, []string{"openspec"}) || jobs["test"].If != "" || jobs["test"].ContinueOnError != nil {
		t.Fatalf("CI Test must wait for successful validation: needs=%q if=%q continue-on-error=%v", jobs["test"].Needs, jobs["test"].If, jobs["test"].ContinueOnError)
	}
	for _, independent := range []string{"lint", "build", "schema-validation", "api-compat"} {
		if len(jobs[independent].Needs) != 0 {
			t.Errorf("independent %s became serialized: needs=%q", independent, jobs[independent].Needs)
		}
	}
	for _, step := range jobs["lint"].Steps {
		if strings.Contains(step.Run, "openspec validate") || strings.Contains(step.Run, "@fission-ai/openspec") {
			t.Error("late Lint OpenSpec validation was not consolidated")
		}
	}
}

func TestEarlyCIValidationStatus(t *testing.T) {
	status := earlyCIJobs(t)["status-check"]
	required := []string{"openspec", "lint", "test", "build", "schema-validation", "api-compat"}
	if !reflect.DeepEqual(status.Needs, required) || status.If != "always()" || status.ContinueOnError != nil || len(status.Steps) != 1 || status.Steps[0].If != nil || status.Steps[0].ContinueOnError != nil {
		t.Fatalf("aggregate must always inspect every required result: %+v", status)
	}
	script := status.Steps[0].Run
	for _, job := range required {
		if !strings.Contains(script, fmt.Sprintf("${{ needs.%s.result }}", job)) {
			t.Fatalf("aggregate does not inspect %s", job)
		}
	}
	for _, tc := range []struct {
		name    string
		changes map[string]string
		wantOK  bool
	}{
		{"healthy", nil, true},
		{"failed prerequisite", map[string]string{"openspec": "failure"}, false},
		{"cancelled prerequisite", map[string]string{"openspec": "cancelled"}, false},
		{"skipped Test", map[string]string{"test": "skipped"}, false},
		{"missing prerequisite", map[string]string{"openspec": ""}, false},
		{"unknown prerequisite", map[string]string{"openspec": "unknown"}, false},
		{"failed independent job", map[string]string{"build": "failure"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved := script
			for _, job := range required {
				result := "success"
				if changed, ok := tc.changes[job]; ok {
					result = changed
				}
				resolved = strings.ReplaceAll(resolved, fmt.Sprintf("${{ needs.%s.result }}", job), result)
			}
			if strings.Contains(resolved, "${{") {
				t.Fatalf("unresolved GitHub expression in aggregate: %s", resolved)
			}
			command := exec.CommandContext(t.Context(), "bash", "-e", "-c", resolved)
			output, err := command.CombinedOutput()
			if (err == nil) != tc.wantOK {
				t.Fatalf("aggregate exit=%v, want success=%t\n%s", err, tc.wantOK, output)
			}
		})
	}
}

func TestEarlyCheckPushValidationAdmission(t *testing.T) {
	taskBinary, err := exec.LookPath("task")
	if err != nil {
		t.Fatal("task is required for check:push admission: ", err)
	}
	for _, tc := range []struct {
		name  string
		exit  string
		admit bool
	}{
		{"invalid", "exit 64", false},
		{"valid", "exit 0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := cleanupAdmissionWorkspace(t, findRepoRoot(t), "clean")
			shim := "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$SEMSTREAMS_TEST_CLEANUP_ROOT/openspec-command\"\n" + tc.exit + "\n"
			writeCleanupAdmissionFile(t, filepath.Join(root, "bin", "openspec"), shim, 0o700)
			result := runCleanupAdmission(t, root, []string{taskBinary, "--silent", "check:push"})
			body, err := os.ReadFile(filepath.Join(root, "openspec-command"))
			if err != nil || string(body) != "validate --all --strict --no-interactive\n" {
				t.Fatalf("check:push did not first invoke strict non-interactive OpenSpec: command=%q read=%v; task=%v\n%s", body, err, result.CommandError, result.Output)
			}
			_, guardErr := os.Stat(filepath.Join(root, "guard-observation.json"))
			_, expensiveErr := os.Stat(filepath.Join(root, "expensive-command"))
			if tc.admit {
				if guardErr != nil || expensiveErr != nil {
					t.Fatalf("valid candidate did not reach existing guard and next stage: guard=%v next=%v; task=%v\n%s", guardErr, expensiveErr, result.CommandError, result.Output)
				}
			} else if result.CommandError == nil || !os.IsNotExist(guardErr) || !os.IsNotExist(expensiveErr) {
				t.Fatalf("invalid candidate reached expensive work: guard=%v next=%v; task=%v\n%s", guardErr, expensiveErr, result.CommandError, result.Output)
			}
		})
	}
}
