package agentictools_test

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// TestConfig_Validate_PortsNonEmpty closes the silent-broken-input gap
// from the 2026-05-08 audit (project_audit_findings_2026_05_08.md
// Finding 3). Pre-fix, an operator config that supplied a Ports block
// without any Inputs (or set inputs:[]) started the component running
// and healthy with zero JetStream consumers — silent dispatch death.
// Symmetric to the publishResult silent-drop bug closed in beta.57;
// that one was outputs, this catches inputs (and outputs as well, for
// symmetry with the same shape on the publish side).
//
// Validation is by-presence-of-any-port, not by-canonical-name —
// operators can rename ports freely (`Name: "input"` with the canonical
// Subject is a working config). What's caught is the structural
// "operator supplied Ports but forgot a direction entirely."
func TestConfig_Validate_PortsNonEmpty(t *testing.T) {
	tests := []struct {
		name          string
		ports         *component.PortConfig
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:    "nil Ports uses DefaultConfig — accepted",
			ports:   nil,
			wantErr: false,
		},
		{
			name: "empty Inputs slice — rejected (silent-dispatch-death case)",
			ports: &component.PortConfig{
				Inputs: []component.PortDefinition{},
				Outputs: []component.PortDefinition{
					{Name: "tool.result", Type: "jetstream", Subject: "tool.result.*"},
				},
			},
			wantErr:       true,
			wantErrSubstr: "Ports.Inputs is empty",
		},
		{
			name: "empty Outputs slice — rejected (silent-publish-drop case)",
			ports: &component.PortConfig{
				Inputs: []component.PortDefinition{
					{Name: "tool.execute", Type: "jetstream", Subject: "tool.execute.>"},
				},
				Outputs: []component.PortDefinition{},
			},
			wantErr:       true,
			wantErrSubstr: "Ports.Outputs is empty",
		},
		{
			name: "operator-renamed ports with canonical subjects — accepted",
			ports: &component.PortConfig{
				Inputs: []component.PortDefinition{
					{Name: "input", Type: "nats", Subject: "tool.execute.>"},
				},
				Outputs: []component.PortDefinition{
					{Name: "output", Type: "nats", Subject: "tool.result.*"},
				},
			},
			wantErr: false,
		},
		{
			name: "operator-renamed ports with operator subjects — accepted",
			ports: &component.PortConfig{
				Inputs: []component.PortDefinition{
					{Name: "myinput", Type: "jetstream", Subject: "custom.tool.execute.>"},
				},
				Outputs: []component.PortDefinition{
					{Name: "myoutput", Type: "jetstream", Subject: "custom.tool.result.*"},
				},
			},
			wantErr: false,
		},
		{
			name: "canonical defaults — accepted",
			ports: &component.PortConfig{
				Inputs: []component.PortDefinition{
					{Name: "tool.execute", Type: "jetstream", Subject: "tool.execute.>"},
				},
				Outputs: []component.PortDefinition{
					{Name: "tool.result", Type: "jetstream", Subject: "tool.result.*"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := agentictools.Config{
				Timeout: "60s",
				Ports:   tt.ports,
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.wantErrSubstr != "" && !strings.Contains(err.Error(), tt.wantErrSubstr) {
				t.Errorf("Validate() error = %v, expected to contain %q", err, tt.wantErrSubstr)
			}
		})
	}
}
