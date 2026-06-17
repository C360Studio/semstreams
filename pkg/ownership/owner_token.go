package ownership

// ownerTokenSep separates the owner id from the per-process incarnation nonce in
// an OwnerToken's wire form. It is intentionally unexported — no caller outside
// this file should know or compose the token format.
const ownerTokenSep = "#"

// OwnerToken is the write-lease credential a producer stamps on an owned graph
// mutation request (ADR-056 OwnerToken write-lease). Its wire form is
// "<owner>#<incarnation>", but that composition is an IMPLEMENTATION DETAIL:
// producers obtain a token from Registry.OwnerToken (or a pkg/projection bind
// result, which delegates here) and put OwnerToken.Wire() on the request — they
// never hand-compose the "<owner>#<incarnation>" string. Hand-composition
// couples every adapter, YAML config, and operator to the credential format and
// re-opens the footgun SemOps flagged (ADR-056 PR-3.5): a governance credential
// should be minted by the registry that owns the incarnation, not assembled by
// its consumers.
//
// The zero value is the empty token: Wire() returns "" and IsZero() reports
// true. The graph-ingest lease check treats an empty token as "skip" — the
// two-state wire contract (empty = skip, "<owner>#<incarnation>" = compare). An
// OwnerToken built with an empty incarnation (a legacy / pre-fence owner, or a
// process with no ownership Registry wired) collapses to the zero token, so a
// bare "<owner>#" can never reach the wire.
type OwnerToken struct {
	owner       string
	incarnation string
}

// newOwnerToken is the single composition point for the token. An empty
// incarnation collapses to the zero token, enforcing the two-state contract: a
// token is either empty or a full "<owner>#<incarnation>", never a bare owner.
func newOwnerToken(owner, incarnation string) OwnerToken {
	if incarnation == "" {
		return OwnerToken{}
	}
	return OwnerToken{owner: owner, incarnation: incarnation}
}

// ExpectedOwnerToken reconstructs the token the rightful owner of a cell would
// stamp, from the (owner, incarnation) the lease reader (ClaimReader.OwnerOf)
// returns for a claimed (entity, predicate) cell. The graph-ingest lease check
// compares the incoming request's OwnerToken against ExpectedOwnerToken(...).Wire().
// A "" incarnation (a legacy / pre-fence owner) yields the zero token, so the
// caller fail-opens rather than rejecting every legitimate write against an
// unfenced owner. This is also the explicit constructor tests use to forge a
// token from known parts; producers mint via Registry.OwnerToken instead.
func ExpectedOwnerToken(owner, incarnation string) OwnerToken {
	return newOwnerToken(owner, incarnation)
}

// Wire returns the on-the-wire credential stamped on a mutation request's
// OwnerToken field: "<owner>#<incarnation>", or "" for the zero token.
func (t OwnerToken) Wire() string {
	if t.incarnation == "" {
		return ""
	}
	return t.owner + ownerTokenSep + t.incarnation
}

// IsZero reports whether this is the empty token — no incarnation, so the lease
// is not enforceable and the graph-ingest check skips it.
func (t OwnerToken) IsZero() bool {
	return t.incarnation == ""
}

// OwnerToken mints the write-lease credential for `owner` from this process's
// incarnation nonce (ADR-056 PR-3.5). This — and the pkg/projection bind result,
// which delegates here — is the ONLY way a producer should obtain a token.
// Producers never string-concatenate "<owner>#<incarnation>" themselves; the
// format lives only in this file. A Registry always carries a non-empty
// incarnation (NewRegistry generates one), so the minted token is the zero token
// only via an explicitly empty incarnation, never in normal operation.
func (reg *Registry) OwnerToken(owner string) OwnerToken {
	return newOwnerToken(owner, reg.incarnation)
}
