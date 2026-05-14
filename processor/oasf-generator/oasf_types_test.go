package oasfgenerator

import (
	"encoding/json"
	"testing"
)

func TestNewOASFRecord(t *testing.T) {
	record := NewOASFRecord("test-agent", "1.0.0", "A test agent")

	if record.Name != "test-agent" {
		t.Errorf("expected name 'test-agent', got %q", record.Name)
	}
	if record.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", record.Version)
	}
	if record.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("expected schema version %q, got %q", CurrentSchemaVersion, record.SchemaVersion)
	}
	if record.Description != "A test agent" {
		t.Errorf("expected description 'A test agent', got %q", record.Description)
	}
	if record.CreatedAt == "" {
		t.Error("expected created_at to be set")
	}
	if len(record.Skills) != 0 {
		t.Errorf("expected empty skills, got %d", len(record.Skills))
	}
}

func TestOASFRecord_AddSkill(t *testing.T) {
	record := NewOASFRecord("test-agent", "1.0.0", "A test agent")

	// Use the OASF code-evaluation extension ID for "code-review"; the
	// test exercises the AddSkill plumbing, not the resolver — wire
	// shape is what matters here.
	const codeReviewExtID uint32 = 9_100_001
	skill := OASFSkill{
		ID:          codeReviewExtID,
		Name:        "semstreams/code_review",
		Description: "Reviews code for quality",
		Confidence:  0.9,
		Permissions: []string{"file_read"},
	}
	record.AddSkill(skill)

	if len(record.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(record.Skills))
	}
	if record.Skills[0].ID != codeReviewExtID {
		t.Errorf("expected skill ID %d, got %d", codeReviewExtID, record.Skills[0].ID)
	}
}

func TestOASFRecord_AddDomain(t *testing.T) {
	record := NewOASFRecord("test-agent", "1.0.0", "A test agent")

	domain := OASFDomain{
		Name:        "software-engineering",
		Description: "Software engineering domain",
		Priority:    1,
	}
	record.AddDomain(domain)

	if len(record.Domains) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(record.Domains))
	}
	if record.Domains[0].Name != "software-engineering" {
		t.Errorf("expected domain name 'software-engineering', got %q", record.Domains[0].Name)
	}
}

func TestOASFRecord_SetExtension(t *testing.T) {
	record := NewOASFRecord("test-agent", "1.0.0", "A test agent")

	record.SetExtension("custom_field", "custom_value")
	record.SetExtension("numeric_field", 42)

	if record.Extensions == nil {
		t.Fatal("expected extensions to be initialized")
	}
	if record.Extensions["custom_field"] != "custom_value" {
		t.Errorf("expected custom_field to be 'custom_value', got %v", record.Extensions["custom_field"])
	}
	if record.Extensions["numeric_field"] != 42 {
		t.Errorf("expected numeric_field to be 42, got %v", record.Extensions["numeric_field"])
	}
}

func TestOASFRecord_Validate(t *testing.T) {
	tests := []struct {
		name    string
		record  *OASFRecord
		wantErr bool
	}{
		{
			name:    "valid record",
			record:  NewOASFRecord("test-agent", "1.0.0", "A test agent"),
			wantErr: false,
		},
		{
			name: "missing name",
			record: &OASFRecord{
				Version:       "1.0.0",
				SchemaVersion: CurrentSchemaVersion,
				CreatedAt:     "2024-01-15T10:00:00Z",
			},
			wantErr: true,
		},
		{
			name: "missing version",
			record: &OASFRecord{
				Name:          "test-agent",
				SchemaVersion: CurrentSchemaVersion,
				CreatedAt:     "2024-01-15T10:00:00Z",
			},
			wantErr: true,
		},
		{
			name: "missing schema_version",
			record: &OASFRecord{
				Name:      "test-agent",
				Version:   "1.0.0",
				CreatedAt: "2024-01-15T10:00:00Z",
			},
			wantErr: true,
		},
		{
			name: "missing created_at",
			record: &OASFRecord{
				Name:          "test-agent",
				Version:       "1.0.0",
				SchemaVersion: CurrentSchemaVersion,
			},
			wantErr: true,
		},
		{
			name: "invalid skill",
			record: func() *OASFRecord {
				r := NewOASFRecord("test-agent", "1.0.0", "A test agent")
				r.AddSkill(OASFSkill{Name: "test"}) // Missing ID
				return r
			}(),
			wantErr: true,
		},
		{
			name: "invalid domain",
			record: func() *OASFRecord {
				r := NewOASFRecord("test-agent", "1.0.0", "A test agent")
				r.AddDomain(OASFDomain{}) // Missing name
				return r
			}(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.record.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOASFSkill_Validate(t *testing.T) {
	tests := []struct {
		name    string
		skill   OASFSkill
		wantErr bool
	}{
		{
			name: "valid skill",
			skill: OASFSkill{
				ID:         9_100_001, // arbitrary extension-range ID; test exercises Validate, not the resolver
				Name:       "Code Review",
				Confidence: 0.9,
			},
			wantErr: false,
		},
		{
			name: "missing id",
			skill: OASFSkill{
				Name:       "Code Review",
				Confidence: 0.9,
			},
			wantErr: true,
		},
		{
			name: "missing name",
			skill: OASFSkill{
				ID:         9_100_001,
				Confidence: 0.9,
			},
			wantErr: true,
		},
		{
			name: "confidence too low",
			skill: OASFSkill{
				ID:         9_100_001, // arbitrary extension-range ID; test exercises Validate, not the resolver
				Name:       "Code Review",
				Confidence: -0.1,
			},
			wantErr: true,
		},
		{
			name: "confidence too high",
			skill: OASFSkill{
				ID:         9_100_001, // arbitrary extension-range ID; test exercises Validate, not the resolver
				Name:       "Code Review",
				Confidence: 1.1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.skill.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOASFRecord_MarshalJSON(t *testing.T) {
	record := NewOASFRecord("test-agent", "1.0.0", "A test agent")
	record.AddSkill(OASFSkill{
		ID:          9_100_001,
		Name:        "semstreams/code_review",
		Description: "Reviews code",
		Confidence:  0.9,
	})
	record.AddDomain(OASFDomain{
		Name: "software",
	})
	record.SetExtension("source", "semstreams")

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	// Unmarshal back to verify
	var decoded OASFRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.Name != record.Name {
		t.Errorf("expected name %q, got %q", record.Name, decoded.Name)
	}
	if len(decoded.Skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(decoded.Skills))
	}
	if len(decoded.Domains) != 1 {
		t.Errorf("expected 1 domain, got %d", len(decoded.Domains))
	}
}
