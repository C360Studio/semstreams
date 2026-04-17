package operatingmodel

import (
	"encoding/json"
	"strings"
	"testing"
)

func validProfileContext() *ProfileContext {
	return &ProfileContext{
		UserID:         "coby",
		LoopID:         "loop-abc",
		ProfileVersion: 1,
		OperatingModel: ProfileContextSlice{
			Content:    "- weekly planning block: Mondays 9-10am",
			TokenCount: 12,
			EntryCount: 1,
		},
		TokenBudget: 800,
		AssembledAt: fixedTime(),
	}
}

func TestProfileContext_Schema(t *testing.T) {
	p := validProfileContext()
	got := p.Schema()
	if got.Domain != Domain || got.Category != CategoryProfileContext || got.Version != SchemaVersion {
		t.Errorf("Schema = %+v", got)
	}
}

func TestProfileContext_Validate_Success(t *testing.T) {
	p := validProfileContext()
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate = %v, want nil", err)
	}
}

func TestProfileContext_Validate_EmptyProfileAllowed(t *testing.T) {
	// User hasn't onboarded yet. Hydrator still publishes an empty payload.
	p := &ProfileContext{UserID: "coby", LoopID: "loop-abc"}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate empty profile = %v, want nil", err)
	}
	if p.HasOperatingModel() {
		t.Error("HasOperatingModel for empty profile = true, want false")
	}
	if p.SystemPromptPreamble() != "" {
		t.Errorf("SystemPromptPreamble for empty profile = %q, want empty", p.SystemPromptPreamble())
	}
}

func TestProfileContext_Validate_Errors(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ProfileContext)
		wantIn string
	}{
		{"missing user", func(p *ProfileContext) { p.UserID = "" }, "user_id"},
		{"missing loop", func(p *ProfileContext) { p.LoopID = "" }, "loop_id"},
		{"negative version", func(p *ProfileContext) { p.ProfileVersion = -1 }, "profile_version"},
		{"negative budget", func(p *ProfileContext) { p.TokenBudget = -1 }, "token_budget"},
		{"negative slice tokens", func(p *ProfileContext) { p.OperatingModel.TokenCount = -1 }, "token_count"},
		{"negative slice entries", func(p *ProfileContext) { p.OperatingModel.EntryCount = -1 }, "entry_count"},
		{"exceeds budget", func(p *ProfileContext) {
			p.TokenBudget = 10
			p.OperatingModel.TokenCount = 9
			p.LessonsLearned.TokenCount = 5
		}, "exceed token_budget"},
		{"entries with empty content", func(p *ProfileContext) {
			p.OperatingModel.EntryCount = 3
			p.OperatingModel.Content = ""
		}, "content is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := validProfileContext()
			tc.mutate(p)
			err := p.Validate()
			if err == nil {
				t.Fatalf("Validate = nil, want error containing %q", tc.wantIn)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("Validate error = %q, want substring %q", err, tc.wantIn)
			}
		})
	}
}

func TestProfileContext_JSONRoundTrip(t *testing.T) {
	p := validProfileContext()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var p2 ProfileContext
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if p2.OperatingModel.Content != p.OperatingModel.Content {
		t.Errorf("content round-trip mismatch")
	}
}

func TestProfileContext_SystemPromptPreamble_OperatingOnly(t *testing.T) {
	p := validProfileContext()
	out := p.SystemPromptPreamble()
	if !strings.Contains(out, "## How this user works") {
		t.Errorf("missing operating-model header: %q", out)
	}
	if strings.Contains(out, "Lessons from prior sessions") {
		t.Errorf("unexpected lessons header: %q", out)
	}
}

func TestProfileContext_SystemPromptPreamble_BothSlices(t *testing.T) {
	p := validProfileContext()
	p.LessonsLearned = ProfileContextSlice{Content: "- prior lesson", TokenCount: 3, EntryCount: 1}
	out := p.SystemPromptPreamble()
	if !strings.Contains(out, "How this user works") {
		t.Errorf("missing operating-model header")
	}
	if !strings.Contains(out, "Lessons from prior sessions") {
		t.Errorf("missing lessons header")
	}
	// Operating model section should appear before lessons.
	if strings.Index(out, "How this user works") > strings.Index(out, "Lessons from prior sessions") {
		t.Error("operating-model section should precede lessons section")
	}
}

func TestProfileContext_SystemPromptPreamble_NormalizesWhitespace(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"trailing newlines", "- foo\n\n\n"},
		{"no trailing newline", "- foo"},
		{"leading+trailing whitespace", "  \n- foo\n  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &ProfileContext{
				UserID: "u", LoopID: "l",
				OperatingModel: ProfileContextSlice{Content: tc.content, EntryCount: 1, TokenCount: 1},
			}
			out := p.SystemPromptPreamble()
			want := "## How this user works\n- foo\n"
			if out != want {
				t.Errorf("preamble = %q, want %q", out, want)
			}
		})
	}
}

func TestProfileContext_HasOperatingModel(t *testing.T) {
	p := validProfileContext()
	if !p.HasOperatingModel() {
		t.Error("HasOperatingModel = false, want true")
	}
	p.OperatingModel.Content = "   \n  "
	if p.HasOperatingModel() {
		t.Error("HasOperatingModel with whitespace = true, want false")
	}
}
