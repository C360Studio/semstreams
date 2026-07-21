package fusion

import "github.com/c360studio/semstreams/graph"

// ExportReadinessEnvelope exposes the unexported gate projection to the external
// fusion_test package. The projection is deliberately unexported in production — it is
// an internal detail of how Fuse asks the canonical gate its question, not part of the
// contract — but it needs a test that proves it copies every field, because a silent
// drop there changes whether fusion serves at all.
func ExportReadinessEnvelope(s IndexStatus) graph.IndexStatusResponse {
	return s.readinessEnvelope()
}
