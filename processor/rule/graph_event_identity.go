package rule

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

const (
	ruleTriggerDigestDomain = "semstreams.graph.rule-trigger.v1"
	ruleTriggerEntityPrefix = "semstreams.framework.graph.rules.trigger."
)

// ruleTriggerEntityID returns the stable framework-owned entity updated when a
// rule triggers. Pack identity disambiguates processor-local rule IDs, while
// the full digest keeps both exact inputs out of NATS key positions. Replicas
// of the same pack intentionally converge on the same entity.
func ruleTriggerEntityID(packID, ruleID string) (string, error) {
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
	entityID := ruleTriggerEntityPrefix + hex.EncodeToString(digest.Sum(nil))
	if err := semtypes.ValidateEntityID(entityID); err != nil {
		return "", errs.WrapInvalid(err, "RuleProcessor", "ruleTriggerEntityID", "validate derived trigger entity ID")
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
