package natsclient

import (
	"encoding/hex"
	"errors"
	"strings"

	"github.com/c360studio/semstreams/pkg/errs"
)

const (
	// MaxKVLiteralTokenBytes is the SemStreams byte budget for one literal KV token.
	MaxKVLiteralTokenBytes = 512
	// MaxKVLiteralKeyBytes is the SemStreams byte budget for one complete literal KV key.
	MaxKVLiteralKeyBytes = 1024
	// MaxKVLiteralKeyTokens is the SemStreams arity budget for one complete literal KV key.
	MaxKVLiteralKeyTokens = 64
	// MaxKVWildcardFilterBytes is the SemStreams byte budget for one complete KV filter.
	MaxKVWildcardFilterBytes = 1024
	// MaxKVWildcardFilterTokens is the SemStreams arity budget for one complete KV filter.
	MaxKVWildcardFilterTokens = 64
	// MaxKVOpaqueTokenInputBytes is the largest byte string accepted by the v1 opaque codec.
	MaxKVOpaqueTokenInputBytes = 254
	// MaxKVOpaqueTokenBytes is the largest token produced or accepted by the v1 opaque codec.
	MaxKVOpaqueTokenBytes = 511
)

const (
	// ErrorCodeKVTokenInvalid classifies invalid literal KV tokens.
	ErrorCodeKVTokenInvalid = "kv_token_invalid"
	// ErrorCodeKVKeyInvalid classifies invalid literal KV keys.
	ErrorCodeKVKeyInvalid = "kv_key_invalid"
	// ErrorCodeKVFilterInvalid classifies invalid KV wildcard filters.
	ErrorCodeKVFilterInvalid = "kv_filter_invalid"
	// ErrorCodeKVTokenEncodeInvalid classifies unsupported opaque-token input.
	ErrorCodeKVTokenEncodeInvalid = "kv_token_encode_invalid"
	// ErrorCodeKVTokenDecodeInvalid classifies invalid opaque-token encodings.
	ErrorCodeKVTokenDecodeInvalid = "kv_token_decode_invalid"
)

const (
	// KVReasonEmpty identifies an empty whole input.
	KVReasonEmpty = "empty"
	// KVReasonBytes identifies a whole-input byte-budget violation.
	KVReasonBytes = "bytes"
	// KVReasonTokens identifies a whole-input token-count violation.
	KVReasonTokens = "tokens"
	// KVReasonEmptyToken identifies an empty position in a key or filter.
	KVReasonEmptyToken = "empty_token"
	// KVReasonTokenBytes identifies a per-token byte-budget violation.
	KVReasonTokenBytes = "token_bytes"
	// KVReasonSeparator identifies a separator in a literal token.
	KVReasonSeparator = "separator"
	// KVReasonWildcard identifies a wildcard in a literal position.
	KVReasonWildcard = "wildcard"
	// KVReasonPosition identifies a wildcard in a forbidden position.
	KVReasonPosition = "position"
	// KVReasonAlphabet identifies a byte outside the literal alphabet.
	KVReasonAlphabet = "alphabet"
	// KVReasonVersion identifies an unknown opaque codec version.
	KVReasonVersion = "version"
	// KVReasonHex identifies malformed opaque hexadecimal.
	KVReasonHex = "hex"
	// KVReasonNoncanonical identifies valid but non-canonical opaque hexadecimal.
	KVReasonNoncanonical = "noncanonical"
)

const (
	// KVDetailReason is the mandatory stable reason detail key.
	KVDetailReason = "reason"
	// KVDetailMeasuredBytes reports the rejected byte count.
	KVDetailMeasuredBytes = "measured_bytes"
	// KVDetailAllowedBytes reports the applicable byte budget.
	KVDetailAllowedBytes = "allowed_bytes"
	// KVDetailMeasuredTokens reports the rejected token count.
	KVDetailMeasuredTokens = "measured_tokens"
	// KVDetailAllowedTokens reports the applicable token-count budget.
	KVDetailAllowedTokens = "allowed_tokens"
	// KVDetailTokenIndex reports the zero-based failing position.
	KVDetailTokenIndex = "token_index"
)

const opaqueKVTokenPrefix = "x1_"

var errInvalidKVContractInput = errors.New("invalid NATS KV contract input")

// ValidateKVLiteralToken validates one literal NATS KV key position. It never
// rewrites, normalizes, or encodes the supplied token.
func ValidateKVLiteralToken(token string) error {
	if token == "" {
		return newKVContractError(ErrorCodeKVTokenInvalid, KVReasonEmpty, nil)
	}
	if len(token) > MaxKVLiteralTokenBytes {
		return newKVContractError(ErrorCodeKVTokenInvalid, KVReasonBytes, map[string]any{
			KVDetailMeasuredBytes: len(token),
			KVDetailAllowedBytes:  MaxKVLiteralTokenBytes,
		})
	}
	if strings.ContainsAny(token, "*>") {
		return newKVContractError(ErrorCodeKVTokenInvalid, KVReasonWildcard, nil)
	}
	if strings.Contains(token, ".") {
		return newKVContractError(ErrorCodeKVTokenInvalid, KVReasonSeparator, nil)
	}
	if !isKVLiteralAlphabet(token) {
		return newKVContractError(ErrorCodeKVTokenInvalid, KVReasonAlphabet, nil)
	}
	return nil
}

// ValidateKVLiteralKey validates a complete dot-separated literal KV key. It
// rejects wildcards and empty positions and never changes key identity.
func ValidateKVLiteralKey(key string) error {
	if key == "" {
		return newKVContractError(ErrorCodeKVKeyInvalid, KVReasonEmpty, nil)
	}
	if len(key) > MaxKVLiteralKeyBytes {
		return newKVContractError(ErrorCodeKVKeyInvalid, KVReasonBytes, map[string]any{
			KVDetailMeasuredBytes: len(key),
			KVDetailAllowedBytes:  MaxKVLiteralKeyBytes,
		})
	}
	tokens := strings.Split(key, ".")
	if len(tokens) > MaxKVLiteralKeyTokens {
		return newKVContractError(ErrorCodeKVKeyInvalid, KVReasonTokens, map[string]any{
			KVDetailMeasuredTokens: len(tokens),
			KVDetailAllowedTokens:  MaxKVLiteralKeyTokens,
		})
	}
	for index, token := range tokens {
		if token == "" {
			return newKVTokenLocalError(ErrorCodeKVKeyInvalid, KVReasonEmptyToken, index, nil)
		}
		if strings.ContainsAny(token, "*>") {
			return newKVTokenLocalError(ErrorCodeKVKeyInvalid, KVReasonWildcard, index, nil)
		}
		if len(token) > MaxKVLiteralTokenBytes {
			return newKVTokenLocalError(ErrorCodeKVKeyInvalid, KVReasonTokenBytes, index, map[string]any{
				KVDetailMeasuredBytes: len(token),
				KVDetailAllowedBytes:  MaxKVLiteralTokenBytes,
			})
		}
		if !isKVLiteralAlphabet(token) {
			return newKVTokenLocalError(ErrorCodeKVKeyInvalid, KVReasonAlphabet, index, nil)
		}
	}
	return nil
}

// ValidateKVWildcardFilter validates a complete KV search filter. Wildcards
// are accepted only as a complete '*' token or a complete final '>' token.
func ValidateKVWildcardFilter(filter string) error {
	if filter == "" {
		return newKVContractError(ErrorCodeKVFilterInvalid, KVReasonEmpty, nil)
	}
	if len(filter) > MaxKVWildcardFilterBytes {
		return newKVContractError(ErrorCodeKVFilterInvalid, KVReasonBytes, map[string]any{
			KVDetailMeasuredBytes: len(filter),
			KVDetailAllowedBytes:  MaxKVWildcardFilterBytes,
		})
	}
	tokens := strings.Split(filter, ".")
	if len(tokens) > MaxKVWildcardFilterTokens {
		return newKVContractError(ErrorCodeKVFilterInvalid, KVReasonTokens, map[string]any{
			KVDetailMeasuredTokens: len(tokens),
			KVDetailAllowedTokens:  MaxKVWildcardFilterTokens,
		})
	}
	for index, token := range tokens {
		if token == "" {
			return newKVTokenLocalError(ErrorCodeKVFilterInvalid, KVReasonEmptyToken, index, nil)
		}
		if token == ">" {
			if index != len(tokens)-1 {
				return newKVTokenLocalError(ErrorCodeKVFilterInvalid, KVReasonPosition, index, nil)
			}
			continue
		}
		if token == "*" {
			continue
		}
		if strings.ContainsAny(token, "*>") {
			return newKVTokenLocalError(ErrorCodeKVFilterInvalid, KVReasonWildcard, index, nil)
		}
		if len(token) > MaxKVLiteralTokenBytes {
			return newKVTokenLocalError(ErrorCodeKVFilterInvalid, KVReasonTokenBytes, index, map[string]any{
				KVDetailMeasuredBytes: len(token),
				KVDetailAllowedBytes:  MaxKVLiteralTokenBytes,
			})
		}
		if !isKVLiteralAlphabet(token) {
			return newKVTokenLocalError(ErrorCodeKVFilterInvalid, KVReasonAlphabet, index, nil)
		}
	}
	return nil
}

// EncodeKVOpaqueToken encodes arbitrary bytes as one canonical v1 literal KV
// token. Callers must explicitly choose opaque storage for their key axis.
func EncodeKVOpaqueToken(raw []byte) (string, error) {
	if len(raw) > MaxKVOpaqueTokenInputBytes {
		return "", newKVContractError(ErrorCodeKVTokenEncodeInvalid, KVReasonBytes, map[string]any{
			KVDetailMeasuredBytes: len(raw),
			KVDetailAllowedBytes:  MaxKVOpaqueTokenInputBytes,
		})
	}
	encoded := make([]byte, len(opaqueKVTokenPrefix)+hex.EncodedLen(len(raw)))
	copy(encoded, opaqueKVTokenPrefix)
	hex.Encode(encoded[len(opaqueKVTokenPrefix):], raw)
	return string(encoded), nil
}

// DecodeKVOpaqueToken decodes one canonical v1 opaque token. It rejects unknown
// versions, malformed hexadecimal, and uppercase non-canonical forms.
func DecodeKVOpaqueToken(token string) ([]byte, error) {
	if token == "" {
		return nil, newKVContractError(ErrorCodeKVTokenDecodeInvalid, KVReasonEmpty, nil)
	}
	if len(token) > MaxKVOpaqueTokenBytes {
		return nil, newKVContractError(ErrorCodeKVTokenDecodeInvalid, KVReasonBytes, map[string]any{
			KVDetailMeasuredBytes: len(token),
			KVDetailAllowedBytes:  MaxKVOpaqueTokenBytes,
		})
	}
	if !strings.HasPrefix(token, opaqueKVTokenPrefix) {
		return nil, newKVContractError(ErrorCodeKVTokenDecodeInvalid, KVReasonVersion, nil)
	}
	hexText := token[len(opaqueKVTokenPrefix):]
	if len(hexText)%2 != 0 || hasNonHex(hexText) {
		return nil, newKVContractError(ErrorCodeKVTokenDecodeInvalid, KVReasonHex, nil)
	}
	if hasUpperHex(hexText) {
		return nil, newKVContractError(ErrorCodeKVTokenDecodeInvalid, KVReasonNoncanonical, nil)
	}
	decoded := make([]byte, hex.DecodedLen(len(hexText)))
	if _, err := hex.Decode(decoded, []byte(hexText)); err != nil {
		return nil, newKVContractError(ErrorCodeKVTokenDecodeInvalid, KVReasonHex, nil)
	}
	if len(decoded) > MaxKVOpaqueTokenInputBytes {
		return nil, newKVContractError(ErrorCodeKVTokenDecodeInvalid, KVReasonBytes, map[string]any{
			KVDetailMeasuredBytes: len(decoded),
			KVDetailAllowedBytes:  MaxKVOpaqueTokenInputBytes,
		})
	}
	return decoded, nil
}

func isKVLiteralAlphabet(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' {
			continue
		}
		switch character {
		case '-', '/', '_', '=':
			continue
		default:
			return false
		}
	}
	return true
}

func hasNonHex(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= '0' && character <= '9' ||
			character >= 'a' && character <= 'f' ||
			character >= 'A' && character <= 'F' {
			continue
		}
		return true
	}
	return false
}

func hasUpperHex(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] >= 'A' && value[index] <= 'F' {
			return true
		}
	}
	return false
}

func newKVTokenLocalError(code, reason string, tokenIndex int, detail map[string]any) error {
	if detail == nil {
		detail = make(map[string]any, 2)
	}
	detail[KVDetailTokenIndex] = tokenIndex
	return newKVContractError(code, reason, detail)
}

func newKVContractError(code, reason string, detail map[string]any) error {
	if detail == nil {
		detail = make(map[string]any, 1)
	}
	detail[KVDetailReason] = reason
	return errs.ClassifiedCodeDetail(errs.ErrorInvalid, code, detail, errInvalidKVContractInput)
}
