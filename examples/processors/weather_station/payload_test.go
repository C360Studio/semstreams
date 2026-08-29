package weatherstation

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/types"
)

// testAuthority is a deployment authority in the shape a composition root
// supplies it: platform.org / platform.id. "dep1" is a deployment name, not a
// product name — position 2 never carries a product (ADR-102 d2, d3).
var testAuthority = types.PlatformMeta{Org: "acme", Platform: "dep1"}

func TestWeatherReading_EntityID_6PartFormat(t *testing.T) {
	reading := WeatherReading{
		StationID:     "ws-001",
		EntityIDValue: WeatherReadingEntityID(testAuthority, "ws-001"),
	}

	entityID := reading.EntityID()
	parts := strings.Split(entityID, ".")

	if len(parts) != 6 {
		t.Errorf("EntityID() = %q has %d parts, want 6", entityID, len(parts))
	}

	if !message.IsValidEntityID(entityID) {
		t.Errorf("EntityID() = %q is not valid", entityID)
	}

	// Verify expected format
	want := "acme.dep1.station.meteorology.outdoor.ws-001"
	if entityID != want {
		t.Errorf("EntityID() = %q, want %q", entityID, want)
	}
}

func TestWeatherReading_Triples_SemanticPredicates(t *testing.T) {
	reading := WeatherReading{
		StationID:     "ws-001",
		Temperature:   22.5,
		Humidity:      65.0,
		Condition:     "sunny",
		City:          "San Francisco",
		ObservedAt:    time.Now(),
		EntityIDValue: WeatherReadingEntityID(testAuthority, "ws-001"),
	}

	triples := reading.Triples()

	// Should have at least 4 triples (temp, humidity, condition, timestamp)
	if len(triples) < 4 {
		t.Errorf("Triples() returned %d triples, want at least 4", len(triples))
	}

	// Verify all triples reference this entity
	entityID := reading.EntityID()
	for i, triple := range triples {
		if triple.Subject != entityID {
			t.Errorf("Triple[%d].Subject = %q, want %q", i, triple.Subject, entityID)
		}
	}

	// Verify predicates use dotted notation (no colons)
	for i, triple := range triples {
		if strings.Contains(triple.Predicate, ":") {
			t.Errorf("Triple[%d].Predicate = %q contains colon, want dotted notation",
				i, triple.Predicate)
		}
	}
}

func TestWeatherReading_Validate(t *testing.T) {
	tests := []struct {
		name    string
		reading WeatherReading
		wantErr bool
	}{
		{
			name: "valid reading",
			reading: WeatherReading{
				StationID:     "ws-001",
				Condition:     "sunny",
				EntityIDValue: WeatherReadingEntityID(testAuthority, "ws-001"),
			},
			wantErr: false,
		},
		{
			name: "missing station_id",
			reading: WeatherReading{
				Condition:     "sunny",
				EntityIDValue: WeatherReadingEntityID(testAuthority, ""),
			},
			wantErr: true,
		},
		{
			name: "missing condition",
			reading: WeatherReading{
				StationID:     "ws-001",
				EntityIDValue: WeatherReadingEntityID(testAuthority, "ws-001"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.reading.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Compile-time check that WeatherReading implements Graphable
func TestGraphableInterface(_ *testing.T) {
	var _ graph.Graphable = (*WeatherReading)(nil)
}
