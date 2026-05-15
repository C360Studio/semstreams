package agenticdetonator

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// markdownImageOpenPattern matches the opening of a markdown image
// syntax `![alt](`. We capture only up to the opening paren and then
// hand off to extractBalancedURL so URLs containing parens — common
// in Wikipedia-style links and any URL with query-string parens —
// don't truncate at the first inner `)`.
var markdownImageOpenPattern = regexp.MustCompile(`!\[[^\]]*\]\(`)

// markdownLinkOpenPattern is the link-syntax analogue. The leading
// `[^!]` lookahead-substitute prevents matching the image syntax
// twice. Captured by extractBalancedURL.
var markdownLinkOpenPattern = regexp.MustCompile(`(^|[^!])\[[^\]]*\]\(`)

// bareURLPattern is a permissive http(s) URL matcher for raw text
// that the LLM emitted outside markdown syntax. (?i) flag handles
// HTTPS:// (RFC 3986 schemes are case-insensitive). Callers treat
// results as "candidate exfil URL, needs review" rather than
// confirmed. Trailing punctuation is stripped post-match.
var bareURLPattern = regexp.MustCompile(`(?i)https?://[^\s)<>"']+`)

// trailingPunctuationCutset is the set of characters stripped from
// the tail of a bare URL match. Common sentence-trailing chars that
// regex greedy capture pulls into the URL string.
const trailingPunctuationCutset = `.,;!?)]}>`

// ScannedURL records one URL the scanner observed along with the
// markdown form that surfaced it. Provenance lets the Phase 3b
// canary aggregator weigh image-syntax matches more heavily than
// bare URLs (image markdown is auto-rendered → auto-fetched).
type ScannedURL struct {
	URL  string
	Form URLForm
}

// URLForm distinguishes how the URL appeared in the canary text.
// Image-syntax URLs are the primary exfil vector; link-syntax and
// bare URLs are weaker signals but still recorded so operators
// can audit the full surface.
type URLForm string

// URL-form constants enumerate how a URL appeared in canary text.
const (
	URLFormImage URLForm = "image"
	URLFormLink  URLForm = "link"
	URLFormBare  URLForm = "bare"
)

// scanHit records one regex-pass observation: where the URL fell
// in the source text, the URL string, and the markdown form it
// surfaced as.
type scanHit struct {
	pos  int
	url  string
	form URLForm
}

// ScanMarkdownURLs returns the URLs the canary text reveals in
// text-position order with their markdown form recorded.
// Deduplicates by URL — if the same URL appears both as an image
// and as a bare reference, the stronger form (image > link > bare)
// is retained; text position of the first observation wins.
//
// Pure function — no I/O, no goroutines, no shared state. Safe to
// call from anywhere with any input. Caller decides what to do
// with a populated result; the scanner is signal-extraction only.
func ScanMarkdownURLs(text string) []ScannedURL {
	if text == "" {
		return nil
	}

	hits := collectURLHits(text)
	return dedupAndOrder(hits)
}

// collectURLHits walks the text through three regex passes (image,
// link, bare) with byte-range masking so the permissive bare-URL
// pass doesn't re-find URLs already extracted by the balanced-paren
// markdown passes.
func collectURLHits(text string) []scanHit {
	type rangeSpan struct{ start, end int }
	var consumed []rangeSpan
	overlapsConsumed := func(pos int) bool {
		for _, r := range consumed {
			if pos >= r.start && pos < r.end {
				return true
			}
		}
		return false
	}

	var hits []scanHit

	// Image and link openings — for each, walk forward from the
	// opening `(` and extract the URL with balanced-paren handling
	// so URLs containing `()` (Wikipedia-style, query-string parens)
	// don't truncate. Index past the opening `(` is the first URL
	// character.
	collectMarkdown := func(re *regexp.Regexp, form URLForm) {
		for _, m := range re.FindAllStringIndex(text, -1) {
			start := m[1]
			url, ok := extractBalancedURL(text, start)
			if !ok {
				continue
			}
			hits = append(hits, scanHit{pos: start, url: url, form: form})
			consumed = append(consumed, rangeSpan{start: start, end: start + len(url)})
		}
	}
	collectMarkdown(markdownImageOpenPattern, URLFormImage)
	collectMarkdown(markdownLinkOpenPattern, URLFormLink)

	for _, m := range bareURLPattern.FindAllStringIndex(text, -1) {
		if overlapsConsumed(m[0]) {
			continue
		}
		clean := strings.TrimRight(strings.TrimSpace(text[m[0]:m[1]]), trailingPunctuationCutset)
		if !looksLikeURL(clean) {
			continue
		}
		hits = append(hits, scanHit{pos: m[0], url: clean, form: URLFormBare})
	}
	return hits
}

// dedupAndOrder collapses hits to one entry per URL (strongest form
// wins, earliest text position wins) and returns the result in
// text-position order.
func dedupAndOrder(hits []scanHit) []ScannedURL {
	strength := map[URLForm]int{
		URLFormImage: 3,
		URLFormLink:  2,
		URLFormBare:  1,
	}

	// Sort by text position so dedup sees observations in reading
	// order; first observation establishes the position.
	sort.Slice(hits, func(i, j int) bool { return hits[i].pos < hits[j].pos })

	type entry struct {
		url  string
		form URLForm
		pos  int
	}
	byURL := make(map[string]entry, len(hits))
	for _, h := range hits {
		if h.url == "" {
			continue
		}
		if prior, ok := byURL[h.url]; ok {
			if strength[h.form] > strength[prior.form] {
				prior.form = h.form
				byURL[h.url] = prior
			}
			continue
		}
		byURL[h.url] = entry{url: h.url, form: h.form, pos: h.pos}
	}

	entries := make([]entry, 0, len(byURL))
	for _, e := range byURL {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].pos < entries[j].pos })

	out := make([]ScannedURL, 0, len(entries))
	for _, e := range entries {
		out = append(out, ScannedURL{URL: e.url, Form: e.form})
	}
	return out
}

// extractBalancedURL walks from `start` through the text and returns
// the URL string up to the matching `)` that closes the markdown
// syntax, handling balanced inner `()` pairs. Returns (url, true)
// on success or ("", false) when the opening paren is never closed
// (truncated input).
//
// Whitespace inside the URL terminates extraction — markdown does
// not permit unescaped whitespace in a URL, and a real-world canary
// emitting `![](http://x.example /foo)` is almost certainly an LLM
// confusion artifact, not a defensible URL.
//
// Angle-bracket variant `<https://example.com/foo(bar)>` is
// supported: when start points at `<`, we extract through the
// matching `>` instead, then trim the brackets.
func extractBalancedURL(text string, start int) (string, bool) {
	if start >= len(text) {
		return "", false
	}

	// Angle-bracket form: `<URL>` inside the markdown parens.
	if text[start] == '<' {
		end := strings.IndexByte(text[start+1:], '>')
		if end < 0 {
			return "", false
		}
		// Confirm a closing `)` follows the `>`, allowing only
		// whitespace between them.
		after := strings.TrimLeft(text[start+1+end+1:], " \t")
		if !strings.HasPrefix(after, ")") {
			return "", false
		}
		inner := strings.TrimSpace(text[start+1 : start+1+end])
		return inner, inner != ""
	}

	depth := 1
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				url := strings.TrimSpace(text[start:i])
				return url, url != ""
			}
		case ' ', '\t', '\n', '\r':
			return "", false
		}
	}
	return "", false
}

// looksLikeURL is a cheap shape check used by callers that want to
// drop garbage captures before persisting. Validates that the
// extracted string parses as an absolute http/https URL via the
// stdlib parser. Exported because the Phase 3b canary aggregator
// surfaces ScannedURL.URL into the audit record and operators
// should not see malformed entries.
func looksLikeURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return (scheme == "http" || scheme == "https") && u.Host != ""
}

// SignalFromScannedURLs returns SignalNetworkEgress when the
// scanner found any URL the canary surfaced. Empty input → "" so
// the caller distinguishes "scanned and found nothing" from
// "scanner contributed a signal."
//
// This is intentionally coarse for Phase 3a — every observed URL
// is treated as potential exfil. Phase 3b+ can refine (allowlist
// known-safe domains, weight by form, etc.). Operators tune via
// the corpus, not via the scanner heuristic.
func SignalFromScannedURLs(urls []ScannedURL) string {
	if len(urls) == 0 {
		return ""
	}
	return SignalNetworkEgress
}
