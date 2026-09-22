package main

import (
	"os"
	"strings"
	"testing"
)

func TestProcessBarrierE2EBuildDoesNotReplaceProductionTarget(t *testing.T) {
	dockerfile, err := os.ReadFile("../../docker/Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	source := string(dockerfile)
	production := strings.Index(source, "FROM alpine:latest AS production")
	taggedBuilder := strings.Index(source, "FROM builder AS process-barrier-builder")
	taggedTarget := strings.Index(source, "FROM production AS e2e-process-barrier")
	if production < 0 || taggedBuilder <= production || taggedTarget <= taggedBuilder {
		t.Fatalf("tagged process-barrier target is not isolated after production")
	}
	section := source[taggedBuilder:taggedTarget]
	if !strings.Contains(section, "-tags=e2e_process_barrier") ||
		!strings.Contains(section, "./cmd/semstreams") ||
		strings.Contains(section, "./cmd/e2e-semstreams") {
		t.Fatalf("tagged builder section is not the cmd/semstreams barrier build:\n%s", section)
	}
}

func TestDefaultProcessBarrierFileDoesNotImportHarness(t *testing.T) {
	disabled, err := os.ReadFile("process_barrier_disabled.go")
	if err != nil {
		t.Fatalf("read default process barrier file: %v", err)
	}
	source := string(disabled)
	if !strings.Contains(source, "//go:build !e2e_process_barrier") {
		t.Fatal("default process barrier file lacks negative build constraint")
	}
	if strings.Contains(source, "test/e2e/harness/processbarrier") {
		t.Fatal("default cmd/semstreams build imports the E2E process barrier harness")
	}

	tagged, err := os.ReadFile("process_barrier_e2e.go")
	if err != nil {
		t.Fatalf("read tagged process barrier file: %v", err)
	}
	if !strings.Contains(string(tagged), "//go:build e2e_process_barrier") ||
		!strings.Contains(string(tagged), "test/e2e/harness/processbarrier") {
		t.Fatal("tagged process barrier file does not exclusively import the harness")
	}
}

func TestShippedAgenticConfigDoesNotAdmitProcessBarrier(t *testing.T) {
	shipped, err := os.ReadFile("../../configs/agentic.json")
	if err != nil {
		t.Fatalf("read shipped agentic config: %v", err)
	}
	if strings.Contains(string(shipped), "e2e_process_barrier") {
		t.Fatal("shipped agentic config admits the E2E-only process barrier")
	}
}
