package flowtemplate

import (
	"strings"
	"testing"
)

func TestTemplate_Validate(t *testing.T) {
	tests := []struct {
		name    string
		tpl     Template
		wantErr bool
	}{
		{
			name: "valid minimal",
			tpl: Template{
				ID:   "t1",
				Name: "Test",
				Body: `{"id": "{{.FlowID}}", "name": "demo", "nodes": [], "connections": []}`,
			},
		},
		{
			name:    "missing id",
			tpl:     Template{Name: "x", Body: "{}"},
			wantErr: true,
		},
		{
			name:    "missing name",
			tpl:     Template{ID: "t1", Body: "{}"},
			wantErr: true,
		},
		{
			name:    "missing body",
			tpl:     Template{ID: "t1", Name: "x"},
			wantErr: true,
		},
		{
			name:    "malformed template body",
			tpl:     Template{ID: "t1", Name: "x", Body: "{{.Unclosed"},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.tpl.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestTemplate_Instantiate_UsesDefaults(t *testing.T) {
	tpl := Template{
		ID:   "t1",
		Name: "Test",
		Body: `{"id": "{{.FlowID}}", "name": "{{.FlowName}}", "nodes": [], "connections": []}`,
		Parameters: []Parameter{
			{Name: "FlowID", Default: "default-flow"},
			{Name: "FlowName", Default: "Default Name"},
		},
	}
	flow, err := tpl.Instantiate(map[string]string{})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if flow.ID != "default-flow" || flow.Name != "Default Name" {
		t.Errorf("defaults not applied: %+v", flow)
	}
}

func TestTemplate_Instantiate_UserOverridesDefaults(t *testing.T) {
	tpl := Template{
		ID:         "t1",
		Name:       "Test",
		Body:       `{"id": "{{.FlowID}}", "name": "demo", "nodes": [], "connections": []}`,
		Parameters: []Parameter{{Name: "FlowID", Default: "default-id"}},
	}
	flow, err := tpl.Instantiate(map[string]string{"FlowID": "custom-id"})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if flow.ID != "custom-id" {
		t.Errorf("expected override, got %q", flow.ID)
	}
}

func TestTemplate_Instantiate_RequiredParamMissingErrors(t *testing.T) {
	tpl := Template{
		ID:         "t1",
		Name:       "Test",
		Body:       `{"id": "{{.FlowID}}", "name": "demo", "nodes": [], "connections": []}`,
		Parameters: []Parameter{{Name: "FlowID", Required: true}},
	}
	_, err := tpl.Instantiate(map[string]string{})
	if err == nil {
		t.Fatalf("expected error for missing required param")
	}
	if !strings.Contains(err.Error(), "FlowID") {
		t.Errorf("expected 'FlowID' in error, got: %v", err)
	}
}

func TestTemplate_Instantiate_RenderedMustBeValidFlowJSON(t *testing.T) {
	tpl := Template{
		ID:   "t1",
		Name: "Test",
		Body: `{this is not valid json`,
	}
	_, err := tpl.Instantiate(map[string]string{})
	if err == nil {
		t.Fatalf("expected JSON unmarshal error")
	}
	if !strings.Contains(err.Error(), "valid flow") {
		t.Errorf("expected 'valid flow' in error, got: %v", err)
	}
}

func TestTemplate_Instantiate_ExtraParamsIgnored(t *testing.T) {
	tpl := Template{
		ID:         "t1",
		Name:       "Test",
		Body:       `{"id": "{{.FlowID}}", "name": "demo", "nodes": [], "connections": []}`,
		Parameters: []Parameter{{Name: "FlowID", Default: "x"}},
	}
	// Pass an undeclared param — should not error.
	flow, err := tpl.Instantiate(map[string]string{
		"FlowID":     "real-id",
		"Unexpected": "ignored",
	})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if flow.ID != "real-id" {
		t.Errorf("got %q, want real-id", flow.ID)
	}
}
