package operatingmodel

import "context"

// ProfileResult holds the output of a ProfileReader query.
type ProfileResult struct {
	Entries []Entry
	Version int
}

// ProfileReader reads a user's current operating-model profile from the
// knowledge graph.
//
// A nil *ProfileResult with a nil error signals "no profile yet" — the
// assembler produces an empty operating-model slice and downstream consumers
// skip injection.
type ProfileReader interface {
	ReadOperatingModel(ctx context.Context, org, platform, userID string) (*ProfileResult, error)
}

// EmptyProfileReader always reports no profile. Used as the default when a
// real graph client is not wired.
type EmptyProfileReader struct{}

// ReadOperatingModel implements ProfileReader. No I/O — always returns nil.
func (EmptyProfileReader) ReadOperatingModel(_ context.Context, _, _, _ string) (*ProfileResult, error) {
	return nil, nil
}
