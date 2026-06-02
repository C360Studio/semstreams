package llmwrap

import "testing"

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "bare object",
			input: `{"sufficient":true}`,
			want:  `{"sufficient":true}`,
		},
		{
			name:  "markdown fenced",
			input: "```json\n{\"sufficient\":false,\"refined_queries\":[\"x\"]}\n```",
			want:  `{"sufficient":false,"refined_queries":["x"]}`,
		},
		{
			name:  "prose preface",
			input: `Sure — here is the assessment:` + "\n" + `{"sufficient":true,"rationale":"clear hits"}` + "\nLet me know if you need more.",
			want:  `{"sufficient":true,"rationale":"clear hits"}`,
		},
		{
			name:  "nested braces preserved",
			input: `{"synthesis":"x","evidence_refs":["a","b"],"meta":{"k":"v"}}`,
			want:  `{"synthesis":"x","evidence_refs":["a","b"],"meta":{"k":"v"}}`,
		},
		{
			name:    "no object",
			input:   "I cannot answer this.",
			wantErr: true,
		},
		{
			name:    "unbalanced",
			input:   `{"action":"x"`,
			wantErr: true,
		},
		{
			name:  "brace inside string value",
			input: `{"sufficient":true,"rationale":"axes spanning {time, entity_type}"}`,
			want:  `{"sufficient":true,"rationale":"axes spanning {time, entity_type}"}`,
		},
		{
			name:  "both braces inside string value",
			input: `{"sufficient":false,"rationale":"oh no a } in prose and another { for good measure"}`,
			want:  `{"sufficient":false,"rationale":"oh no a } in prose and another { for good measure"}`,
		},
		{
			name:  "escaped quote does not exit string",
			input: `{"synthesis":"the \"quoted\" } term"}`,
			want:  `{"synthesis":"the \"quoted\" } term"}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ExtractJSON(c.input)
			if c.wantErr {
				if err == nil {
					t.Errorf("ExtractJSON: want error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtractJSON: unexpected error %v", err)
			}
			if string(got) != c.want {
				t.Errorf("ExtractJSON = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
	}
	for _, c := range cases {
		got := Truncate(c.in, c.n)
		if got != c.want {
			t.Errorf("Truncate(%q,%d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
