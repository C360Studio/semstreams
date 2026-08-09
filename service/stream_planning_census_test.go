package service_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/internal/componentadmission"
	"github.com/stretchr/testify/require"
)

type streamPlanningCoverageSummary struct {
	DefaultOnly int
	Covered     int
	Uncovered   int
}

type defaultOnlyJetStreamOutput struct {
	Config    string
	Component string
	Factory   string
	Port      string
	Subject   string
	CoveredBy []string
}

func TestShippedDefaultOnlyJetStreamOutputsHaveExplicitPreconstructionCoverage(t *testing.T) {
	artifact := loadSubjectCensusArtifact(t)
	rows := computeDefaultOnlyJetStreamOutputs(t, artifact.Scope)

	summary, err := validateDefaultOnlyJetStreamCoverage(rows)
	require.NoError(t, err)
	require.Equal(t, streamPlanningCoverageSummary{
		DefaultOnly: 61,
		Covered:     61,
		Uncovered:   0,
	}, summary)

	byFactory := make(map[string]int)
	bySubject := make(map[string]int)
	for _, row := range rows {
		byFactory[row.Factory]++
		bySubject[row.Subject]++
		require.Equal(t, []string{"AGENT/agent.>"}, row.CoveredBy,
			"%s %s/%s must retain the accepted explicit coverage", row.Config, row.Component, row.Port)
	}
	require.Equal(t, map[string]int{"agentic-dispatch": 16, "agentic-loop": 45}, byFactory)
	require.Equal(t, map[string]int{
		"agent.approval_pending.*":   9,
		"agent.approval_response.*":  8,
		"agent.context.compaction.*": 9,
		"agent.created.*":            9,
		"agent.failed.*":             9,
		"agent.signal.*":             8,
		"agent.toolcall.proposed.*":  9,
	}, bySubject)
}

func TestDefaultOnlyJetStreamCoverageRejectsSyntheticFutureUncoveredOutput(t *testing.T) {
	rows := []defaultOnlyJetStreamOutput{{
		Config:    "configs/future.json",
		Component: "future",
		Factory:   "future-factory",
		Port:      "future_events",
		Subject:   "future.events",
	}}

	summary, err := validateDefaultOnlyJetStreamCoverage(rows)
	require.Equal(t, streamPlanningCoverageSummary{DefaultOnly: 1, Uncovered: 1}, summary)
	require.ErrorContains(t, err, "future.events")
	require.ErrorContains(t, err, "explicit preconstruction stream declaration")
}

func TestStreamSubjectCoverageRequiresWholeDeclaredFamily(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		filter  string
		want    bool
	}{
		{name: "exact under tail", subject: "agent.created.*", filter: "agent.>", want: true},
		{name: "same single token family", subject: "agent.created.*", filter: "agent.created.*", want: true},
		{name: "tail is not covered by one token", subject: "future.>", filter: "future.*", want: false},
		{name: "single token is not covered by one literal", subject: "future.*", filter: "future.event", want: false},
		{name: "short exact is not covered by longer tail", subject: "future.event", filter: "future.event.>", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, subjectPatternCoveredByFilter(tt.subject, tt.filter))
		})
	}
}

func loadSubjectCensusArtifact(t *testing.T) subjectCensusArtifact {
	t.Helper()
	data, err := os.ReadFile("testdata/message_logger_subject_census.json")
	require.NoError(t, err)
	var artifact subjectCensusArtifact
	require.NoError(t, json.Unmarshal(data, &artifact))
	return artifact
}

func computeDefaultOnlyJetStreamOutputs(t *testing.T, scope []string) []defaultOnlyJetStreamOutput {
	t.Helper()
	var outputs []defaultOnlyJetStreamOutput
	var constructionFailures []string
	deps := messageLoggerCensusDependencies()

	for _, path := range scope {
		data, err := os.ReadFile(filepath.Join("..", path))
		require.NoError(t, err, path)
		cfg, err := config.NewLoader().LoadFromBytes(data)
		require.NoError(t, err, path)

		// Keep the two lifecycle facts separate. rawRows is configured
		// preconstruction intent resolved through PortDefinition/PortFacts;
		// effectiveRows is the admitted generation captured by Registry after a
		// production factory constructs it. Neither owner imports the other's
		// policy response.
		registry := newMessageLoggerCensusRegistry(t)
		rawRows := make(map[censusRow]int)
		effectiveRows := make(map[censusRow]int)
		discardedKeys := make(map[string]struct{})
		discardedGlobal := make(map[string]struct{})

		for _, instanceName := range slices.Sorted(maps.Keys(cfg.Components)) {
			componentConfig := cfg.Components[instanceName]
			if !componentConfig.Enabled {
				continue
			}
			collectRawCensusRows(t, instanceName, componentConfig.Name, componentConfig.Config,
				rawRows, discardedKeys, discardedGlobal)
			if _, createErr := registry.CreateComponent(instanceName, componentConfig, deps); createErr != nil {
				constructionFailures = append(constructionFailures,
					fmt.Sprintf("%s component %s factory %s: %v",
						path, instanceName, componentConfig.Name, createErr))
			}
		}

		for _, snapshot := range registry.Snapshots(componentadmission.Access{}) {
			collectEffectiveCensusRows(snapshot.Name(), snapshot.Factory(), component.DirectionOutput,
				snapshot.Outputs(), snapshot.OutputDeclarationFacts(), effectiveRows,
				discardedKeys, discardedGlobal)
		}

		for row, effectiveCount := range effectiveRows {
			if row.Direction != component.DirectionOutput || row.Kind != component.PortKindJetStream {
				continue
			}
			for range effectiveCount - rawRows[row] {
				outputs = append(outputs, defaultOnlyJetStreamOutput{
					Config: path, Component: row.Component, Factory: row.Factory,
					Port: row.Port, Subject: row.Subject,
					CoveredBy: explicitStreamCoverage(cfg.Streams, row.Subject),
				})
			}
		}
	}

	require.Empty(t, constructionFailures,
		"stream-planning census requires every shipped enabled component to construct through production registration")
	slices.SortFunc(outputs, func(left, right defaultOnlyJetStreamOutput) int {
		return strings.Compare(
			strings.Join([]string{left.Config, left.Component, left.Factory, left.Port, left.Subject}, "\x00"),
			strings.Join([]string{right.Config, right.Component, right.Factory, right.Port, right.Subject}, "\x00"),
		)
	})
	return outputs
}

func validateDefaultOnlyJetStreamCoverage(rows []defaultOnlyJetStreamOutput) (streamPlanningCoverageSummary, error) {
	summary := streamPlanningCoverageSummary{DefaultOnly: len(rows)}
	var uncovered []string
	for _, row := range rows {
		if len(row.CoveredBy) > 0 {
			summary.Covered++
			continue
		}
		summary.Uncovered++
		uncovered = append(uncovered, fmt.Sprintf("%s component %s factory %s output %s subject %s",
			row.Config, row.Component, row.Factory, row.Port, row.Subject))
	}
	if len(uncovered) == 0 {
		return summary, nil
	}
	slices.Sort(uncovered)
	return summary, fmt.Errorf("default-only JetStream output lacks an explicit preconstruction stream declaration: %s",
		strings.Join(uncovered, "; "))
}

func explicitStreamCoverage(streams config.StreamConfigs, subject string) []string {
	// Coverage is intentionally read only from explicit config.streams policy.
	// A Registry fact supplies the subject under test, never a guessed stream.
	var coveredBy []string
	for _, streamName := range slices.Sorted(maps.Keys(streams)) {
		stream := streams[streamName]
		for _, filter := range stream.Subjects {
			if subjectPatternCoveredByFilter(subject, filter) {
				coveredBy = append(coveredBy, streamName+"/"+filter)
			}
		}
	}
	return coveredBy
}

func subjectPatternCoveredByFilter(subject, filter string) bool {
	subjectTokens := strings.Split(subject, ".")
	filterTokens := strings.Split(filter, ".")
	if !validSubjectPattern(subjectTokens) || !validSubjectPattern(filterTokens) {
		return false
	}

	filterHasTail := filterTokens[len(filterTokens)-1] == ">"
	if !filterHasTail {
		if subjectTokens[len(subjectTokens)-1] == ">" || len(subjectTokens) != len(filterTokens) {
			return false
		}
		for index := range filterTokens {
			if !subjectTokenCoveredByFilter(subjectTokens[index], filterTokens[index]) {
				return false
			}
		}
		return true
	}

	filterPrefixLen := len(filterTokens) - 1
	// NATS > consumes at least one token, so len is also the minimum
	// concrete-subject width for either a fixed pattern or a trailing >.
	if len(subjectTokens) < filterPrefixLen+1 {
		return false
	}
	for index := range filterPrefixLen {
		subjectToken := subjectTokens[index]
		if subjectToken == ">" {
			subjectToken = "*"
		}
		if !subjectTokenCoveredByFilter(subjectToken, filterTokens[index]) {
			return false
		}
	}
	return true
}

func validSubjectPattern(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	for index, token := range tokens {
		if token == "" || (strings.ContainsAny(token, "*>") && token != "*" && token != ">") ||
			(token == ">" && index != len(tokens)-1) {
			return false
		}
	}
	return true
}

func subjectTokenCoveredByFilter(subjectToken, filterToken string) bool {
	if filterToken == "*" {
		return true
	}
	return subjectToken != "*" && subjectToken == filterToken
}
