package config

import (
	"fmt"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
)

// maxBucketNameBytes is the longest KV bucket name NATS accepts: the server
// bounds stream names at 255 bytes (nats-server server/jetstream_api.go
// JSMaxNameLen) and a KV bucket's stream is "KV_" + its name.
const maxBucketNameBytes = 255 - len("KV_")

// BucketName is the ONE derivation of a deployment's configuration bucket
// name: "semstreams_config_<org>_<stem>", from the configuration document's
// platform.org and its DECLARED platform.id — never the minted identifier,
// which lives inside the bucket and so cannot name it (owner ruling on #1188,
// 2026-09-01: "Bucket = semstreams_config_<org>_<stem>, looked up by the
// same").
//
// It refuses an empty part, a byte outside the NATS bucket grammar
// [A-Za-z0-9_-] (nats.go jetstream/kv.go validBucketRe), and a result longer
// than NATS accepts. Every pair Config.Validate admits passes: a segment is
// already a subset of that grammar, and a declared pair is at most 163 bytes.
//
// The separator is not injective: `_` is legal inside both parts, so org
// "a_b" with stem "c" and org "a" with stem "b_c" name the same bucket. That
// alias is refused at Start by the identity record's org/stem compare, not
// prevented here (owner ruling on #1188, 2026-09-27, Q2 (a)).
func BucketName(org, stem string) (string, error) {
	for _, part := range []struct{ field, value string }{{"platform.org", org}, {"platform.id", stem}} {
		if part.value == "" {
			return "", errs.WrapInvalid(errs.ErrInvalidConfig, "config", "BucketName",
				fmt.Sprintf("%s is empty, so no configuration bucket can be named for it", part.field))
		}
		for i := 0; i < len(part.value); i++ {
			if !isBucketNameByte(part.value[i]) {
				return "", errs.WrapInvalid(errs.ErrInvalidConfig, "config", "BucketName",
					fmt.Sprintf("%s %q holds byte %q, which a NATS KV bucket name cannot carry", part.field, part.value, part.value[i]))
			}
		}
	}
	name := graph.BucketSemStreamsConfig + "_" + org + "_" + stem
	if len(name) > maxBucketNameBytes {
		return "", errs.WrapInvalid(errs.ErrInvalidConfig, "config", "BucketName",
			fmt.Sprintf("configuration bucket name for %q/%q is %d bytes; NATS accepts at most %d", org, stem, len(name), maxBucketNameBytes))
	}
	return name, nil
}

func isBucketNameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-'
}
