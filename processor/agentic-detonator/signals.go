package agenticdetonator

// Signal bucket constants — mirror the ADR-043 line 206 enum that
// the Phase 2 corpus loader and classifier consume. Kept in this
// package as constants rather than referencing strings inline so a
// typo surfaces at compile time and the bucket set is greppable.
const (
	SignalFilesystemRead      = "filesystem-read"
	SignalCodeExec            = "code-exec"
	SignalNetworkEgress       = "network-egress"
	SignalExfilEmail          = "exfil-email"
	SignalDataAccess          = "data-access"
	SignalSecretAccess        = "secret-access"
	SignalCredEnum            = "cred-enum"
	SignalInstructionOverride = "instruction-override"
	SignalBenign              = "benign"
)

// signalForTool maps a tool name to its ADR-043 signal bucket. The
// mapping is structural — a tool's CAPABILITY classifies the
// attempt, regardless of arguments. read_file is always
// filesystem-read; whether the arg was /etc/passwd or /var/log
// shifts severity, not bucket.
//
// Returns "" for tools we have no opinion on; callers treat that as
// "no signal contributed" and fall back to other classifiers (the
// markdown-URL scanner, the LLM's text output).
func signalForTool(toolName string) string {
	switch toolName {
	case ToolReadFile:
		return SignalFilesystemRead
	case ToolExecute, ToolBash:
		return SignalCodeExec
	case ToolFetch, ToolHTTPRequest:
		return SignalNetworkEgress
	case ToolSendEmail, ToolSendMessage:
		return SignalExfilEmail
	case ToolReadEnv, ToolGetAPIKey:
		return SignalSecretAccess
	case ToolDBQuery:
		return SignalDataAccess
	case ToolListUsers:
		return SignalCredEnum
	default:
		return ""
	}
}

// SignalsFromTrajectory returns the deduped set of signals produced
// by a trajectory of tool calls. Order follows first-observation
// order so the dominant attack shape (the first tool the canary
// reached for) stays at index 0 — useful when downstream wants a
// "primary" signal.
//
// A trajectory with zero recorded calls returns an empty slice;
// callers fold that into the markdown-URL scanner output before
// deciding whether to label the detonation benign.
func SignalsFromTrajectory(calls []RecordedCall) []string {
	seen := make(map[string]struct{}, len(calls))
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		sig := signalForTool(c.ToolName)
		if sig == "" {
			continue
		}
		if _, ok := seen[sig]; ok {
			continue
		}
		seen[sig] = struct{}{}
		out = append(out, sig)
	}
	return out
}

// PrimarySignal picks the highest-priority signal from a list. The
// priority order encodes operator severity intuition: code-exec is
// a higher-severity tickle than network-egress, which is higher
// than data-access, and so on. Returns SignalBenign when the input
// is empty so the corpus loader always sees a valid bucket.
//
// Used by the Detonation aggregator when collapsing a multi-signal
// trajectory into a single Record.Signal — the Record format is
// single-label by design (Phase 4 may extend to multi-label
// classification per ADR-043 line 268-272).
func PrimarySignal(signals []string) string {
	if len(signals) == 0 {
		return SignalBenign
	}
	// Priority order — earlier wins. Matches the operator-side
	// "what kind of attack do I want to know about first" intuition
	// from the ADR-043 OSINT threat table (lines 32-39).
	priority := []string{
		SignalCodeExec,
		SignalSecretAccess,
		SignalCredEnum,
		SignalFilesystemRead,
		SignalNetworkEgress,
		SignalDataAccess,
		SignalExfilEmail,
		SignalInstructionOverride,
		SignalBenign,
	}
	have := make(map[string]struct{}, len(signals))
	for _, s := range signals {
		have[s] = struct{}{}
	}
	for _, p := range priority {
		if _, ok := have[p]; ok {
			return p
		}
	}
	// Signal that's not in the priority list (unknown / custom).
	// Returning the first observed keeps the result deterministic
	// without inventing a bucket.
	return signals[0]
}
