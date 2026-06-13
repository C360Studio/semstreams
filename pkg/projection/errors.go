package projection

import "errors"

// ErrInvalidContract is returned when a projection contract is malformed, or
// when deriving claims from it produces an inconsistent registration. It wraps
// the underlying ownership error (errors.Is still matches ownership.ErrInvalidClaim
// / ownership.ErrOwnershipOverlap) so callers can branch on the precise cause.
var ErrInvalidContract = errors.New("projection: invalid contract")
