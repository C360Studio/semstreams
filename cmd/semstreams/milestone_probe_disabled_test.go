//go:build !e2e_process_barrier

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMilestoneProbeIsInertWithoutTag pins the behaviour of the untagged stub.
// Passing nils is the point: an ordinary build must not reach code that would
// dereference them.
//
// The compile-time gate itself — that only a tagged file imports the harness,
// and that the agentic tier is the only tier arming the probe — is pinned once
// for every hook by test/contract/e2e_tier_binary_contract_test.go.
func TestMilestoneProbeIsInertWithoutTag(t *testing.T) {
	require.NoError(t, registerE2EMilestoneProbe(nil, nil, nil))
}
