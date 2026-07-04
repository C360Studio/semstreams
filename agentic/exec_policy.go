package agentic

// Read-only execution policy (ADR-067, gh#443). A task-scoped filesystem
// write-scope that flows to write-capable tool executors (v1: bash) via
// ToolCall.Metadata and is enforced framework-side on every call. The model
// produces only tool Arguments, never Metadata, and dispatch stamps these keys
// authoritatively at the shared dispatchToolCall seam — so an inspect role
// (planner, plan-reviewer) cannot create, modify, or delete product-worktree
// files even though it has bash for inspection and probes.

// MetadataKeyFilesystemPolicy is the TaskMessage.Metadata / ToolCall.Metadata
// key under which a task's filesystem write-scope flows to write-capable tool
// executors (v1: bash). Values are the sandbox-substrate filesystem enum
// (ADR-052 vocabulary): FilesystemPolicyReadOnly | FilesystemPolicyWorkspaceWrite.
// Absent/empty == workspace_write (unchanged, back-compat).
//
// Under read_only the bash executor enforces a pre+post git worktree-and-HEAD
// non-mutation proof and returns a typed violation on mutation. The value is
// model-uncontrollable: stamped by dispatchToolCall as an authoritative
// overwrite (a framework enforcement fact), never read from tool Arguments.
//
// Set by the product's role→policy mapping when spawning an inspect-role loop
// (role→policy is product-layer; the framework owns the enforcement floor).
// Per-task, NOT inherited by spawned sub-loops — a product must re-assert it on
// each spawned inspect child (see ADR-067 §5).
const MetadataKeyFilesystemPolicy = "agent.exec.filesystem_policy"

// MetadataKeyScratchPaths is the TaskMessage.Metadata / ToolCall.Metadata key
// under which IN-WORKTREE paths exempt from the read_only proof flow (e.g. a
// declared ".scratch/" build dir). Out-of-worktree paths (the common case:
// /tmp) are exempt automatically — git in the worktree never reports them, so
// probes there are invisible to the proof by construction.
//
// []string. Comes back from BaseMessage decode as []any (each element still a
// Go string); readers coerce on access, like MetadataKeyDecideActionAllowlist.
// Empty == only out-of-worktree paths are writable.
const MetadataKeyScratchPaths = "agent.exec.scratch_paths"

// Filesystem policy values. Deliberately match the sandbox-substrate filesystem
// enum (ADR-052 vocabulary/sandbox: filesystem.read_only / filesystem.workspace_write)
// so there is ONE filesystem-policy model across the framework and the future
// substrate. host_write has no v1 meaning here (an environment-level concern the
// substrate owns).
const (
	FilesystemPolicyReadOnly       = "read_only"
	FilesystemPolicyWorkspaceWrite = "workspace_write"
)

// DispatchEnforcedMetadataKeys is the set of task-scoped enforcement keys that
// dispatchToolCall stamps AUTHORITATIVELY (overwrite) from the loop's cached
// task metadata onto every tool call — so they reach every dispatch path (main,
// approval re-dispatch, queue dequeue), not just the main-path merge. These are
// framework enforcement facts; a fill-only merge would let a stray pre-existing
// value defeat the control. Absent keys stay absent (back-compat).
//
// MetadataKeyDecideActionAllowlist is included because it had the identical
// approval-redispatch gap (an approved decide call silently lost its closed
// vocabulary) — fixed at the same seam (ADR-067 §2).
var DispatchEnforcedMetadataKeys = []string{
	MetadataKeyFilesystemPolicy,
	MetadataKeyScratchPaths,
	MetadataKeyDecideActionAllowlist,
}

// FilesystemPolicyFromMetadata resolves the read-only execution policy from a
// tool call's metadata. It returns the policy string (FilesystemPolicyReadOnly
// or "" for the default workspace_write) and the in-worktree scratch-path
// exemptions. Nil/absent metadata yields ("", nil) — the back-compat default.
//
// scratch values arrive from BaseMessage JSON decode as []any (each element a
// Go string); this coerces them, dropping non-string / empty entries.
func FilesystemPolicyFromMetadata(m map[string]any) (policy string, scratch []string) {
	if m == nil {
		return "", nil
	}
	if p, ok := m[MetadataKeyFilesystemPolicy].(string); ok {
		policy = p
	}
	scratch = coerceStringSlice(m[MetadataKeyScratchPaths])
	return policy, scratch
}

// IsReadOnlyPolicy reports whether a resolved policy string means read_only.
func IsReadOnlyPolicy(policy string) bool {
	return policy == FilesystemPolicyReadOnly
}

// coerceStringSlice coerces a metadata value into []string. JSON decode lands a
// list as []any (with string elements); a native []string is also accepted (the
// in-process spawn path that never round-trips through JSON). Anything else, or
// a nil, yields nil. Non-string and empty elements are dropped.
func coerceStringSlice(raw any) []string {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}
