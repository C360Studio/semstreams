package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

func TestSemanticDefaultBaseAndExplicitEndpoint(t *testing.T) {
	t.Setenv("SEMSTREAMS_BASE_URL", "")
	t.Setenv("E2E_VARIANT", "")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "semantic alias default", args: []string{"--scenario", "semantic"}, want: "http://localhost:38180"},
		{name: "tiered semantic default", args: []string{"--scenario", "tiered", "--variant", "semantic"}, want: "http://localhost:38180"},
		{name: "explicit remote", args: []string{"--scenario", "semantic", "--base-url", "http://remote.example:5678"}, want: "http://remote.example:5678"},
		{name: "explicit standard port", args: []string{"--scenario", "semantic", "--base-url", "http://localhost:38080"}, want: "http://localhost:38080"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldFlags, oldArgs := flag.CommandLine, os.Args
			flag.CommandLine = flag.NewFlagSet("e2e", flag.ContinueOnError)
			os.Args = append([]string{"e2e"}, tc.args...)
			t.Cleanup(func() { flag.CommandLine, os.Args = oldFlags, oldArgs })
			got := parseCommandLineFlags()
			assert.Equal(t, tc.want, got.baseURL)
		})
	}
}

type assertionReportingScenario struct {
	result      *scenarios.Result
	err         error
	setupErr    error
	teardownErr error
	teardownRan *bool
}

func TestLessonsScenarioIsDispatchedAndListed(t *testing.T) {
	got := createScenario(nil, &cliFlags{scenarioName: "lessons"})
	if got == nil || got.Name() != "lessons" {
		t.Fatalf("createScenario(lessons) = %v", got)
	}

	output := captureListOutput(t)
	assert.Contains(t, output, "e2e:lessons")
	assert.Contains(t, output, "lessons         - Direct product birth")
}

// spec: e2e-evidence / Selection determines proof scope
func TestTieredConstructorRoutesGraphQLThroughSelectedBase(t *testing.T) {
	for _, tc := range []struct {
		variant string
		baseURL string
	}{
		{variant: "structural", baseURL: "http://127.0.0.1:38080"},
		{variant: "statistical", baseURL: "http://remote.example:1234"},
		{variant: "semantic", baseURL: "http://127.0.0.1:38180"},
		{variant: "semantic", baseURL: "http://remote.example:5678/"},
	} {
		t.Run(tc.variant+"/"+tc.baseURL, func(t *testing.T) {
			flags := &cliFlags{scenarioName: "tiered", variant: tc.variant, baseURL: tc.baseURL}
			selected := createScenario(nil, flags)
			assert.NotNil(t, selected)
			// The consumer owns this private config. Inspect its actual constructor
			// input so a copied CLI flag alone cannot make this test pass.
			cfg := reflect.ValueOf(selected).Elem().FieldByName("config").Elem()
			got := cfg.FieldByName("GraphQLURL").String()
			want := strings.TrimRight(tc.baseURL, "/") + "/graph-gateway/graphql"
			assert.Equal(t, want, got)
		})
	}
}

func TestCoreConstructorsReceiveSelectedEvidenceIdentity(t *testing.T) {
	for _, name := range []string{"core-health", "core-dataflow"} {
		t.Run(name, func(t *testing.T) {
			flags := &cliFlags{scenarioName: name, evidenceRunID: "run-controlled",
				evidenceMemberID: name, baseURL: "http://localhost:38080"}
			selected := createScenario(nil, flags)
			assert.NotNil(t, selected)
			cfg := reflect.ValueOf(selected).Elem().FieldByName("config").Elem()
			assert.Equal(t, flags.evidenceRunID, cfg.FieldByName("EvidenceRunID").String())
			assert.Equal(t, flags.evidenceMemberID, cfg.FieldByName("EvidenceMemberID").String())
		})
	}
}

func (s assertionReportingScenario) Name() string                { return "assertion-reporting" }
func (s assertionReportingScenario) Description() string         { return "test" }
func (s assertionReportingScenario) Setup(context.Context) error { return s.setupErr }
func (s assertionReportingScenario) Execute(context.Context) (*scenarios.Result, error) {
	return s.result, s.err
}
func (s assertionReportingScenario) Teardown(context.Context) error {
	if s.teardownRan != nil {
		*s.teardownRan = true
	}
	return s.teardownErr
}

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
			scenario := assertionReportingScenario{result: tc.result, err: tc.err}
			_, exit := executeScenario(t.Context(), logger, scenario, &cliFlags{}, nil, false)
			assert.Equal(t, tc.wantExit, exit)
			assert.Contains(t, output.String(), tc.wantOutput)
		})
	}
}

// spec: e2e-evidence / Failure reaches the outer gate
func TestRunScenarioFailsClosedOnNilResultAndTeardownFailure(t *testing.T) {
	for _, tc := range []struct {
		name          string
		scenario      assertionReportingScenario
		wantTeardown  bool
		wantLogPhrase string
	}{
		{name: "nil result", scenario: assertionReportingScenario{}, wantTeardown: true, wantLogPhrase: "nil result"},
		{name: "teardown after passed behavior", scenario: assertionReportingScenario{
			result: &scenarios.Result{Success: true}, teardownErr: errors.New("cleanup failed"),
		}, wantTeardown: true, wantLogPhrase: "cleanup failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			teardownRan := false
			tc.scenario.teardownRan = &teardownRan
			logger := slog.New(slog.NewTextHandler(&output, nil))
			_, exit := executeScenario(t.Context(), logger, tc.scenario, &cliFlags{}, nil, false)
			assert.Equal(t, 1, exit)
			assert.Equal(t, tc.wantTeardown, teardownRan)
			assert.Contains(t, output.String(), tc.wantLogPhrase)
		})
	}
}

type requiredReportingScenario struct {
	assertionReportingScenario
	checks []scenarios.CheckRequirement
}

func (s requiredReportingScenario) CheckRequirements() []scenarios.CheckRequirement {
	return s.checks
}

// spec: e2e-evidence / Required observations determine success
func TestRunScenarioRejectsDeclaredButUnobservedRequiredCheck(t *testing.T) {
	checks := []scenarios.CheckRequirement{{ID: "controlled-output", Required: true}}
	result := &scenarios.Result{
		ScenarioName:      "assertion-reporting",
		RunID:             "run-controlled",
		MemberID:          "assertion-reporting",
		CheckRequirements: checks,
		Success:           true,
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	scenario := requiredReportingScenario{
		assertionReportingScenario: assertionReportingScenario{result: result},
		checks:                     checks,
	}
	_, exit := executeScenario(t.Context(), logger, scenario,
		&cliFlags{evidenceRunID: "run-controlled", evidenceMemberID: "assertion-reporting"}, checks, true)
	assert.Equal(t, 1, exit)
	assert.Contains(t, output.String(), "controlled-output")
}
