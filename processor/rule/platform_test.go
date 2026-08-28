package rule

import "github.com/c360studio/semstreams/types"

// testPlatform is the deployment authority every directly constructed rule in
// this package's tests mints under (positions 1-2 of trigger identities).
var testPlatform = types.PlatformMeta{Org: "acme", Platform: "dep1"}
