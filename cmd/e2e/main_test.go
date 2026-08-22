package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

type assertionReportingScenario struct {
	result *scenarios.Result
	err    error
}

func TestLessonsScenarioIsDispatchedAndListed(t *testing.T) {
	got := createScenario(nil, nil, &cliFlags{scenarioName: "lessons"})
	if got == nil || got.Name() != "lessons" {
		t.Fatalf("createScenario(lessons) = %v", got)
	}

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = original })
	if !handleListCommand(true) {
		t.Fatal("handleListCommand returned false")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = original
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	assert.Contains(t, string(output), "e2e:lessons")
	assert.Contains(t, string(output), "lessons         - Direct product birth")
}

func (s assertionReportingScenario) Name() string                { return "assertion-reporting" }
func (s assertionReportingScenario) Description() string         { return "test" }
func (s assertionReportingScenario) Setup(context.Context) error { return nil }
func (s assertionReportingScenario) Execute(context.Context) (*scenarios.Result, error) {
	return s.result, s.err
}
func (s assertionReportingScenario) Teardown(context.Context) error { return nil }

func TestRunScenarioReportsAssertionsOnSuccessAndPartialFailure(t *testing.T) {
	for _, tc := range []struct {
		name       string
		result     *scenarios.Result
		err        error
		wantExit   int
		wantOutput string
	}{
		{name: "success", result: &scenarios.Result{Success: true, AssertionsRun: 11}, wantOutput: "assertions_run=11"},
		{name: "partial failure", result: &scenarios.Result{AssertionsRun: 4}, err: errors.New("failed"),
			wantExit: 1, wantOutput: "assertions_run=4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&output, nil))
			exit := runScenario(t.Context(), logger,
				assertionReportingScenario{result: tc.result, err: tc.err}, &cliFlags{})
			assert.Equal(t, tc.wantExit, exit)
			assert.Contains(t, output.String(), tc.wantOutput)
		})
	}
}
