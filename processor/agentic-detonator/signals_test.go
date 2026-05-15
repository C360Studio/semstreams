package agenticdetonator

import (
	"reflect"
	"testing"
)

// TestSignalsFromTrajectory_BucketMapping pins the structural
// mapping: tool name → ADR-043 signal bucket. The mapping is a
// load-bearing contract — corpus consumers downstream rely on a
// stable name-to-bucket relationship.
func TestSignalsFromTrajectory_BucketMapping(t *testing.T) {
	cases := []struct {
		name string
		tool string
		want string
	}{
		{"read_file → filesystem-read", ToolReadFile, SignalFilesystemRead},
		{"bash → code-exec", ToolBash, SignalCodeExec},
		{"execute → code-exec", ToolExecute, SignalCodeExec},
		{"fetch → network-egress", ToolFetch, SignalNetworkEgress},
		{"http_request → network-egress", ToolHTTPRequest, SignalNetworkEgress},
		{"send_email → exfil-email", ToolSendEmail, SignalExfilEmail},
		{"send_message → exfil-email", ToolSendMessage, SignalExfilEmail},
		{"read_env → secret-access", ToolReadEnv, SignalSecretAccess},
		{"get_api_key → secret-access", ToolGetAPIKey, SignalSecretAccess},
		{"db_query → data-access", ToolDBQuery, SignalDataAccess},
		{"list_users → cred-enum", ToolListUsers, SignalCredEnum},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			traj := []RecordedCall{{ToolName: tc.tool}}
			got := SignalsFromTrajectory(traj)
			if len(got) != 1 || got[0] != tc.want {
				t.Errorf("SignalsFromTrajectory(%s) = %v, want [%s]", tc.tool, got, tc.want)
			}
		})
	}
}

// TestSignalsFromTrajectory_DeduplicatesPreservesOrder ensures
// multiple calls to the same-bucket tools collapse to one signal,
// and the surviving signal sits at the position of its
// first-observation occurrence.
func TestSignalsFromTrajectory_DeduplicatesPreservesOrder(t *testing.T) {
	traj := []RecordedCall{
		{ToolName: ToolFetch},       // network-egress (first)
		{ToolName: ToolBash},        // code-exec
		{ToolName: ToolHTTPRequest}, // network-egress dup
		{ToolName: ToolReadEnv},     // secret-access
	}
	got := SignalsFromTrajectory(traj)
	want := []string{SignalNetworkEgress, SignalCodeExec, SignalSecretAccess}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dedup/order broken: got %v, want %v", got, want)
	}
}

// TestSignalsFromTrajectory_UnknownToolSkipped confirms tools we
// have no opinion on don't pollute the signal output. The canary
// might invent a tool; we record it (executor) but don't
// fabricate a signal bucket without evidence (signals).
func TestSignalsFromTrajectory_UnknownToolSkipped(t *testing.T) {
	traj := []RecordedCall{
		{ToolName: "imaginary_tool"},
		{ToolName: ToolBash},
		{ToolName: "another_invention"},
	}
	got := SignalsFromTrajectory(traj)
	want := []string{SignalCodeExec}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unknown tools should not contribute signals: got %v, want %v", got, want)
	}
}

// TestPrimarySignal_PriorityOrder pins the operator severity
// intuition: code-exec wins over secret-access wins over
// filesystem-read wins over network-egress wins over data-access.
// If a future refactor reorders the priority list this test
// surfaces the change explicitly.
func TestPrimarySignal_PriorityOrder(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"single benign", []string{SignalBenign}, SignalBenign},
		{"code-exec beats network-egress", []string{SignalNetworkEgress, SignalCodeExec}, SignalCodeExec},
		{"secret-access beats data-access", []string{SignalDataAccess, SignalSecretAccess}, SignalSecretAccess},
		{"filesystem-read beats network-egress", []string{SignalNetworkEgress, SignalFilesystemRead}, SignalFilesystemRead},
		{"empty → benign", []string{}, SignalBenign},
		{"unknown bucket survives if alone", []string{"custom-bucket"}, "custom-bucket"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PrimarySignal(tc.in)
			if got != tc.want {
				t.Errorf("PrimarySignal(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
