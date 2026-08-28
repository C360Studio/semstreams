package rule

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

const ruleTriggerDigestDomain = "semstreams.graph.rule-trigger.v1"

// ruleTriggerEntityID returns the stable entity updated when a rule triggers:
// org.platform.rules.graph.trigger.<digest>, composed from the pkg/types
// rule-trigger identity family under the deployment's own authority (ADR-102
// d2; supersedes ADR-076 d1's fixed framework namespace). Pack identity
// disambiguates processor-local rule IDs, while the full digest keeps both
// exact inputs out of NATS key positions. Replicas of the same pack in one
// deployment intentionally converge on the same entity; two deployments
// running the same pack do not, because the authority differs.
func ruleTriggerEntityID(org, platform, packID, ruleID string) (string, error) {
	if err := validatePackID(packID); err != nil {
		return "", errs.WrapInvalid(err, "RuleProcessor", "ruleTriggerEntityID", "validate pack ID")
	}
	if ruleID == "" {
		return "", errs.WrapInvalid(errs.ErrInvalidData, "RuleProcessor", "ruleTriggerEntityID", "rule ID is required")
	}
	digest := sha256.New()
	writeRuleTriggerFrame(digest, ruleTriggerDigestDomain)
	writeRuleTriggerFrame(digest, packID)
	writeRuleTriggerFrame(digest, ruleID)
	entityID, err := semtypes.RuleTriggerIdentityFamily().EntityID(org, platform, hex.EncodeToString(digest.Sum(nil)))
	if err != nil {
		return "", errs.WrapInvalid(err, "RuleProcessor", "ruleTriggerEntityID", "compose trigger entity ID under the deployment authority")
	}
	return entityID, nil
}

type ruleTriggerWriter interface {
	Write([]byte) (int, error)
}

func writeRuleTriggerFrame(destination ruleTriggerWriter, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write([]byte(value))
}
