package rulepack

import (
	"strings"
	"testing"
)

func TestValidateID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		packID  string
		wantErr string
	}{
		{name: "minimum", packID: "a"},
		{name: "full alphabet", packID: "Pack-v1_test=value-1"},
		{name: "maximum", packID: strings.Repeat("p", MaxPackIDBytes)},
		{name: "empty", wantErr: "pack_id is required"},
		{name: "oversized", packID: strings.Repeat("p", MaxPackIDBytes+1), wantErr: "maximum is 246"},
		{name: "invalid alphabet", packID: "pack:one", wantErr: "invalid pack_id"},
		{name: "separator rejected", packID: "pack.one", wantErr: "one literal KV token"},
		{name: "slash rejected", packID: "pack/one", wantErr: "invalid pack_id"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateID(test.packID)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateID(%q): %v", test.packID, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("ValidateID(%q) error = %v, want substring %q", test.packID, err, test.wantErr)
			}
		})
	}
}
