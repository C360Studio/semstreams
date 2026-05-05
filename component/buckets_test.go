package component

import "testing"

func TestBucketName(t *testing.T) {
	tests := []struct {
		name string
		base string
		org  string
		want string
	}{
		{
			name: "empty org returns base unchanged",
			base: "RULE_STATE",
			org:  "",
			want: "RULE_STATE",
		},
		{
			name: "non-empty org appends underscore-org",
			base: "RULE_STATE",
			org:  "acme",
			want: "RULE_STATE_acme",
		},
		{
			name: "multi-word base with org",
			base: "AGENT_LOOPS",
			org:  "myorg",
			want: "AGENT_LOOPS_myorg",
		},
		{
			name: "hyphenated org",
			base: "COMPONENT_STATUS",
			org:  "my-org",
			want: "COMPONENT_STATUS_my-org",
		},
		{
			name: "lowercase base (semstreams_config) with org",
			base: "semstreams_config",
			org:  "acme",
			want: "semstreams_config_acme",
		},
		{
			name: "empty org with lowercase base returns base unchanged",
			base: "semstreams_config",
			org:  "",
			want: "semstreams_config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BucketName(tt.base, tt.org)
			if got != tt.want {
				t.Errorf("BucketName(%q, %q) = %q, want %q", tt.base, tt.org, got, tt.want)
			}
		})
	}
}
