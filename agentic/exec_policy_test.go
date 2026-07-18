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

// TestAdvertisedToolsFromMetadata covers the gh#551 accessor: the advertised
// tool set arrives as []any after a BaseMessage JSON round-trip but may be a
// native []string on the in-process path. The present flag must distinguish
// key-absent (no per-loop restriction, back-compat) from key-present-but-
// empty/malformed (executor fails closed) — collapsing the two would turn a
// malformed security control into a silent allow-all.
func TestAdvertisedToolsFromMetadata(t *testing.T) {
	tests := []struct {
		name        string
		meta        map[string]any
		wantTools   []string
		wantPresent bool
	}{
		{name: "nil metadata", meta: nil, wantTools: nil, wantPresent: false},
		{
			name:        "absent key",
			meta:        map[string]any{"loop_id": "x"},
			wantTools:   nil,
			wantPresent: false,
		},
		{
			name:        "[]any of strings (post-JSON-decode shape)",
			meta:        map[string]any{MetadataKeyAdvertisedTools: []any{"decide", "query_entity"}},
			wantTools:   []string{"decide", "query_entity"},
			wantPresent: true,
		},
		{
			name:        "native []string (in-process path)",
			meta:        map[string]any{MetadataKeyAdvertisedTools: []string{"decide"}},
			wantTools:   []string{"decide"},
			wantPresent: true,
		},
		{
			name:        "empty list is present-but-empty",
			meta:        map[string]any{MetadataKeyAdvertisedTools: []any{}},
			wantTools:   nil,
			wantPresent: true,
		},
		{
			name:        "non-list value is present-but-empty",
			meta:        map[string]any{MetadataKeyAdvertisedTools: "decide"},
			wantTools:   nil,
			wantPresent: true,
		},
		{
			name:        "list with non-string elements keeps only strings",
			meta:        map[string]any{MetadataKeyAdvertisedTools: []any{"decide", 42, nil, ""}},
			wantTools:   []string{"decide"},
			wantPresent: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTools, gotPresent := AdvertisedToolsFromMetadata(tt.meta)
			if gotPresent != tt.wantPresent {
				t.Errorf("present = %v, want %v", gotPresent, tt.wantPresent)
			}
			if !reflect.DeepEqual(gotTools, tt.wantTools) {
				t.Errorf("tools = %#v, want %#v", gotTools, tt.wantTools)
			}
		})
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

// TestIsKnownFilesystemPolicy locks the recognized enum set — an unknown value
// must be reported so callers can fail closed instead of silently degrading.
func TestIsKnownFilesystemPolicy(t *testing.T) {
	for _, p := range []string{"", FilesystemPolicyReadOnly, FilesystemPolicyWorkspaceWrite, FilesystemPolicyHostWrite} {
		if !IsKnownFilesystemPolicy(p) {
			t.Errorf("IsKnownFilesystemPolicy(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"readonly", "read-only", "ro", "workspace", "nonsense"} {
		if IsKnownFilesystemPolicy(p) {
			t.Errorf("IsKnownFilesystemPolicy(%q) = true, want false (unrecognized must fail closed)", p)
		}
	}
}
