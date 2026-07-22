package embedding

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateAtWord(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		maxLen int
		want   string
	}{
		{
			name:   "no truncation needed",
			text:   "short text",
			maxLen: 100,
			want:   "short text",
		},
		{
			name:   "exact length",
			text:   "exact",
			maxLen: 5,
			want:   "exact",
		},
		{
			name:   "truncates at word boundary",
			text:   "hello world this is a test",
			maxLen: 15,
			want:   "hello world",
		},
		{
			name:   "no spaces falls back to hard cut",
			text:   "abcdefghijklmnop",
			maxLen: 10,
			want:   "abcdefghij",
		},
		{
			name:   "space too early uses hard cut",
			text:   "a bcdefghijklmnop",
			maxLen: 10,
			// Space at index 1 is before maxLen/2 (5), so hard cut
			want: "a bcdefghi",
		},
		{
			name:   "single word longer than max",
			text:   "superlongword more text",
			maxLen: 5,
			want:   "super",
		},
		{
			name:   "empty text",
			text:   "",
			maxLen: 10,
			want:   "",
		},
		{
			name:   "realistic document truncation",
			text:   strings.Repeat("word ", 1000), // 5000 chars
			maxLen: 100,
			want:   strings.TrimRight(strings.Repeat("word ", 20), " "), // 20 words = 99 chars
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateAtWord(tt.text, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateAtWord(%q, %d) = %q, want %q", tt.text, tt.maxLen, got, tt.want)
			}
			// Always verify result doesn't exceed maxLen
			if len(got) > tt.maxLen {
				t.Errorf("result length %d exceeds maxLen %d", len(got), tt.maxLen)
			}
		})
	}
}

func TestGetSourceText_Truncation(t *testing.T) {
	w := &Worker{
		maxSourceTextLen: 20,
	}

	record := &Record{
		SourceText: "this is a longer text that should be truncated",
	}

	text, err := w.getSourceText(record)
	if err != nil {
		t.Fatalf("getSourceText error: %v", err)
	}

	if len(text) > 20 {
		t.Errorf("text length %d exceeds max %d: %q", len(text), 20, text)
	}
}

func TestGetSourceText_NoTruncation_WhenZero(t *testing.T) {
	w := &Worker{
		maxSourceTextLen: 0, // disabled
	}

	longText := strings.Repeat("word ", 500)
	record := &Record{
		SourceText: longText,
	}

	text, err := w.getSourceText(record)
	if err != nil {
		t.Fatalf("getSourceText error: %v", err)
	}

	if text != longText {
		t.Error("text should not be truncated when maxSourceTextLen is 0")
	}
}

// --- StreamableStore / fetchTextFromStorage tests ---

type mockStreamableStore struct {
	data      map[string][]byte
	openCalls int
}

func (m *mockStreamableStore) Put(_ context.Context, key string, data []byte) error {
	m.data[key] = data
	return nil
}

func (m *mockStreamableStore) Get(_ context.Context, key string) ([]byte, error) {
	d, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return d, nil
}

func (m *mockStreamableStore) List(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (m *mockStreamableStore) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockStreamableStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	m.openCalls++
	d, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return io.NopCloser(bytes.NewReader(d)), nil
}

func TestFetchTextFromStorage_StreamsLimitedBytes(t *testing.T) {
	store := &mockStreamableStore{
		data: map[string][]byte{
			"doc/safety-001": []byte(strings.Repeat("safety content here ", 500)), // 10000 chars
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := &Worker{
		contentStore:     store,
		maxSourceTextLen: 100,
		ctx:              ctx,
	}

	text, err := w.fetchTextFromStorage(&StorageRef{Key: "doc/safety-001"})
	if err != nil {
		t.Fatalf("fetchTextFromStorage error: %v", err)
	}

	// The offloaded lane now truncates through the shared rune-safe routine, so it
	// honors the word boundary (the old byte-cut returned exactly 100). The cap is in
	// CHARACTERS, so at most 100 runes, and never more than the cap.
	if got := utf8.RuneCountInString(text); got > 100 {
		t.Errorf("expected at most 100 runes, got %d", got)
	}
	// It must not have loaded the whole 10000-char body into memory.
	if len(text) >= 10000 {
		t.Errorf("read the entire body (%d bytes); the read must be bounded", len(text))
	}
	// This ASCII body repeats "safety content here " (20 chars), so the 100-rune cut
	// lands on the trailing space at index 99 and the word boundary drops it → 99 chars.
	if want := strings.TrimRight(strings.Repeat("safety content here ", 5), " "); text != want {
		t.Errorf("expected word-boundary truncation %q, got %q", want, text)
	}
	if store.openCalls != 1 {
		t.Errorf("expected 1 Open() call, got %d", store.openCalls)
	}
}

func TestFetchTextFromStorage_ShortContent(t *testing.T) {
	store := &mockStreamableStore{
		data: map[string][]byte{
			"doc/short": []byte("brief"),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := &Worker{
		contentStore:     store,
		maxSourceTextLen: 4000,
		ctx:              ctx,
	}

	text, err := w.fetchTextFromStorage(&StorageRef{Key: "doc/short"})
	if err != nil {
		t.Fatalf("fetchTextFromStorage error: %v", err)
	}

	if text != "brief" {
		t.Errorf("expected 'brief', got %q", text)
	}
}

func TestFetchTextFromStorage_NilStore(t *testing.T) {
	w := &Worker{
		contentStore:     nil,
		maxSourceTextLen: 4000,
	}

	_, err := w.fetchTextFromStorage(&StorageRef{Key: "doc/any"})
	if err == nil {
		t.Error("expected error when content store is nil")
	}
}

func TestFetchTextFromStorage_KeyNotFound(t *testing.T) {
	store := &mockStreamableStore{
		data: map[string][]byte{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := &Worker{
		contentStore:     store,
		maxSourceTextLen: 4000,
		ctx:              ctx,
	}

	_, err := w.fetchTextFromStorage(&StorageRef{Key: "doc/missing"})
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestGetSourceText_StorageRef_UsesStreaming(t *testing.T) {
	store := &mockStreamableStore{
		data: map[string][]byte{
			"doc/safety": []byte("full body text from object store"),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := &Worker{
		contentStore:     store,
		maxSourceTextLen: 4000,
		ctx:              ctx,
	}

	record := &Record{
		StorageRef: &StorageRef{Key: "doc/safety"},
	}

	text, err := w.getSourceText(record)
	if err != nil {
		t.Fatalf("getSourceText error: %v", err)
	}

	if text != "full body text from object store" {
		t.Errorf("expected body text, got %q", text)
	}
	if store.openCalls != 1 {
		t.Errorf("expected streaming path (Open), got %d calls", store.openCalls)
	}
}

func TestGetSourceText_SourceText_TakesPrecedence(t *testing.T) {
	store := &mockStreamableStore{
		data: map[string][]byte{
			"doc/safety": []byte("store content"),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := &Worker{
		contentStore:     store,
		maxSourceTextLen: 4000,
		ctx:              ctx,
	}

	record := &Record{
		SourceText: "triple-based text",
		StorageRef: &StorageRef{Key: "doc/safety"},
	}

	text, err := w.getSourceText(record)
	if err != nil {
		t.Fatalf("getSourceText error: %v", err)
	}

	if text != "triple-based text" {
		t.Errorf("SourceText should take precedence, got %q", text)
	}
	if store.openCalls != 0 {
		t.Errorf("should NOT call Open when SourceText is present, got %d calls", store.openCalls)
	}
}
