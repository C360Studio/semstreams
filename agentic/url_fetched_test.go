package agentic

import (
	"reflect"
	"testing"
)

func TestExtractFetchedURLs(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want []string
	}{
		// Positive — fetch verbs.
		{"curl simple", "curl https://x.com", []string{"https://x.com"}},
		{"curl with flags and query", "curl -sL -o out https://x.com/a?q=b", []string{"https://x.com/a?q=b"}},
		{"curl multi-param query preserved", "curl https://x.com/a?q=b&r=c", []string{"https://x.com/a?q=b&r=c"}},
		{"wget", "wget https://x.com/file.tar", []string{"https://x.com/file.tar"}},
		{"httpie", "http https://api.x.com/v1/items", []string{"https://api.x.com/v1/items"}},
		{"abs path to curl", "/usr/bin/curl https://x.com", []string{"https://x.com"}},
		{"multi via semicolon", "curl https://a.com; curl https://b.com", []string{"https://a.com", "https://b.com"}},
		{"multi via &&", "curl https://a.com && curl https://b.com", []string{"https://a.com", "https://b.com"}},
		{"dedup repeated", "curl https://a.com; curl https://a.com", []string{"https://a.com"}},
		{"curl piped to grep keeps only fetched", "curl https://a.com | grep https://b.com", []string{"https://a.com"}},

		// Negative — URL is data, not a fetch.
		{"echo string", `echo "https://x.com"`, nil},
		{"grep pattern", "grep https://x logfile", nil},
		{"filesystem path", "cat /var/log/http.log", nil},
		{"no url", "ls -la /tmp", nil},
		{"empty", "", nil},
		{"http-named file, not fetch", "rm http_notes.txt", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractFetchedURLs(tt.cmd)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ExtractFetchedURLs(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestBashStepURLs(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		args     map[string]any
		want     []string
	}{
		{"bash with fetch", "bash", map[string]any{"command": "curl https://x.com"}, []string{"https://x.com"}},
		{"bash no fetch", "bash", map[string]any{"command": "ls"}, nil},
		{"non-bash tool ignored", "read_file", map[string]any{"command": "curl https://x.com"}, nil},
		{"bash missing command", "bash", map[string]any{}, nil},
		{"bash non-string command", "bash", map[string]any{"command": 42}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BashStepURLs(tt.toolName, tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("BashStepURLs(%q, %v) = %v, want %v", tt.toolName, tt.args, got, tt.want)
			}
		})
	}
}
