package projection

import "errors"

// ErrInvalidContract identifies a projection contract rejected before use.
var ErrInvalidContract = errors.New("projection: invalid contract")
