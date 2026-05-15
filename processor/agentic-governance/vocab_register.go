package agenticgovernance

import "github.com/c360studio/semstreams/vocabulary/governance"

// init registers the governance vocabulary predicates as a
// side-effect of importing this package. This is the only way the
// rule validator's vocabulary.IsRuleOpaque check
// (processor/rule/config_validation.go:179) sees the
// governance.injection.* metadata: the global registry is
// in-process, and no production binary calls vocabulary sub-package
// Register() functions explicitly today. When the wider boot
// sequence acquires an explicit vocab-bootstrap step the call here
// can move there.
func init() {
	governance.Register()
}
