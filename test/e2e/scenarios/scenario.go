// Package scenarios defines E2E test scenarios for SemStreams
package scenarios

import (
	"context"
	"time"
)

// Scenario defines the interface that all E2E test scenarios must implement
type Scenario interface {
	// Name returns the scenario name for identification and reporting
	Name() string

	// Description provides a human-readable description of what the scenario tests
	Description() string

	// Setup prepares the scenario environment before execution
	// This may include creating test data, configuring components, etc.
	Setup(ctx context.Context) error

	// Execute runs the actual test scenario
	// Returns detailed results including pass/fail status and diagnostics
	Execute(ctx context.Context) (*Result, error)

	// Teardown cleans up after the scenario execution
	// This should restore the system to its original state
	Teardown(ctx context.Context) error
}

// Result contains the outcome of a scenario execution
type Result struct {
	// Scenario identification
	ScenarioName string        `json:"scenario_name"`
	StartTime    time.Time     `json:"start_time"`
	EndTime      time.Time     `json:"end_time"`
	Duration     time.Duration `json:"duration"`

	// Overall status
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	// EvidenceStatus is complete only after the declared required checks finalize.
	// Legacy results remain unattested even when their execution succeeds.
	EvidenceStatus    string             `json:"evidence_status"`
	RunID             string             `json:"run_id,omitempty"`
	MemberID          string             `json:"member_id,omitempty"`
	CheckRequirements []CheckRequirement `json:"check_requirements,omitempty"`
	CheckObservations []CheckObservation `json:"check_observations,omitempty"`

	// Detailed results (legacy flat format)
	Metrics  map[string]any `json:"metrics,omitempty"`
	Details  map[string]any `json:"details,omitempty"`
	Errors   []string       `json:"errors,omitempty"`
	Warnings []string       `json:"warnings,omitempty"`

	// AssertionsRun counts evaluated required named checks for finalized adopted
	// results. Unattested legacy results retain their historical scenario-local count.
	AssertionsRun int `json:"assertions_run,omitempty"`

	// Structured results (new typed format for tiered scenarios)
	// This is populated alongside Metrics/Details for backward compatibility
	Structured *TieredResults `json:"structured,omitempty"`

	finalizationError string
}
