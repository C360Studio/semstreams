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
			name:  "valid 6-part entity ID (org.platform.system.domain.type.instance)",
			input: "c360.platform1.gcs1.robotics.drone.1",
			want: EntityID{
				Org:      "c360",
				Platform: "platform1",
				System:   "gcs1",
				Domain:   "robotics",
				Type:     "drone",
				Instance: "1",
			},
			wantErr: false,
		},
		{
			name:  "valid with different values",
			input: "noaa.pacific.buoy7.oceanographic.sensor.temperature",
			want: EntityID{
				Org:      "noaa",
				Platform: "pacific",
				System:   "buoy7",
				Domain:   "oceanographic",
				Type:     "sensor",
				Instance: "temperature",
			},
			wantErr: false,
		},
		{
			name:    "invalid: single part",
			input:   "invalid",
			want:    EntityID{},
			wantErr: true,
		},
		{
			name:    "invalid: two parts",
			input:   "a.b",
			want:    EntityID{},
			wantErr: true,
		},
		{
			name:    "invalid: five parts (missing instance)",
			input:   "a.b.c.d.e",
			want:    EntityID{},
			wantErr: true,
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

func TestEntityIDPrefixMethods(t *testing.T) {
	eid := EntityID{
		Org:      "c360",
		Platform: "logistics",
		Domain:   "environmental",
		System:   "sensor",
		Type:     "temperature",
		Instance: "cold-storage-01",
	}

	tests := []struct {
		name   string
		method func() string
		want   string
	}{
		// Canonical order org.platform.system.domain.type.instance: the named
		// levels are deployment (2), source (3), taxonomy (4), type (5).
		{
			name:   "TypePrefix",
			method: eid.TypePrefix,
			want:   "c360.logistics.sensor.environmental.temperature",
		},
		{
			name:   "TaxonomyPrefix",
			method: eid.TaxonomyPrefix,
			want:   "c360.logistics.sensor.environmental",
		},
		{
			name:   "SourcePrefix",
			method: eid.SourcePrefix,
			want:   "c360.logistics.sensor",
		},
		{
			name:   "DeploymentPrefix",
			method: eid.DeploymentPrefix,
			want:   "c360.logistics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.method()
			if got != tt.want {
				t.Errorf("%s() = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestEntityIDHasPrefix(t *testing.T) {
	eid := EntityID{
		Org:      "c360",
		Platform: "logistics",
		Domain:   "environmental",
		System:   "sensor",
		Type:     "temperature",
		Instance: "cold-storage-01",
	}

	tests := []struct {
		name   string
		prefix string
		want   bool
	}{
		{
			name:   "exact type prefix match",
			prefix: "c360.logistics.sensor.environmental.temperature",
			want:   true,
		},
		{
			name:   "taxonomy prefix match",
			prefix: "c360.logistics.sensor.environmental",
			want:   true,
		},
		{
			name:   "source prefix match",
			prefix: "c360.logistics.sensor",
			want:   true,
		},
		{
			name:   "deployment prefix match",
			prefix: "c360.logistics",
			want:   true,
		},
		{
			name:   "org prefix match",
			prefix: "c360",
			want:   true,
		},
		{
			name:   "full key match",
			prefix: "c360.logistics.sensor.environmental.temperature.cold-storage-01",
			want:   true,
		},
		{
			name:   "different source - no match",
			prefix: "c360.logistics.zone",
			want:   false,
		},
		{
			name:   "different org - no match",
			prefix: "acme",
			want:   false,
		},
		{
			name:   "partial match in middle - no match",
			prefix: "logistics.sensor",
			want:   false,
		},
		{
			name:   "partial type name - no match",
			prefix: "c360.logistics.sensor.environmental.temp",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eid.HasPrefix(tt.prefix)
			if got != tt.want {
				t.Errorf("HasPrefix(%q) = %v, want %v", tt.prefix, got, tt.want)
			}
		})
	}
}

func TestEntityIDIsSibling(t *testing.T) {
	sensor1 := EntityID{
		Org:      "c360",
		Platform: "logistics",
		Domain:   "environmental",
		System:   "sensor",
		Type:     "temperature",
		Instance: "cold-storage-01",
	}

	sensor2 := EntityID{
		Org:      "c360",
		Platform: "logistics",
		Domain:   "environmental",
		System:   "sensor",
		Type:     "temperature",
		Instance: "cold-storage-02",
	}

	humid := EntityID{
		Org:      "c360",
		Platform: "logistics",
		Domain:   "environmental",
		System:   "sensor",
		Type:     "humidity",
		Instance: "zone-a",
	}

	otherOrg := EntityID{
		Org:      "acme",
		Platform: "logistics",
		Domain:   "environmental",
		System:   "sensor",
		Type:     "temperature",
		Instance: "warehouse-1",
	}

	tests := []struct {
		name string
		a    EntityID
		b    EntityID
		want bool
		desc string
	}{
		{
			name: "same type different instance - siblings",
			a:    sensor1,
			b:    sensor2,
			want: true,
			desc: "Two temperature sensors in same system are siblings",
		},
		{
			name: "different type same system - not siblings",
			a:    sensor1,
			b:    humid,
			want: false,
			desc: "Temperature and humidity sensors are not siblings",
		},
		{
			name: "same type different org - not siblings",
			a:    sensor1,
			b:    otherOrg,
			want: false,
			desc: "Same type but different org are not siblings",
		},
		{
			name: "same entity - not sibling of itself",
			a:    sensor1,
			b:    sensor1,
			want: false,
			desc: "An entity is not a sibling of itself",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.IsSibling(tt.b)
			if got != tt.want {
				t.Errorf("IsSibling() = %v, want %v (%s)", got, tt.want, tt.desc)
			}
		})
	}
}

func TestEntityIDIsSameSource(t *testing.T) {
	temp := EntityID{
		Org:      "c360",
		Platform: "logistics",
		Domain:   "environmental",
		System:   "sensor",
		Type:     "temperature",
		Instance: "cold-storage-01",
	}

	humid := EntityID{
		Org:      "c360",
		Platform: "logistics",
		Domain:   "environmental",
		System:   "sensor",
		Type:     "humidity",
		Instance: "zone-a",
	}

	differentSystem := EntityID{
		Org:      "c360",
		Platform: "logistics",
		Domain:   "environmental",
		System:   "monitor",
		Type:     "display",
		Instance: "main",
	}

	tests := []struct {
		name string
		a    EntityID
		b    EntityID
		want bool
	}{
		{
			name: "same source different types - true",
			a:    temp,
			b:    humid,
			want: true,
		},
		{
			name: "different sources - false",
			a:    temp,
			b:    differentSystem,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.a.IsSameSource(tt.b)
			if got != tt.want {
				t.Errorf("IsSameSource() = %v, want %v", got, tt.want)
			}
		})
	}
}
