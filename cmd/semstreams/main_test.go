package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/composition"
)

// TestMain lets this test binary stand in for the semstreams binary: when
// SEMSTREAMS_TEST_RUN_MAIN is set the process runs main() with the arguments
// carried in SEMSTREAMS_TEST_RUN_ARGS, so a test can drive the real flag path
// (--validate --config <path>) and observe the exit code and stdout.
func TestMain(m *testing.M) {
	if os.Getenv("SEMSTREAMS_TEST_RUN_MAIN") == "1" {
		os.Args = append([]string{os.Args[0]}, strings.Split(os.Getenv("SEMSTREAMS_TEST_RUN_ARGS"), "\x00")...)
		main()
		return
	}
	os.Exit(m.Run())
}

// TestValidateFlagReportsCompositionFindings — `semstreams --validate --config
// <path>` prints the composition findings and exits non-zero when the result
// has errors; it never attempts a NATS connection.
func TestValidateFlagReportsCompositionFindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "errors.json")
	if err := os.WriteFile(path, []byte(`{
  "version": "1.0.0",
  "platform": {"org": "flag", "id": "test", "environment": "test"},
  "nats": {"urls": ["nats://127.0.0.1:1"]},
  "components": {
    "ghost": {"type": "processor", "name": "no-such-factory", "enabled": true, "config": {}}
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(),
		"SEMSTREAMS_TEST_RUN_MAIN=1",
		"SEMSTREAMS_TEST_RUN_ARGS=--validate\x00--config\x00"+path,
	)
	output, err := cmd.Output()
	exitErr, isExit := err.(*exec.ExitError)
	if err == nil || !isExit {
		t.Fatalf("--validate exited 0 (err=%v) for a configuration with an error finding; output:\n%s", err, output)
	}
	if exitErr.ExitCode() == 0 {
		t.Fatalf("--validate exit code 0; output:\n%s", output)
	}
	start := strings.Index(string(output), "{")
	if start < 0 {
		t.Fatalf("--validate printed no JSON findings:\n%s", output)
	}
	var result composition.Result
	if err := json.Unmarshal(output[start:], &result); err != nil {
		t.Fatalf("--validate output is not a composition.Result: %v\n%s", err, output)
	}
	if result.Status != composition.StatusErrors || len(result.Errors) != 1 {
		t.Fatalf("--validate printed status %q with %d errors, want one error", result.Status, len(result.Errors))
	}
	if result.Errors[0].Type != composition.TypeUnknownComponent || result.Errors[0].Component != "ghost" {
		t.Fatalf("printed finding %+v, want unknown_component on ghost", result.Errors[0])
	}
}
