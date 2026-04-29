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
// A nil *ProfileResult with a nil error from ReadOperatingModel signals
// "no profile yet" — the assembler produces an empty operating-model slice
// and downstream consumers skip injection.
type ProfileReader interface {
	ReadOperatingModel(ctx context.Context, org, platform, userID string) (*ProfileResult, error)

	// ReadProfileVersion returns the current ProfileVersion for the user, or
	// 0 if the user has no profile yet. Cheaper than ReadOperatingModel for
	// callers that only need the version (e.g. /onboard re-run version-bump).
	ReadProfileVersion(ctx context.Context, org, platform, userID string) (int, error)

	// ReadLessons returns up to `limit` of the user's compaction-extracted
	// lessons, ranked most-recent first. Returns nil + nil error when the
	// user has no lessons yet. limit <= 0 means "use the implementation's
	// default cap" (the GraphProfileReader caps at 50 to bound the KV
	// traversal cost).
	ReadLessons(ctx context.Context, org, platform, userID string, limit int) ([]Lesson, error)
}

// EmptyProfileReader always reports no profile. Used as the default when a
// real graph client is not wired.
type EmptyProfileReader struct{}

// ReadOperatingModel implements ProfileReader. No I/O — always returns nil.
func (EmptyProfileReader) ReadOperatingModel(_ context.Context, _, _, _ string) (*ProfileResult, error) {
	return nil, nil
}

// ReadProfileVersion implements ProfileReader. No I/O — always returns 0.
func (EmptyProfileReader) ReadProfileVersion(_ context.Context, _, _, _ string) (int, error) {
	return 0, nil
}

// ReadLessons implements ProfileReader. No I/O — always returns nil.
func (EmptyProfileReader) ReadLessons(_ context.Context, _, _, _ string, _ int) ([]Lesson, error) {
	return nil, nil
}
