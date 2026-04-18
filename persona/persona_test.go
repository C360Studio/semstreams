package persona

import "testing"

func TestPersona_Validate(t *testing.T) {
	tests := []struct {
		name    string
		p       Persona
		wantErr bool
	}{
		{
			name: "valid persona",
			p:    Persona{ID: "role-researcher", Content: "You are a research agent."},
		},
		{
			name:    "missing id",
			p:       Persona{Content: "some content"},
			wantErr: true,
		},
		{
			name:    "missing content",
			p:       Persona{ID: "role-researcher"},
			wantErr: true,
		},
		{
			name:    "empty everywhere",
			p:       Persona{},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
