package message

import (
	"testing"
)

func TestParseEntityID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    EntityID
		wantErr bool
	}{
		{
			name:  "valid 6-part entity ID",
			input: "c360.platform1.robotics.gcs1.drone.1",
			want: EntityID{
				Org:      "c360",
				Platform: "platform1",
				Domain:   "robotics",
				System:   "gcs1",
				Type:     "drone",
				Instance: "1",
			},
			wantErr: false,
		},
		{
			name:  "valid with different values",
			input: "noaa.pacific.oceanographic.buoy7.sensor.temperature",
			want: EntityID{
				Org:      "noaa",
				Platform: "pacific",
				Domain:   "oceanographic",
				System:   "buoy7",
				Type:     "sensor",
				Instance: "temperature",
			},
			wantErr: false,
		},
		{
			name:    "too few parts",
			input:   "c360.platform1.robotics.drone.1",
			want:    EntityID{},
			wantErr: true,
		},
		{
			name:    "too many parts",
			input:   "c360.platform1.robotics.gcs1.drone.1.extra",
			want:    EntityID{},
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			want:    EntityID{},
			wantErr: true,
		},
		{
			name:    "empty part in middle",
			input:   "c360.platform1..gcs1.drone.1",
			want:    EntityID{},
			wantErr: true,
		},
		{
			name:    "empty part at start",
			input:   ".platform1.robotics.gcs1.drone.1",
			want:    EntityID{},
			wantErr: true,
		},
		{
			name:    "empty part at end",
			input:   "c360.platform1.robotics.gcs1.drone.",
			want:    EntityID{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEntityID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseEntityID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.Org != tt.want.Org {
					t.Errorf("ParseEntityID() Org = %v, want %v", got.Org, tt.want.Org)
				}
				if got.Platform != tt.want.Platform {
					t.Errorf("ParseEntityID() Platform = %v, want %v", got.Platform, tt.want.Platform)
				}
				if got.Domain != tt.want.Domain {
					t.Errorf("ParseEntityID() Domain = %v, want %v", got.Domain, tt.want.Domain)
				}
				if got.System != tt.want.System {
					t.Errorf("ParseEntityID() System = %v, want %v", got.System, tt.want.System)
				}
				if got.Type != tt.want.Type {
					t.Errorf("ParseEntityID() Type = %v, want %v", got.Type, tt.want.Type)
				}
				if got.Instance != tt.want.Instance {
					t.Errorf("ParseEntityID() Instance = %v, want %v", got.Instance, tt.want.Instance)
				}
			}
		})
	}
}

func TestParseEntityIDRoundTrip(t *testing.T) {
	// Test that parsing and then calling Key() gives back the original string
	original := "c360.platform1.robotics.gcs1.drone.42"

	parsed, err := ParseEntityID(original)
	if err != nil {
		t.Fatalf("ParseEntityID() failed: %v", err)
	}

	result := parsed.Key()
	if result != original {
		t.Errorf("Round trip failed: got %v, want %v", result, original)
	}
}

func TestParseEntityIDValidation(t *testing.T) {
	// Test that parsed EntityID passes IsValid()
	input := "c360.platform1.robotics.gcs1.drone.1"

	parsed, err := ParseEntityID(input)
	if err != nil {
		t.Fatalf("ParseEntityID() failed: %v", err)
	}

	if !parsed.IsValid() {
		t.Errorf("Parsed EntityID failed IsValid() check")
	}
}
