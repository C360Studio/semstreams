package graphindex

import (
	"errors"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphIndexKVContractMatrix(t *testing.T) {
	t.Parallel()

	entityID := "acme.ops.robotics.gcs.drone.001"
	targetID := "acme.ops.robotics.gcs.truck.001"
	maxSegment := "a" + strings.Repeat("b", vocabulary.MaxPredicateSegmentBytes-1)
	maxPredicate := strings.Join([]string{maxSegment, maxSegment, maxSegment}, ".")
	maxPredicateToken := graph.EncodePredicateToken(maxPredicate)
	entityBytes := len(entityID)
	predicateBytes := len(maxPredicate)
	require.Len(t, maxPredicate, vocabulary.MaxPredicateBytes)
	require.Equal(t, 194, len(maxPredicate))
	require.Len(t, maxPredicateToken, 388)
	require.Len(t, targetID, entityBytes)
	_, err := vocabulary.ParsePredicate(maxPredicate)
	require.NoError(t, err)

	nameHash := nameIndexKey("Alpha")
	contextHash := contextHashHex("source.alpha")
	predicateHash := predicateHashHex(maxPredicate)
	alias := "drone.local"

	type filterContract struct {
		value        string
		byteFormula  string
		wantBytes    int
		tokenFormula string
		wantTokens   int
	}
	tests := []struct {
		name            string
		layout          string
		key             string
		keyByteFormula  string
		wantKeyBytes    int
		keyTokenFormula string
		wantKeyTokens   int
		forwardFilters  []filterContract
		ownerFilter     filterContract
	}{
		{
			name:            "predicate current hash",
			layout:          "hash(predicate).entity6",
			key:             predicateIndexKey(maxPredicate, entityID),
			keyByteFormula:  "65+E",
			wantKeyBytes:    65 + entityBytes,
			keyTokenFormula: "1+6",
			wantKeyTokens:   7,
			forwardFilters: []filterContract{{
				value: predicateIndexForwardFilter(maxPredicate), byteFormula: "64+1+11", wantBytes: 76,
				tokenFormula: "1+6", wantTokens: 7,
			}},
			ownerFilter: filterContract{
				value: "*." + entityID, byteFormula: "2+E", wantBytes: entityBytes + 2,
				tokenFormula: "1+6", wantTokens: 7,
			},
		},
		{
			name: "predicate catalog", layout: "predicate3", key: maxPredicate,
			keyByteFormula: "P<=194", wantKeyBytes: predicateBytes,
			keyTokenFormula: "3", wantKeyTokens: 3,
			forwardFilters: []filterContract{
				{value: maxPredicate, byteFormula: "P<=194", wantBytes: 194, tokenFormula: "3", wantTokens: 3},
				{value: maxSegment + "." + maxSegment + ".*", byteFormula: "2S+3", wantBytes: 131,
					tokenFormula: "3", wantTokens: 3},
				{value: maxSegment + ".*.*", byteFormula: "S+4", wantBytes: 68,
					tokenFormula: "3", wantTokens: 3},
			},
		},
		{
			name: "name", layout: "hash(name).entity6.hex(predicate)",
			key:            nameCompositeKey(nameHash, entityID, maxPredicate),
			keyByteFormula: "E+66+2P=E+454", wantKeyBytes: entityBytes + 454,
			keyTokenFormula: "1+6+1", wantKeyTokens: 8,
			forwardFilters: []filterContract{{
				value: nameIndexForwardFilter("Alpha"), byteFormula: "64+1+13", wantBytes: 78,
				tokenFormula: "1+7", wantTokens: 8,
			}},
			ownerFilter: filterContract{
				value: nameIndexEntityFilter(entityID), byteFormula: "E+4", wantBytes: entityBytes + 4,
				tokenFormula: "1+6+1", wantTokens: 8,
			},
		},
		{
			name: "context", layout: "entity6.hash(context).hex(predicate)",
			key:            contextIndexKey(entityID, contextHash, maxPredicate),
			keyByteFormula: "E+66+2P=E+454", wantKeyBytes: entityBytes + 454,
			keyTokenFormula: "6+1+1", wantKeyTokens: 8,
			ownerFilter: filterContract{
				value: contextIndexEntityFilter(entityID), byteFormula: "E+4", wantBytes: entityBytes + 4,
				tokenFormula: "6+1+1", wantTokens: 8,
			},
		},
		{
			name: "incoming", layout: "target6.source6.hex(predicate)",
			key:            incomingIndexKey(targetID, entityID, maxPredicate),
			keyByteFormula: "2E+2+2P=2E+390", wantKeyBytes: 2*entityBytes + 390,
			keyTokenFormula: "6+6+1", wantKeyTokens: 13,
			forwardFilters: []filterContract{{
				value: incomingIndexTargetFilter(targetID), byteFormula: "E+14", wantBytes: entityBytes + 14,
				tokenFormula: "6+7", wantTokens: 13,
			}},
			ownerFilter: filterContract{
				value: incomingIndexSourceFilter(entityID), byteFormula: "E+14", wantBytes: entityBytes + 14,
				tokenFormula: "6+6+1", wantTokens: 13,
			},
		},
		{
			name: "outgoing", layout: "entity6", key: entityID,
			keyByteFormula: "E", wantKeyBytes: entityBytes,
			keyTokenFormula: "6", wantKeyTokens: 6,
			forwardFilters: []filterContract{{value: entityID, byteFormula: "E", wantBytes: entityBytes,
				tokenFormula: "6", wantTokens: 6}},
			ownerFilter: filterContract{value: entityID, byteFormula: "E", wantBytes: entityBytes,
				tokenFormula: "6", wantTokens: 6},
		},
		{
			name: "alias current raw audit", layout: "raw alias -> entityID value", key: alias,
			keyByteFormula: "A (unbounded)", wantKeyBytes: len(alias),
			keyTokenFormula: "variable", wantKeyTokens: 2,
			forwardFilters: []filterContract{{value: alias, byteFormula: "A (unbounded)", wantBytes: len(alias),
				tokenFormula: "variable", wantTokens: 2}},
		},
		{
			name: "predicate raw candidate", layout: "predicate3.entity6",
			key:            rawPredicateCandidateKey(maxPredicate, entityID),
			keyByteFormula: "E+P+1=E+195", wantKeyBytes: entityBytes + 195,
			keyTokenFormula: "3+6", wantKeyTokens: 9,
			forwardFilters: []filterContract{
				{value: rawPredicateCandidateForwardFilters(maxPredicate)[0], byteFormula: "P+12", wantBytes: 206,
					tokenFormula: "3+6", wantTokens: 9},
				{value: rawPredicateCandidateForwardFilters(maxPredicate)[1], byteFormula: "2S+15", wantBytes: 143,
					tokenFormula: "3+6", wantTokens: 9},
				{value: rawPredicateCandidateForwardFilters(maxPredicate)[2], byteFormula: "S+16", wantBytes: 80,
					tokenFormula: "3+6", wantTokens: 9},
			},
			ownerFilter: filterContract{
				value: rawPredicateCandidateOwnerFilter(entityID), byteFormula: "E+6", wantBytes: entityBytes + 6,
				tokenFormula: "3+6", wantTokens: 9,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, natsclient.ValidateKVLiteralKey(tt.key), tt.layout)
			assert.Equal(t, tt.wantKeyBytes, len(tt.key), tt.keyByteFormula)
			assert.Equal(t, tt.wantKeyTokens, len(strings.Split(tt.key, ".")), tt.keyTokenFormula)
			for _, filter := range tt.forwardFilters {
				require.NoError(t, natsclient.ValidateKVWildcardFilter(filter.value), filter.value)
				assert.Equal(t, filter.wantBytes, len(filter.value), filter.byteFormula)
				assert.Equal(t, filter.wantTokens, len(strings.Split(filter.value, ".")), filter.tokenFormula)
			}
			if tt.ownerFilter.value != "" {
				require.NoError(t, natsclient.ValidateKVWildcardFilter(tt.ownerFilter.value))
				assert.Equal(t, tt.ownerFilter.wantBytes, len(tt.ownerFilter.value), tt.ownerFilter.byteFormula)
				assert.Equal(t, tt.ownerFilter.wantTokens,
					len(strings.Split(tt.ownerFilter.value, ".")), tt.ownerFilter.tokenFormula)
			}
		})
	}

	assert.Equal(t, predicateHash+".*.*.*.*.*.*", predicateIndexForwardFilter(maxPredicate))
	assert.Equal(t, nameHash+".*.*.*.*.*.*.*", nameIndexForwardFilter("Alpha"))
	assert.Equal(t, targetID+".*.*.*.*.*.*.*", incomingIndexTargetFilter(targetID))
	assert.Equal(t, []string{
		maxPredicate + ".*.*.*.*.*.*",
		maxSegment + "." + maxSegment + ".*.*.*.*.*.*.*",
		maxSegment + ".*.*.*.*.*.*.*.*",
	}, rawPredicateCandidateForwardFilters(maxPredicate))
}

func TestGraphIndexKVContract_UnresolvedEntityAndAliasMaximaFailBudgets(t *testing.T) {
	t.Parallel()

	longSegment := strings.Repeat("e", natsclient.MaxKVLiteralTokenBytes+1)
	oversizedEntity := strings.Join([]string{longSegment, "p", "d", "s", "t", "i"}, ".")
	require.True(t, message.IsValidEntityID(oversizedEntity), "fixture must preserve the current six-part contract")

	maxSegment := "a" + strings.Repeat("b", vocabulary.MaxPredicateSegmentBytes-1)
	maxPredicate := strings.Join([]string{maxSegment, maxSegment, maxSegment}, ".")
	otherEntity := "acme.ops.robotics.gcs.mission.001"

	tests := []struct {
		name         string
		key          string
		filter       string
		keyReason    string
		filterReason string
	}{
		{
			name:         "predicate current hash",
			key:          predicateIndexKey(maxPredicate, oversizedEntity),
			filter:       predicateIndexEntityFilter(oversizedEntity),
			keyReason:    natsclient.KVReasonTokenBytes,
			filterReason: natsclient.KVReasonTokenBytes,
		},
		{
			name:         "name",
			key:          nameCompositeKey(nameIndexKey("Alpha"), oversizedEntity, maxPredicate),
			filter:       nameIndexEntityFilter(oversizedEntity),
			keyReason:    natsclient.KVReasonTokenBytes,
			filterReason: natsclient.KVReasonTokenBytes,
		},
		{
			name:         "context",
			key:          contextIndexKey(oversizedEntity, contextHashHex("source.alpha"), maxPredicate),
			filter:       contextIndexEntityFilter(oversizedEntity),
			keyReason:    natsclient.KVReasonTokenBytes,
			filterReason: natsclient.KVReasonTokenBytes,
		},
		{
			name:         "incoming",
			key:          incomingIndexKey(otherEntity, oversizedEntity, maxPredicate),
			filter:       incomingIndexSourceFilter(oversizedEntity),
			keyReason:    natsclient.KVReasonTokenBytes,
			filterReason: natsclient.KVReasonTokenBytes,
		},
		{
			name:         "outgoing",
			key:          oversizedEntity,
			filter:       oversizedEntity,
			keyReason:    natsclient.KVReasonTokenBytes,
			filterReason: natsclient.KVReasonTokenBytes,
		},
		{
			name:         "predicate raw candidate",
			key:          rawPredicateCandidateKey(maxPredicate, oversizedEntity),
			filter:       rawPredicateCandidateOwnerFilter(oversizedEntity),
			keyReason:    natsclient.KVReasonTokenBytes,
			filterReason: natsclient.KVReasonTokenBytes,
		},
		{
			name:         "alias current raw audit",
			key:          strings.Repeat("a", natsclient.MaxKVLiteralKeyBytes+1),
			filter:       strings.Repeat("a", natsclient.MaxKVWildcardFilterBytes+1),
			keyReason:    natsclient.KVReasonBytes,
			filterReason: natsclient.KVReasonBytes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertStableKVContractFailure(
				t, natsclient.ValidateKVLiteralKey, tt.key,
				natsclient.ErrorCodeKVKeyInvalid, tt.keyReason,
			)
			assertStableKVContractFailure(
				t, natsclient.ValidateKVWildcardFilter, tt.filter,
				natsclient.ErrorCodeKVFilterInvalid, tt.filterReason,
			)
		})
	}
}

func TestGraphIndexKVContract_IndividuallyValidEntityTokensFailWholeBudgets(t *testing.T) {
	t.Parallel()

	segment := strings.Repeat("e", 170)
	entityID := strings.Join([]string{segment, segment, segment, segment, segment, segment}, ".")
	require.Len(t, entityID, 1025)
	require.True(t, message.IsValidEntityID(entityID), "current entity contract does not bound total bytes")
	for _, token := range strings.Split(entityID, ".") {
		require.NoError(t, natsclient.ValidateKVLiteralToken(token), "each entity token must be independently valid")
	}

	maxSegment := "a" + strings.Repeat("b", vocabulary.MaxPredicateSegmentBytes-1)
	maxPredicate := strings.Join([]string{maxSegment, maxSegment, maxSegment}, ".")
	tests := []struct {
		name   string
		key    string
		filter string
	}{
		{name: "predicate", key: predicateIndexKey(maxPredicate, entityID), filter: predicateIndexEntityFilter(entityID)},
		{name: "name", key: nameCompositeKey(nameIndexKey("Alpha"), entityID, maxPredicate),
			filter: nameIndexEntityFilter(entityID)},
		{name: "context", key: contextIndexKey(entityID, contextHashHex("source.alpha"), maxPredicate),
			filter: contextIndexEntityFilter(entityID)},
		{name: "incoming", key: incomingIndexKey(entityID, entityID, maxPredicate),
			filter: incomingIndexSourceFilter(entityID)},
		{name: "outgoing", key: entityID, filter: entityID},
		{name: "predicate raw candidate", key: rawPredicateCandidateKey(maxPredicate, entityID),
			filter: rawPredicateCandidateOwnerFilter(entityID)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertStableKVContractFailure(t, natsclient.ValidateKVLiteralKey, tt.key,
				natsclient.ErrorCodeKVKeyInvalid, natsclient.KVReasonBytes)
			assertStableKVContractFailure(t, natsclient.ValidateKVWildcardFilter, tt.filter,
				natsclient.ErrorCodeKVFilterInvalid, natsclient.KVReasonBytes)
		})
	}
}

func assertStableKVContractFailure(
	t *testing.T,
	validate func(string) error,
	input string,
	wantCode string,
	wantReason string,
) {
	t.Helper()
	var firstDetail map[string]any
	for iteration := 0; iteration < 2; iteration++ {
		err := validate(input)
		require.Error(t, err)
		assert.True(t, errs.IsInvalid(err))
		var classified *errs.ClassifiedError
		require.True(t, errors.As(err, &classified))
		assert.Equal(t, wantCode, classified.Code)
		assert.Equal(t, wantReason, classified.Detail[natsclient.KVDetailReason])
		if iteration == 0 {
			firstDetail = classified.Detail
		} else {
			assert.Equal(t, firstDetail, classified.Detail)
		}
	}
}
