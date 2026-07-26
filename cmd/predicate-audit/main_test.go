package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/internal/predicateaudit"
)

func TestRunExitAndFormatContract(t *testing.T) {
	t.Parallel()
	clean := t.TempDir()
	writeMainFixture(t, clean, "clean.go", "package fixture\nvar _ = Triple{Predicate: \"robotics.state.armed\"}\n")
	dirty := t.TempDir()
	writeMainFixture(t, dirty, "dirty.go", "package fixture\nvar _ = Triple{Predicate: \"legacy.bad_name\"}\n")

	var stdout, stderr bytes.Buffer
	if code := run([]string{clean}, &stdout, &stderr); code != 0 {
		t.Fatalf("clean text exit = %d, stderr = %s", code, stderr.String())
	}
	if got := stdout.String(); !strings.HasPrefix(got, "predicate audit passed:") {
		t.Fatalf("default text output = %q", got)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--format=json", clean}, &stdout, &stderr); code != 0 {
		t.Fatalf("clean JSON exit = %d, stderr = %s", code, stderr.String())
	}
	var report predicateaudit.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("JSON output = %q: %v", stdout.String(), err)
	}
	if report.Version != predicateaudit.PredicateAuditReportVersion {
		t.Fatalf("report version = %d", report.Version)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{dirty}, &stdout, &stderr); code != 1 {
		t.Fatalf("dirty exit = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
	_, dirtyFindings, err := predicateaudit.Audit(dirty)
	if err != nil {
		t.Fatal(err)
	}
	expectedDirtyText := fmt.Sprintf(
		"%s:%d: %s: %q: %s\npredicate audit failed: 1 invalid or unclassified candidates (1 extracted)\n",
		dirtyFindings[0].File,
		dirtyFindings[0].Line,
		dirtyFindings[0].Surface,
		dirtyFindings[0].Predicate,
		dirtyFindings[0].Reason,
	)
	if stderr.String() != expectedDirtyText {
		t.Fatalf("dirty stderr = %q, want legacy byte shape %q", stderr.String(), expectedDirtyText)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--format=xml", clean}, &stdout, &stderr); code != 2 {
		t.Fatalf("invalid format exit = %d", code)
	}
	if code := run([]string{filepath.Join(clean, "missing")}, &stdout, &stderr); code != 2 {
		t.Fatalf("I/O error exit = %d", code)
	}
	malformedJSON := t.TempDir()
	writeMainFixture(t, malformedJSON, "rules.json", `{"predicate":`)
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{malformedJSON}, &stdout, &stderr); code != 2 {
		t.Fatalf("malformed JSON exit = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "parse JSON") {
		t.Fatalf("malformed JSON stderr = %q, want parse error", stderr.String())
	}
	if code := run([]string{"--format=json", clean}, failingWriter{}, &stderr); code != 2 {
		t.Fatalf("report write error exit = %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runWithEncoder(
		[]string{"--format=json", clean},
		&stdout,
		&stderr,
		func(predicateaudit.Report) ([]byte, error) { return nil, errors.New("encode failed") },
	); code != 2 {
		t.Fatalf("report encoding error exit = %d", code)
	}
	if !strings.Contains(stderr.String(), "encode report: encode failed") {
		t.Fatalf("report encoding stderr = %q", stderr.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func writeMainFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

var _ io.Writer = failingWriter{}

// Exact classification for the malformed production-audit fixture above.
// predicate-audit:invalid {"location":"line:22:column:87:embedded-structured:inner-offset:43","kind":"stored-predicate","value":"legacy.bad_name","reason":"arity"}
