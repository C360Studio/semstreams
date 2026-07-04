package agentic

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestFilesystemPolicyFromMetadata_Coercion covers the accessor's coercion of
// the scratch list — which arrives as []any after a BaseMessage JSON round-trip
// but may be a native []string on the in-process spawn path — plus the
// absent/default cases.
func TestFilesystemPolicyFromMetadata_Coercion(t *testing.T) {
	tests := []struct {
		name        string
		meta        map[string]any
		wantPolicy  string
		wantScratch []string
	}{
		{name: "nil metadata", meta: nil, wantPolicy: "", wantScratch: nil},
		{name: "absent keys", meta: map[string]any{"loop_id": "x"}, wantPolicy: "", wantScratch: nil},
		{
			name:       "read_only, no scratch",
			meta:       map[string]any{MetadataKeyFilesystemPolicy: FilesystemPolicyReadOnly},
			wantPolicy: FilesystemPolicyReadOnly, wantScratch: nil,
		},
		{
			name: "scratch as []any (post-JSON-decode shape)",
			meta: map[string]any{
				MetadataKeyFilesystemPolicy: FilesystemPolicyReadOnly,
				MetadataKeyScratchPaths:     []any{".scratch", "build/"},
			},
			wantPolicy: FilesystemPolicyReadOnly, wantScratch: []string{".scratch", "build/"},
		},
		{
			name: "scratch as native []string (in-process spawn)",
			meta: map[string]any{
				MetadataKeyFilesystemPolicy: FilesystemPolicyReadOnly,
				MetadataKeyScratchPaths:     []string{".scratch"},
			},
			wantPolicy: FilesystemPolicyReadOnly, wantScratch: []string{".scratch"},
		},
		{
			name: "scratch drops non-string and empty entries",
			meta: map[string]any{
				MetadataKeyScratchPaths: []any{".scratch", "", 42, nil, "ok"},
			},
			wantPolicy: "", wantScratch: []string{".scratch", "ok"},
		},
		{
			name:       "workspace_write is not read_only",
			meta:       map[string]any{MetadataKeyFilesystemPolicy: FilesystemPolicyWorkspaceWrite},
			wantPolicy: FilesystemPolicyWorkspaceWrite, wantScratch: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPolicy, gotScratch := FilesystemPolicyFromMetadata(tt.meta)
			if gotPolicy != tt.wantPolicy {
				t.Errorf("policy = %q, want %q", gotPolicy, tt.wantPolicy)
			}
			if !reflect.DeepEqual(gotScratch, tt.wantScratch) {
				t.Errorf("scratch = %#v, want %#v", gotScratch, tt.wantScratch)
			}
			if IsReadOnlyPolicy(gotPolicy) != (tt.wantPolicy == FilesystemPolicyReadOnly) {
				t.Errorf("IsReadOnlyPolicy(%q) mismatch", gotPolicy)
			}
		})
	}
}

// TestFilesystemPolicyFromMetadata_JSONRoundTrip proves the accessor reads the
// policy back correctly after the exact BaseMessage-style JSON round-trip a
// task's Metadata takes on the wire — the shape ([]any for the scratch list) is
// what actually reaches a tool executor, not the native []string a test literal
// might use.
func TestFilesystemPolicyFromMetadata_JSONRoundTrip(t *testing.T) {
	orig := map[string]any{
		MetadataKeyFilesystemPolicy: FilesystemPolicyReadOnly,
		MetadataKeyScratchPaths:     []string{".scratch", "tmp-build"},
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Sanity: after decode the slice is []any, not []string — the shape the
	// accessor must tolerate.
	if _, ok := decoded[MetadataKeyScratchPaths].([]any); !ok {
		t.Fatalf("expected []any after JSON decode, got %T", decoded[MetadataKeyScratchPaths])
	}

	policy, scratch := FilesystemPolicyFromMetadata(decoded)
	if policy != FilesystemPolicyReadOnly {
		t.Errorf("policy = %q, want read_only", policy)
	}
	if !reflect.DeepEqual(scratch, []string{".scratch", "tmp-build"}) {
		t.Errorf("scratch = %#v, want [.scratch tmp-build]", scratch)
	}
}

// TestDispatchEnforcedMetadataKeys_Membership locks the set that dispatch stamps
// path-universally: the read-only policy keys AND the decide allowlist (whose
// approval-redispatch gap is fixed at the same seam, ADR-067 §2).
func TestDispatchEnforcedMetadataKeys_Membership(t *testing.T) {
	want := map[string]bool{
		MetadataKeyFilesystemPolicy:      true,
		MetadataKeyScratchPaths:          true,
		MetadataKeyDecideActionAllowlist: true,
	}
	if len(DispatchEnforcedMetadataKeys) != len(want) {
		t.Fatalf("DispatchEnforcedMetadataKeys = %v, want keys %v", DispatchEnforcedMetadataKeys, want)
	}
	for _, k := range DispatchEnforcedMetadataKeys {
		if !want[k] {
			t.Errorf("unexpected key %q in DispatchEnforcedMetadataKeys", k)
		}
	}
}
