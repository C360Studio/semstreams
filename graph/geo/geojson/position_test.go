package geojson

import (
	"encoding/json"
	"math"
	"testing"
)

func TestPosition_LonLatAlt(t *testing.T) {
	p := NewPositionAlt(-122.5, 37.7, 100)
	if p.Lon() != -122.5 {
		t.Errorf("Lon() = %v, want -122.5", p.Lon())
	}
	if p.Lat() != 37.7 {
		t.Errorf("Lat() = %v, want 37.7", p.Lat())
	}
	if p.Alt() != 100 {
		t.Errorf("Alt() = %v, want 100", p.Alt())
	}
	if !p.Has3D() {
		t.Errorf("Has3D() = false, want true")
	}
}

func TestPosition_2DNoAlt(t *testing.T) {
	p := NewPosition(-122.5, 37.7)
	if p.Has3D() {
		t.Errorf("Has3D() = true on 2D position")
	}
	if !math.IsNaN(p.Alt()) {
		t.Errorf("Alt() on 2D position should be NaN, got %v", p.Alt())
	}
}

func TestPosition_Valid(t *testing.T) {
	cases := []struct {
		p    Position
		want bool
	}{
		{NewPosition(0, 0), true},
		{NewPosition(-180, -90), true},
		{NewPosition(180, 90), true},
		{NewPosition(180.0001, 0), false}, // lon out
		{NewPosition(0, 90.0001), false},  // lat out
		{NewPosition(math.NaN(), 0), false},
		{Position{}, false},
		{Position{1.0}, false}, // too short
	}
	for _, c := range cases {
		if got := c.p.Valid(); got != c.want {
			t.Errorf("Valid(%v) = %v, want %v", c.p, got, c.want)
		}
	}
}

func TestPosition_Normalize_LongitudeWrap(t *testing.T) {
	cases := []struct {
		in       float64
		wantLon  float64
		wantNote string
	}{
		{0, 0, "zero"},
		{179, 179, "in range positive"},
		{-179, -179, "in range negative"},
		{180, 180, "exact +180"},
		{-180, -180, "exact -180"},
		{181, -179, "wrap east-bound"},
		{-181, 179, "wrap west-bound"},
		{540, 180, "540 → 180"},
		{-540, -180, "-540 → -180"},
	}
	for _, c := range cases {
		t.Run(c.wantNote, func(t *testing.T) {
			p := NewPosition(c.in, 0).Normalize()
			if p.Lon() != c.wantLon {
				t.Errorf("normalize lon=%v: got %v, want %v", c.in, p.Lon(), c.wantLon)
			}
		})
	}
}

func TestPosition_Normalize_LatitudeClamps(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{0, 0},
		{90, 90},
		{-90, -90},
		{91, 90},
		{-91, -90},
		{120, 90},
		{-120, -90},
	}
	for _, c := range cases {
		p := NewPosition(0, c.in).Normalize()
		if p.Lat() != c.want {
			t.Errorf("clamp lat=%v: got %v, want %v", c.in, p.Lat(), c.want)
		}
	}
}

func TestPosition_UnmarshalJSON(t *testing.T) {
	cases := []struct {
		json    string
		wantErr bool
		wantLen int
	}{
		{`[-122.5, 37.7]`, false, 2},
		{`[-122.5, 37.7, 100]`, false, 3},
		{`[-122.5]`, true, 0},   // too short
		{`"point"`, true, 0},    // wrong type
		{`null`, true, 0},       // null
		{`[1, "two"]`, true, 0}, // non-numeric
	}
	for _, c := range cases {
		var p Position
		err := json.Unmarshal([]byte(c.json), &p)
		if c.wantErr {
			if err == nil {
				t.Errorf("Unmarshal(%s): expected error, got nil", c.json)
			}
			continue
		}
		if err != nil {
			t.Errorf("Unmarshal(%s): unexpected error: %v", c.json, err)
			continue
		}
		if len(p) != c.wantLen {
			t.Errorf("Unmarshal(%s): len = %d, want %d", c.json, len(p), c.wantLen)
		}
	}
}
