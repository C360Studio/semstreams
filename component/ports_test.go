package component

import "testing"

// TestResolveSubject_ExistingBehavior covers the pre-chunk-2 contract.
// These cases must continue to pass unchanged — ResolveSubject is now a
// one-line wrapper around ResolveSubjectForOrg(... org=""), so any
// regression surfaces immediately.
func TestResolveSubject_ExistingBehavior(t *testing.T) {
	wildcardStarPorts := []PortDefinition{
		{Name: "agent.request", Subject: "agent.request.*"},
	}
	wildcardGtPorts := []PortDefinition{
		{Name: "agent.request", Subject: "agent.request.>"},
	}
	exactPorts := []PortDefinition{
		{Name: "agent.request", Subject: "agent.request"},
	}

	tests := []struct {
		name     string
		ports    []PortDefinition
		portName string
		suffix   string
		want     string
	}{
		{
			name:     "wildcard star — strips * and appends suffix",
			ports:    wildcardStarPorts,
			portName: "agent.request",
			suffix:   "loop-abc",
			want:     "agent.request.loop-abc",
		},
		{
			name:     "wildcard gt — strips > and appends suffix",
			ports:    wildcardGtPorts,
			portName: "agent.request",
			suffix:   "loop-abc",
			want:     "agent.request.loop-abc",
		},
		{
			name:     "exact subject — appends dot-suffix",
			ports:    exactPorts,
			portName: "agent.request",
			suffix:   "loop-abc",
			want:     "agent.request.loop-abc",
		},
		{
			name:     "unmapped port — fallback portName.suffix",
			ports:    []PortDefinition{},
			portName: "tool.execute",
			suffix:   "web_search",
			want:     "tool.execute.web_search",
		},
		{
			name:     "nil ports — fallback portName.suffix",
			ports:    nil,
			portName: "agent.complete",
			suffix:   "loop-xyz",
			want:     "agent.complete.loop-xyz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveSubject(tt.ports, tt.portName, tt.suffix)
			if got != tt.want {
				t.Errorf("ResolveSubject(%q, %q) = %q, want %q", tt.portName, tt.suffix, got, tt.want)
			}
		})
	}
}

// TestResolveSubjectForOrg_EmptyOrgMatchesResolveSubject confirms the zero
// behavior-change guarantee: every ResolveSubject case run through
// ResolveSubjectForOrg with org="" must return an identical string.
func TestResolveSubjectForOrg_EmptyOrgMatchesResolveSubject(t *testing.T) {
	ports := []PortDefinition{
		{Name: "agent.request", Subject: "agent.request.*"},
		{Name: "agent.complete", Subject: "agent.complete.>"},
		{Name: "agent.created", Subject: "agent.created"},
	}

	tests := []struct {
		portName string
		suffix   string
	}{
		{"agent.request", "loop-abc"},
		{"agent.complete", "loop-abc"},
		{"agent.created", "loop-abc"},
		{"unmapped.port", "some-suffix"},
	}

	for _, tt := range tests {
		t.Run(tt.portName+"/"+tt.suffix, func(t *testing.T) {
			wantFromLegacy := ResolveSubject(ports, tt.portName, tt.suffix)
			got := ResolveSubjectForOrg(ports, tt.portName, tt.suffix, "")
			if got != wantFromLegacy {
				t.Errorf("ResolveSubjectForOrg(%q, %q, %q) = %q, want %q (same as ResolveSubject)",
					tt.portName, tt.suffix, "", got, wantFromLegacy)
			}
		})
	}
}

// TestResolveSubjectForOrg_OrgPrepend verifies that a non-empty org is
// prepended as the leftmost NATS token for all subject resolution paths.
func TestResolveSubjectForOrg_OrgPrepend(t *testing.T) {
	ports := []PortDefinition{
		{Name: "agent.request", Subject: "agent.request.*"},
		{Name: "agent.complete", Subject: "agent.complete.>"},
		{Name: "agent.created", Subject: "agent.created"},
	}

	tests := []struct {
		name     string
		portName string
		suffix   string
		org      string
		want     string
	}{
		{
			name:     "wildcard star port with org",
			portName: "agent.request",
			suffix:   "loop-abc",
			org:      "acme",
			want:     "acme.agent.request.loop-abc",
		},
		{
			name:     "wildcard gt port with org",
			portName: "agent.complete",
			suffix:   "loop-abc",
			org:      "acme",
			want:     "acme.agent.complete.loop-abc",
		},
		{
			name:     "exact subject port with org",
			portName: "agent.created",
			suffix:   "loop-abc",
			org:      "acme",
			want:     "acme.agent.created.loop-abc",
		},
		{
			name:     "unmapped port fallback with org",
			portName: "tool.execute",
			suffix:   "web_search",
			org:      "acme",
			want:     "acme.tool.execute.web_search",
		},
		{
			name:     "hyphenated org is preserved verbatim",
			portName: "agent.request",
			suffix:   "loop-123",
			org:      "my-org",
			want:     "my-org.agent.request.loop-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveSubjectForOrg(ports, tt.portName, tt.suffix, tt.org)
			if got != tt.want {
				t.Errorf("ResolveSubjectForOrg(%q, %q, %q) = %q, want %q",
					tt.portName, tt.suffix, tt.org, got, tt.want)
			}
		})
	}
}
