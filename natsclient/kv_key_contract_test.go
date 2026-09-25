package natsclient

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
)

// pinnedNATSGoContractVersion is the nats.go release whose KV key/filter
// grammar the patterns below MIRROR. It is not a "keep deps still" pin — moving
// it is allowed, but only after re-reading upstream's regexes and confirming
// they still match, because our validation would otherwise silently diverge
// from what the client and server actually accept.
//
// v1.48.0 -> v1.52.0 (2026-07-31, gh#790): re-verified against
// jetstream/kv.go:501-503 at v1.52.0. All three upstream patterns are
// BYTE-IDENTICAL to v1.48.0 —
//
//	validBucketRe    ^[a-zA-Z0-9_-]+$
//	validKeyRe       ^[-/_=\.a-zA-Z0-9]+$
//	validSearchKeyRe ^[-/_=\.a-zA-Z0-9*]*[>]?$
//
// so the mirrored patterns are unchanged and this is a pin move, not a grammar
// change. Re-do this comparison on the next bump; a version bump without it
// turns a contract guard into a version-number formality.
const pinnedNATSGoContractVersion = "v1.52.0"

var (
	pinnedLegacyKVKeyPattern     = regexp.MustCompile(`^[-/_=\.a-zA-Z0-9]+$`)
	pinnedCurrentKVKeyPattern    = regexp.MustCompile(`^[-/_=\.a-zA-Z0-9]+$`)
	pinnedLegacyKVFilterPattern  = regexp.MustCompile(`^[-/_=\.a-zA-Z0-9*]*[>]?$`)
	pinnedCurrentKVFilterPattern = regexp.MustCompile(`^[-/_=\.a-zA-Z0-9*]*[>]?$`)
)

func TestValidateKVLiteralToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		reason string
	}{
		{name: "minimum", input: "a"},
		{name: "alphabet", input: "-/_=AZaz09"},
		{name: "maximum bytes", input: strings.Repeat("a", MaxKVLiteralTokenBytes)},
		{name: "empty", reason: KVReasonEmpty},
		{
			name:   "whole bytes precede wildcard",
			input:  strings.Repeat("a", MaxKVLiteralTokenBytes) + "*",
			reason: KVReasonBytes,
		},
		{name: "wildcard star", input: "a*b", reason: KVReasonWildcard},
		{name: "wildcard greater", input: "a>b", reason: KVReasonWildcard},
		{name: "separator precedes alphabet", input: ".!", reason: KVReasonSeparator},
		{name: "alphabet", input: "abc:def", reason: KVReasonAlphabet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKVLiteralToken(tt.input)
			assertKVContractError(t, err, ErrorCodeKVTokenInvalid, tt.reason, false)
		})
	}
}

func TestValidateKVLiteralKey(t *testing.T) {
	t.Parallel()

	boundary := strings.Repeat("a", MaxKVLiteralTokenBytes) + "." +
		strings.Repeat("b", MaxKVLiteralKeyBytes-MaxKVLiteralTokenBytes-1)
	maxTokens := strings.Repeat("a.", MaxKVLiteralKeyTokens-1) + "a"
	overTokens := strings.Repeat("a.", MaxKVLiteralKeyTokens) + "a"

	tests := []struct {
		name       string
		input      string
		reason     string
		tokenIndex int
	}{
		{name: "one token", input: "a"},
		{name: "boundary bytes", input: boundary},
		{name: "maximum tokens", input: maxTokens},
		{name: "empty", reason: KVReasonEmpty},
		{
			name:   "whole bytes precede token faults",
			input:  strings.Repeat("a", MaxKVLiteralKeyBytes) + ".*",
			reason: KVReasonBytes,
		},
		{name: "token count precedes wildcard", input: overTokens + ".*", reason: KVReasonTokens},
		{name: "leading empty", input: ".a", reason: KVReasonEmptyToken, tokenIndex: 0},
		{name: "interior empty", input: "a..b", reason: KVReasonEmptyToken, tokenIndex: 1},
		{name: "trailing empty", input: "a.", reason: KVReasonEmptyToken, tokenIndex: 1},
		{name: "wildcard", input: "a.*", reason: KVReasonWildcard, tokenIndex: 1},
		{
			name:       "token bytes precede alphabet",
			input:      "a." + strings.Repeat("b", MaxKVLiteralTokenBytes) + "!",
			reason:     KVReasonTokenBytes,
			tokenIndex: 1,
		},
		{name: "alphabet", input: "a.b:c", reason: KVReasonAlphabet, tokenIndex: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKVLiteralKey(tt.input)
			assertKVContractError(t, err, ErrorCodeKVKeyInvalid, tt.reason, tt.reason != "")
			if tt.reason != "" && isTokenLocalReason(tt.reason) {
				assertKVDetail(t, err, KVDetailTokenIndex, tt.tokenIndex)
			}
		})
	}
}

func TestValidateKVWildcardFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      string
		reason     string
		tokenIndex int
	}{
		{name: "exact", input: "domain.category.property"},
		{name: "star", input: "domain.*.property"},
		{name: "terminal greater", input: "domain.category.>"},
		{name: "all keys", input: ">"},
		{name: "empty", reason: KVReasonEmpty},
		{name: "empty token", input: "foo..bar", reason: KVReasonEmptyToken, tokenIndex: 1},
		{name: "embedded star", input: "foo*bar", reason: KVReasonWildcard, tokenIndex: 0},
		{name: "embedded greater", input: "foo>", reason: KVReasonWildcard, tokenIndex: 0},
		{name: "misplaced greater", input: "foo.>.bar", reason: KVReasonPosition, tokenIndex: 1},
		{name: "alphabet", input: "foo.:bar", reason: KVReasonAlphabet, tokenIndex: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKVWildcardFilter(tt.input)
			assertKVContractError(t, err, ErrorCodeKVFilterInvalid, tt.reason, tt.reason != "")
			if tt.reason != "" && isTokenLocalReason(tt.reason) {
				assertKVDetail(t, err, KVDetailTokenIndex, tt.tokenIndex)
			}
		})
	}
}

func TestKVContractBudgets(t *testing.T) {
	t.Parallel()

	maxFilterBytes := strings.Repeat("a", MaxKVLiteralTokenBytes) + "." +
		strings.Repeat("b", MaxKVWildcardFilterBytes-MaxKVLiteralTokenBytes-1)
	maxFilterTokens := strings.Repeat("a.", MaxKVWildcardFilterTokens-1) + "*"
	overFilterTokens := strings.Repeat("a.", MaxKVWildcardFilterTokens) + "*"

	if err := ValidateKVWildcardFilter(maxFilterBytes); err != nil {
		t.Fatalf("maximum-byte filter: %v", err)
	}
	if err := ValidateKVWildcardFilter(maxFilterTokens); err != nil {
		t.Fatalf("maximum-token filter: %v", err)
	}
	assertKVContractError(t,
		ValidateKVWildcardFilter(maxFilterBytes+"a"),
		ErrorCodeKVFilterInvalid,
		KVReasonBytes,
		true,
	)
	assertKVContractError(t,
		ValidateKVWildcardFilter(overFilterTokens),
		ErrorCodeKVFilterInvalid,
		KVReasonTokens,
		true,
	)
}

// TestKVContractPinnedSDKAcceptanceMatrix records the local validators in both
// nats.go v1.48.0 KV implementations. The real-NATS integration test proves the
// accepted cases through both APIs; this table locks intentional strictness.
func TestKVContractPinnedSDKAcceptanceMatrix(t *testing.T) {
	t.Parallel()

	acceptedKeys := []string{"a", "-/_=AZaz09", "domain.category.property"}
	for _, key := range acceptedKeys {
		if err := ValidateKVLiteralKey(key); err != nil {
			t.Fatalf("contract fixture %q: %v", key, err)
		}
		if !pinnedLegacyKVKeyPattern.MatchString(key) || !pinnedCurrentKVKeyPattern.MatchString(key) {
			t.Fatalf("accepted key %q is outside pinned SDK matrix", key)
		}
	}
	acceptedFilters := []string{"domain.category.property", "domain.*.property", "domain.category.>"}
	for _, filter := range acceptedFilters {
		if err := ValidateKVWildcardFilter(filter); err != nil {
			t.Fatalf("contract fixture %q: %v", filter, err)
		}
		if !pinnedLegacyKVFilterPattern.MatchString(filter) || !pinnedCurrentKVFilterPattern.MatchString(filter) {
			t.Fatalf("accepted filter %q is outside pinned SDK matrix", filter)
		}
	}
	intentionalStrictness := []string{"foo..bar", "foo*bar", "foo>"}
	for _, filter := range intentionalStrictness {
		if err := ValidateKVWildcardFilter(filter); err == nil {
			t.Fatalf("contract unexpectedly accepted unsafe SDK shape %q", filter)
		}
		if !pinnedLegacyKVFilterPattern.MatchString(filter) || !pinnedCurrentKVFilterPattern.MatchString(filter) {
			t.Fatalf("fixture %q no longer records SDK-only acceptance", filter)
		}
	}
}

func TestKVOpaqueTokenCodec(t *testing.T) {
	t.Parallel()

	tests := [][]byte{
		nil,
		{},
		{0},
		[]byte("alias.with spaces:\x00"),
		bytes.Repeat([]byte{0xff}, MaxKVOpaqueTokenInputBytes),
	}
	seen := make(map[string][]byte)
	for _, input := range tests {
		token, err := EncodeKVOpaqueToken(input)
		if err != nil {
			t.Fatalf("EncodeKVOpaqueToken(%d bytes): %v", len(input), err)
		}
		if err := ValidateKVLiteralToken(token); err != nil {
			t.Fatalf("encoded token %q is not literal: %v", token, err)
		}
		decoded, err := DecodeKVOpaqueToken(token)
		if err != nil {
			t.Fatalf("DecodeKVOpaqueToken(%q): %v", token, err)
		}
		if !bytes.Equal(decoded, input) {
			t.Fatalf("round trip mismatch: got %x want %x", decoded, input)
		}
		reencoded, err := EncodeKVOpaqueToken(decoded)
		if err != nil || reencoded != token {
			t.Fatalf("canonical re-encode = %q, %v; want %q", reencoded, err, token)
		}
		if prior, ok := seen[token]; ok && !bytes.Equal(prior, input) {
			t.Fatalf("collision: %x and %x encode as %q", prior, input, token)
		}
		seen[token] = append([]byte(nil), input...)
	}

	_, err := EncodeKVOpaqueToken(bytes.Repeat([]byte{'a'}, MaxKVOpaqueTokenInputBytes+1))
	assertKVContractError(t, err, ErrorCodeKVTokenEncodeInvalid, KVReasonBytes, true)
}

func TestDecodeKVOpaqueTokenPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		reason string
	}{
		{name: "empty", reason: KVReasonEmpty},
		{name: "bytes before version", input: strings.Repeat("z", MaxKVOpaqueTokenBytes+1), reason: KVReasonBytes},
		{name: "version", input: "x2_00", reason: KVReasonVersion},
		{name: "short version", input: "x1", reason: KVReasonVersion},
		{name: "odd hex", input: "x1_0", reason: KVReasonHex},
		{name: "invalid hex before uppercase", input: "x1_Ag", reason: KVReasonHex},
		{name: "uppercase noncanonical", input: "x1_AF", reason: KVReasonNoncanonical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeKVOpaqueToken(tt.input)
			assertKVContractError(t, err, ErrorCodeKVTokenDecodeInvalid, tt.reason, true)
		})
	}
}

func TestKVContractErrorsDoNotEchoInput(t *testing.T) {
	t.Parallel()

	secret := "secret:value"
	err := ValidateKVLiteralToken(secret)
	if err == nil {
		t.Fatal("expected invalid token")
	}
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) {
		t.Fatalf("error type = %T, want *errs.ClassifiedError", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error message echoed raw input: %q", err)
	}
	for key, value := range classified.Detail {
		if strings.Contains(key, secret) || strings.Contains(toString(value), secret) {
			t.Fatalf("error detail echoed raw input: %v", classified.Detail)
		}
	}
}

func assertKVContractError(t *testing.T, err error, code, reason string, wantErr bool) {
	t.Helper()
	if !wantErr && reason == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected %s/%s error", code, reason)
	}
	if !errs.IsInvalid(err) || errs.IsTransient(err) {
		t.Fatalf("classification: invalid=%v transient=%v", errs.IsInvalid(err), errs.IsTransient(err))
	}
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) {
		t.Fatalf("error type = %T, want *errs.ClassifiedError", err)
	}
	if classified.Code != code {
		t.Fatalf("code = %q, want %q", classified.Code, code)
	}
	if got := classified.Detail[KVDetailReason]; got != reason {
		t.Fatalf("reason = %v, want %q; detail=%v", got, reason, classified.Detail)
	}
	switch reason {
	case KVReasonBytes, KVReasonTokenBytes:
		if _, ok := classified.Detail[KVDetailMeasuredBytes]; !ok {
			t.Fatalf("missing %q: %v", KVDetailMeasuredBytes, classified.Detail)
		}
		if _, ok := classified.Detail[KVDetailAllowedBytes]; !ok {
			t.Fatalf("missing %q: %v", KVDetailAllowedBytes, classified.Detail)
		}
	case KVReasonTokens:
		if _, ok := classified.Detail[KVDetailMeasuredTokens]; !ok {
			t.Fatalf("missing %q: %v", KVDetailMeasuredTokens, classified.Detail)
		}
		if _, ok := classified.Detail[KVDetailAllowedTokens]; !ok {
			t.Fatalf("missing %q: %v", KVDetailAllowedTokens, classified.Detail)
		}
	}
}

func assertKVDetail(t *testing.T, err error, key string, want any) {
	t.Helper()
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) {
		t.Fatalf("error type = %T, want *errs.ClassifiedError", err)
	}
	if got := classified.Detail[key]; got != want {
		t.Fatalf("detail[%q] = %v, want %v", key, got, want)
	}
}

func isTokenLocalReason(reason string) bool {
	switch reason {
	case KVReasonEmptyToken, KVReasonTokenBytes, KVReasonWildcard, KVReasonPosition, KVReasonAlphabet:
		return true
	default:
		return false
	}
}

func toString(value any) string {
	if stringValue, ok := value.(string); ok {
		return stringValue
	}
	return ""
}

func FuzzKVValidatorsNeverPanic(f *testing.F) {
	for _, seed := range []string{"", "a", "a.b", "a..b", "*", ">", "x1_00", "\x00"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		_ = ValidateKVLiteralToken(input)
		if err := ValidateKVLiteralKey(input); err == nil {
			if !pinnedLegacyKVKeyPattern.MatchString(input) || !pinnedCurrentKVKeyPattern.MatchString(input) {
				t.Fatalf("accepted key is outside pinned SDK grammar")
			}
		}
		if err := ValidateKVWildcardFilter(input); err == nil {
			if !pinnedLegacyKVFilterPattern.MatchString(input) ||
				!pinnedCurrentKVFilterPattern.MatchString(input) {
				t.Fatalf("accepted filter is outside pinned SDK grammar")
			}
		}
		_, _ = DecodeKVOpaqueToken(input)
	})
}

func FuzzKVOpaqueTokenRoundTrip(f *testing.F) {
	for _, seed := range [][]byte{nil, {}, {0}, []byte("alias.example"), bytes.Repeat([]byte{0xff}, 254)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > MaxKVOpaqueTokenInputBytes {
			return
		}
		token, err := EncodeKVOpaqueToken(input)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		decoded, err := DecodeKVOpaqueToken(token)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !bytes.Equal(decoded, input) {
			t.Fatalf("round trip: got %x want %x", decoded, input)
		}
	})
}
