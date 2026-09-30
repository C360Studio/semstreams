package graphquery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const graphQueryOwnerChildEnv = "GRAPH_QUERY_OWNER_CHILD"

func graphQueryOwnerChildRequest(mode string, args []string) error {
	selection := "-test.run=^TestGraphQueryOwnerChildFixture$"
	switch mode {
	case "setup-exit":
		selection = "-test.run=^TestGraphQueryStartRegistersStableLocalSearchResponder$"
	case "caller-exit", "cleanup-error", "fence-error":
	default:
		return fmt.Errorf("unknown graph-query owner child mode %q", mode)
	}
	if len(args) != 2 || args[0] != selection || args[1] != "-test.count=1" {
		return fmt.Errorf("graph-query owner child requires exact selected test and count, got %q", args)
	}
	return nil
}

// spec: test-cleanup-policy / Lexical ownership of lifecycle test fixtures
func TestGraphQueryOwnerSetupAssertionExit(t *testing.T) {
	childCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(childCtx, os.Args[0],
		"-test.run=^TestGraphQueryStartRegistersStableLocalSearchResponder$", "-test.count=1")
	cmd.Env = append(os.Environ(), graphQueryOwnerChildEnv+"=setup-exit")
	output, err := cmd.CombinedOutput()
	if childCtx.Err() != nil {
		t.Fatalf("child proof timed out: %v; output:\n%s", childCtx.Err(), output)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("child exit = %v, want expected test-failure code 1; output:\n%s", err, output)
	}
	for _, witness := range []string{"intentional setup assertion exit", "GRAPH_QUERY_CHILD_COMPONENT_TERMINAL_BEFORE_SUBSTRATE"} {
		if !strings.Contains(string(output), witness) {
			t.Fatalf("child missing witness %q; output:\n%s", witness, output)
		}
	}
	for _, forbidden := range []string{"panic:", "DATA RACE", "GRAPH_QUERY_CHILD_COMPONENT_NOT_TERMINAL", "TestAttack_"} {
		if strings.Contains(string(output), forbidden) {
			t.Fatalf("child emitted forbidden %q; output:\n%s", forbidden, output)
		}
	}
}
