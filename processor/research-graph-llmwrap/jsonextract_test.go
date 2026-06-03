package llmwrap

import (
	"testing"
	"unicode/utf8"
)

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
		name string
		in   string
		n    int
		want string
	}{
		// ASCII path — pre-existing coverage.
		{"shorter than cap", "hello", 10, "hello"},
		{"equal to cap", "hello", 5, "hello"},
		{"longer than cap", "hello world", 5, "hello..."},
		{"empty", "", 5, ""},

		// gh#190 multi-byte rune cases. CJK characters are 3-byte
		// UTF-8 sequences. If the cap lands mid-rune, the pre-fix
		// truncate produced invalid UTF-8 in error-message snippets
		// that slog rendered as "�" replacement sequences.
		// Post-fix walks back to the nearest preceding rune boundary.
		//
		// "你好世界" is 4 CJK runes × 3 bytes each = 12 bytes total.
		// Cap=5 lands inside the second rune (byte 5 of "你好") —
		// walk back to byte 3 (end of "你") and append the ellipsis.
		{"CJK cap mid-rune walks back", "你好世界", 5, "你..."},
		// Cap=6 lands cleanly at the end of "你好" (byte 6 == rune
		// boundary). No walk-back needed.
		{"CJK cap at rune boundary", "你好世界", 6, "你好..."},
		// Cap exceeds string length → unchanged (early-exit path).
		{"CJK shorter than cap", "你好", 100, "你好"},
		// 4-byte rune (emoji). "🌍" is U+1F30D = 4 bytes. With "ab🌍cd"
		// (7 bytes total: a, b, F0, 9F, 8C, 8D, c, d → actually "ab" +
		// 4-byte emoji + "cd" = 2+4+2 = 8 bytes), cap=4 lands inside
		// the emoji at byte 4 (3rd continuation byte). Walk back to
		// byte 2, the rune-start of the emoji. Result: "ab...".
		{"emoji cap mid-rune walks back", "ab🌍cd", 4, "ab..."},
		// Mixed ASCII + CJK. "ab你cd" is 2+3+2 = 7 bytes. Cap=3 lands
		// mid-CJK-rune at byte 3 (continuation byte of "你") — walk
		// back to byte 2 ("ab" end). Result: "ab...".
		{"mixed ASCII+CJK cap mid-rune", "ab你cd", 3, "ab..."},

		// gh#190 review M1 — additional edge cases:

		// Cap=0 on non-empty input: walk-back loop doesn't execute
		// (n>0 false), result is empty body + ellipsis. Locks the
		// "tiny cap" contract.
		{"cap zero on non-empty input", "hello", 0, "..."},
		// Garbage UTF-8 (all continuation bytes, no rune-start). The
		// walk-back hits n=0 by exhaustion and returns "...". The
		// n>0 guard prevents infinite loop on adversarial input.
		{"all continuation bytes (garbage)", "\x80\x80\x80\x80", 2, "..."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Truncate(c.in, c.n)
			if got != c.want {
				t.Errorf("Truncate(%q,%d) = %q, want %q", c.in, c.n, got, c.want)
			}
			// gh#190 invariant: the un-ellipsis portion of the output
			// MUST be valid UTF-8. Pre-fix this could fail on
			// mid-rune cuts.
			if len(got) >= 3 && got[len(got)-3:] == "..." {
				body := got[:len(got)-3]
				if !utf8.ValidString(body) {
					t.Errorf("Truncate(%q,%d) returned invalid UTF-8 body %q", c.in, c.n, body)
				}
			}
		})
	}
}
