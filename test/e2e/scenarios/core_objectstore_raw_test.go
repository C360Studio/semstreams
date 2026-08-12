package scenarios

import (
	"fmt"
	"testing"
)

func TestExtractRawStorageEnvelope(t *testing.T) {
	t.Parallel()

	const markerField = "_e2e_objectstore_raw_marker"
	tests := []struct {
		name    string
		wire    string
		want    rawStorageEnvelope
		wantErr bool
	}{
		{
			name: "mapped generic JSON envelope",
			wire: fmt.Sprintf(`{
				"id":"8ad1c49e-fc3f-491f-b927-6e208a04d79c",
				"type":{"domain":"core","category":"json","version":"v1"},
				"payload":{"data":{"%s":"pair-1-a","value":61}}
			}`, markerField),
			want: rawStorageEnvelope{
				wireID:      "8ad1c49e-fc3f-491f-b927-6e208a04d79c",
				messageType: "core.json.v1",
				marker:      "pair-1-a",
			},
		},
		{
			name:    "invalid JSON",
			wire:    `{`,
			wantErr: true,
		},
		{
			name: "missing wire ID",
			wire: fmt.Sprintf(`{
				"type":{"domain":"core","category":"json","version":"v1"},
				"payload":{"data":{"%s":"pair-1-a"}}
			}`, markerField),
			wantErr: true,
		},
		{
			name: "incomplete message type",
			wire: fmt.Sprintf(`{
				"id":"8ad1c49e-fc3f-491f-b927-6e208a04d79c",
				"type":{"domain":"core","category":"json"},
				"payload":{"data":{"%s":"pair-1-a"}}
			}`, markerField),
			wantErr: true,
		},
		{
			name: "missing marker",
			wire: `{
				"id":"8ad1c49e-fc3f-491f-b927-6e208a04d79c",
				"type":{"domain":"core","category":"json","version":"v1"},
				"payload":{"data":{"value":61}}
			}`,
			wantErr: true,
		},
		{
			name: "numeric marker",
			wire: fmt.Sprintf(`{
				"id":"8ad1c49e-fc3f-491f-b927-6e208a04d79c",
				"type":{"domain":"core","category":"json","version":"v1"},
				"payload":{"data":{"%s":1}}
			}`, markerField),
			wantErr: true,
		},
		{
			name: "empty marker",
			wire: fmt.Sprintf(`{
				"id":"8ad1c49e-fc3f-491f-b927-6e208a04d79c",
				"type":{"domain":"core","category":"json","version":"v1"},
				"payload":{"data":{"%s":""}}
			}`, markerField),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := extractRawStorageEnvelope([]byte(tt.wire), markerField)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("extractRawStorageEnvelope() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("extractRawStorageEnvelope() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("extractRawStorageEnvelope() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestRawStorageKeyNonce(t *testing.T) {
	t.Parallel()

	const wireID = "8ad1c49e-fc3f-491f-b927-6e208a04d79c"
	const nonce = "36b14474-dd83-4be4-889b-0b348b64550f"
	tests := []struct {
		name      string
		key       string
		wantNonce string
		wantErr   bool
	}{
		{
			name:      "canonical decoded-envelope key",
			key:       "core.json.v1/2026/08/12/15/" + wireID + "_" + nonce,
			wantNonce: nonce,
		},
		{
			name:    "wrong type prefix",
			key:     "message/2026/08/12/15/" + wireID + "_" + nonce,
			wantErr: true,
		},
		{
			name:    "different wire ID",
			key:     "core.json.v1/2026/08/12/15/other_" + nonce,
			wantErr: true,
		},
		{
			name:    "numeric seconds suffix is not a nonce",
			key:     "core.json.v1/2026/08/12/15/" + wireID + "_1786554000",
			wantErr: true,
		},
		{
			name:    "numeric nanoseconds suffix is not a nonce",
			key:     "core.json.v1/2026/08/12/15/" + wireID + "_1786554000123456789",
			wantErr: true,
		},
		{
			name:    "non UUID suffix",
			key:     "core.json.v1/2026/08/12/15/" + wireID + "_nonce",
			wantErr: true,
		},
		{
			name:    "UUID with trailing data",
			key:     "core.json.v1/2026/08/12/15/" + wireID + "_" + nonce + "-extra",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := rawStorageKeyNonce(tt.key, wireID)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("rawStorageKeyNonce() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("rawStorageKeyNonce() error = %v", err)
			}
			if got != tt.wantNonce {
				t.Fatalf("rawStorageKeyNonce() = %q, want %q", got, tt.wantNonce)
			}
		})
	}
}
