package githubwebhook

import (
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
)

// TestNewInput_RejectsUnknownOutputPortNames closes the silent-rename
// gap from the 2026-05-08 audit (project_audit_findings_2026_05_08.md
// Finding 1). Pre-fix, an operator config that renamed an output port
// (e.g. Name:"issues" instead of "github.event.issue") silently landed
// in the outputSubjects map under the rename, while publish call sites
// indexed by the canonical key still returned the default subject.
// Events kept publishing to the default — operator's intended
// subscribers saw nothing while the framework happily delivered to
// noone.
//
// Loud-not-silent fix: validate operator-supplied port names against
// the four canonical event types at construction; error on unknown.
//
// natsClient must be non-nil (early NewInput check) but no methods
// are invoked before port validation, so a zero-value client suffices.
func TestNewInput_RejectsUnknownOutputPortNames(t *testing.T) {
	tests := []struct {
		name          string
		ports         *component.PortConfig
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:    "nil Ports — accepted (uses canonical defaults)",
			ports:   nil,
			wantErr: false,
		},
		{
			name: "empty Outputs — accepted (no overrides to validate)",
			ports: &component.PortConfig{
				Outputs: []component.PortDefinition{},
			},
			wantErr: false,
		},
		{
			name: "all four canonical names with subject overrides — accepted",
			ports: &component.PortConfig{
				Outputs: []component.PortDefinition{
					{Name: "github.event.issue", Subject: "custom.issues"},
					{Name: "github.event.pr", Subject: "custom.prs"},
					{Name: "github.event.review", Subject: "custom.reviews"},
					{Name: "github.event.comment", Subject: "custom.comments"},
				},
			},
			wantErr: false,
		},
		{
			name: "subset of canonical names — accepted (partial overrides preserve defaults)",
			ports: &component.PortConfig{
				Outputs: []component.PortDefinition{
					{Name: "github.event.issue", Subject: "custom.issues"},
				},
			},
			wantErr: false,
		},
		{
			name: "operator-renamed port — rejected loud (the audit-finding shape)",
			ports: &component.PortConfig{
				Outputs: []component.PortDefinition{
					{Name: "issues", Subject: "custom.issues"},
				},
			},
			wantErr:       true,
			wantErrSubstr: "unknown output port name",
		},
		{
			name: "typo in canonical name — rejected loud",
			ports: &component.PortConfig{
				Outputs: []component.PortDefinition{
					{Name: "github.event.issuue", Subject: "custom.issues"},
				},
			},
			wantErr:       true,
			wantErrSubstr: "unknown output port name",
		},
		{
			name: "mix of canonical and unknown — rejected on first unknown",
			ports: &component.PortConfig{
				Outputs: []component.PortDefinition{
					{Name: "github.event.issue", Subject: "custom.issues"},
					{Name: "github.event.bogus", Subject: "custom.bogus"},
				},
			},
			wantErr:       true,
			wantErrSubstr: "github.event.bogus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				HTTPPort: 8090,
				Path:     "/github/webhook",
				Ports:    tt.ports,
			}
			_, err := NewInput("github-webhook-test", &natsclient.Client{}, cfg, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewInput() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.wantErrSubstr != "" && !strings.Contains(err.Error(), tt.wantErrSubstr) {
				t.Errorf("NewInput() error = %v, expected to contain %q", err, tt.wantErrSubstr)
			}
		})
	}
}

// TestNewInput_PreservesOperatorSubjectOverrides asserts that when the
// operator supplies a canonical port name with an overridden Subject,
// the publish path actually picks up that override. This is the
// flip-side of the validation test: validation rejects unknown names
// so the keyed-by-Name lookup is sound; this test confirms the
// override flows through.
func TestNewInput_PreservesOperatorSubjectOverrides(t *testing.T) {
	cfg := Config{
		HTTPPort: 8090,
		Path:     "/github/webhook",
		Ports: &component.PortConfig{
			Outputs: []component.PortDefinition{
				{Name: "github.event.issue", Subject: "custom.issue.subject"},
				{Name: "github.event.pr", Subject: "custom.pr.subject"},
			},
		},
	}
	in, err := NewInput("github-webhook-test", &natsclient.Client{}, cfg, nil)
	if err != nil {
		t.Fatalf("NewInput() error: %v", err)
	}

	if got := in.outputSubjects["github.event.issue"]; got != "custom.issue.subject" {
		t.Errorf("outputSubjects[issue] = %q, want %q", got, "custom.issue.subject")
	}
	if got := in.outputSubjects["github.event.pr"]; got != "custom.pr.subject" {
		t.Errorf("outputSubjects[pr] = %q, want %q", got, "custom.pr.subject")
	}
	// Non-overridden ports keep the canonical default
	if got := in.outputSubjects["github.event.review"]; got != "github.event.review" {
		t.Errorf("outputSubjects[review] = %q, want canonical default", got)
	}
	if got := in.outputSubjects["github.event.comment"]; got != "github.event.comment" {
		t.Errorf("outputSubjects[comment] = %q, want canonical default", got)
	}
}
