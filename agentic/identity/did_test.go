package identity

import (
	"testing"
)

func TestParseDID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *DID
		wantErr bool
	}{
		{
			name:  "valid did:key",
			input: "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK",
			want: &DID{
				Method: "key",
				ID:     "z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK",
			},
		},
		{
			name:  "valid did:web",
			input: "did:web:example.com",
			want: &DID{
				Method: "web",
				ID:     "example.com",
			},
		},
		{
			name:  "valid did:web with path",
			input: "did:web:example.com:users:alice",
			want: &DID{
				Method: "web",
				ID:     "example.com:users:alice",
			},
		},
		{
			name:  "valid did with fragment",
			input: "did:key:z123#key-1",
			want: &DID{
				Method:   "key",
				ID:       "z123",
				Fragment: "key-1",
			},
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "missing did: prefix",
			input:   "key:z123",
			wantErr: true,
		},
		{
			name:    "missing method",
			input:   "did::z123",
			wantErr: true,
		},
		{
			name:    "missing id",
			input:   "did:key:",
			wantErr: true,
		},
		{
			name:    "only method",
			input:   "did:key",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.Method != tt.want.Method {
				t.Errorf("Method = %q, want %q", got.Method, tt.want.Method)
			}
			if got.ID != tt.want.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.want.ID)
			}
			if got.Fragment != tt.want.Fragment {
				t.Errorf("Fragment = %q, want %q", got.Fragment, tt.want.Fragment)
			}
		})
	}
}

func TestDID_String(t *testing.T) {
	tests := []struct {
		name string
		did  DID
		want string
	}{
		{
			name: "simple did",
			did:  DID{Method: "key", ID: "z123"},
			want: "did:key:z123",
		},
		{
			name: "did with fragment",
			did:  DID{Method: "key", ID: "z123", Fragment: "key-1"},
			want: "did:key:z123#key-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.did.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDID_Validate(t *testing.T) {
	tests := []struct {
		name    string
		did     DID
		wantErr bool
	}{
		{
			name:    "valid",
			did:     DID{Method: "key", ID: "z123"},
			wantErr: false,
		},
		{
			name:    "missing method",
			did:     DID{ID: "z123"},
			wantErr: true,
		},
		{
			name:    "missing id",
			did:     DID{Method: "key"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.did.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDID_WithFragment(t *testing.T) {
	did := DID{Method: "key", ID: "z123"}
	withFragment := did.WithFragment("key-1")

	if withFragment.Fragment != "key-1" {
		t.Errorf("expected fragment 'key-1', got %q", withFragment.Fragment)
	}
	// Original should be unchanged
	if did.Fragment != "" {
		t.Errorf("original DID fragment should be empty, got %q", did.Fragment)
	}
}

func TestDID_Equal(t *testing.T) {
	did1 := &DID{Method: "key", ID: "z123"}
	did2 := &DID{Method: "key", ID: "z123"}
	did3 := &DID{Method: "key", ID: "z456"}
	did4 := &DID{Method: "key", ID: "z123", Fragment: "key-1"}

	if !did1.Equal(did2) {
		t.Error("expected did1 to equal did2")
	}
	if did1.Equal(did3) {
		t.Error("expected did1 to not equal did3")
	}
	if did1.Equal(did4) {
		t.Error("expected did1 to not equal did4 (different fragment)")
	}
	if !did1.EqualIgnoreFragment(did4) {
		t.Error("expected did1 to equal did4 ignoring fragment")
	}
	if did1.Equal(nil) {
		t.Error("expected did1 to not equal nil")
	}
}

func TestDID_MarshalText(t *testing.T) {
	did := DID{Method: "key", ID: "z123", Fragment: "key-1"}

	text, err := did.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}

	expected := "did:key:z123#key-1"
	if string(text) != expected {
		t.Errorf("MarshalText() = %q, want %q", string(text), expected)
	}
}

func TestDID_UnmarshalText(t *testing.T) {
	var did DID
	err := did.UnmarshalText([]byte("did:key:z123#key-1"))
	if err != nil {
		t.Fatalf("UnmarshalText() error = %v", err)
	}

	if did.Method != "key" {
		t.Errorf("Method = %q, want 'key'", did.Method)
	}
	if did.ID != "z123" {
		t.Errorf("ID = %q, want 'z123'", did.ID)
	}
	if did.Fragment != "key-1" {
		t.Errorf("Fragment = %q, want 'key-1'", did.Fragment)
	}
}

func TestNewKeyDID(t *testing.T) {
	did := NewKeyDID("z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK")

	if did.Method != MethodKey {
		t.Errorf("Method = %q, want %q", did.Method, MethodKey)
	}
	if did.ID != "z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK" {
		t.Errorf("ID = %q, want 'z6Mkha...'", did.ID)
	}
}

func TestNewWebDID(t *testing.T) {
	did := NewWebDID("example.com", "users", "alice")

	if did.Method != MethodWeb {
		t.Errorf("Method = %q, want %q", did.Method, MethodWeb)
	}
	if did.ID != "example.com:users:alice" {
		t.Errorf("ID = %q, want 'example.com:users:alice'", did.ID)
	}
}
