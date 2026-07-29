package natsclient

import (
	"errors"
	"fmt"
	"strings"
)

// Backing-stream naming conventions. NATS implements every KV bucket and every
// ObjectStore as an ordinary JetStream stream with a reserved name prefix, so a
// bucket's lifecycle-eviction config (MaxAge/MaxBytes/Discard) is reachable
// through the plain stream API. That reachability is what the retention
// reconcilers here and in storage/objectstore depend on — and it is also what
// lets an unfiltered provisioning seam stamp age eviction onto authoritative
// graph state, which is why every such seam refuses these prefixes outright.
//
// These are the ONE definition of each prefix. The KV and ObjectStore retention
// paths and every provisioning guard read them from here so the convention
// cannot fork into per-package literals.
const (
	// KVStreamPrefix is the NATS naming convention for a KV bucket's backing
	// stream: KV_<bucket> (nats.go jetstream/kv.go, kvBucketNamePre = "KV_").
	KVStreamPrefix = "KV_"

	// ObjectStoreStreamPrefix is the NATS naming convention for an
	// ObjectStore's backing stream: OBJ_<bucket> (nats.go
	// jetstream/object.go, objNameTmpl = "OBJ_%s").
	ObjectStoreStreamPrefix = "OBJ_"
)

// ResourceKind classifies a physical JetStream stream name by the reserved
// backing-stream namespace it occupies. It is the naming convention read two
// ways: the provisioning guard refuses everything that is not an ordinary
// stream, and the storage inventory reports every kind.
type ResourceKind string

// Resource kinds.
const (
	// ResourceOrdinaryStream is a stream SemStreams provisions to carry
	// time-shaped events — the only kind stream provisioning governs.
	ResourceOrdinaryStream ResourceKind = "stream"
	// ResourceKeyValue is a KV bucket's backing stream (KVStreamPrefix).
	ResourceKeyValue ResourceKind = "kv"
	// ResourceObjectStore is an ObjectStore's backing stream
	// (ObjectStoreStreamPrefix).
	ResourceObjectStore ResourceKind = "objectstore"
)

// ClassifyBackingStream maps a physical JetStream stream name to the logical
// resource it backs: the kind, and for a backing stream the bucket name behind
// it (empty for an ordinary stream, which has no bucket).
//
// EXACTLY ONE leading prefix is stripped. A product bucket may legitimately be
// named "KV_FOO", whose backing stream is then "KV_KV_FOO" and whose real name
// is "KV_FOO" — not "FOO". Stripping greedily would attribute that resource to
// a different bucket entirely, which is why the recovery lives here, once, and
// is read by both the provisioning guard's diagnostic and the inventory's owner
// attribution: the two can disagree about a bucket's name only if they compute
// it separately, so they do not.
//
// The rule is the prefix and nothing else — no catalog lookup, no membership
// test — so a product or sister-repo bucket outside the framework catalog
// classifies identically to a framework one.
func ClassifyBackingStream(stream string) (ResourceKind, string) {
	switch {
	case strings.HasPrefix(stream, KVStreamPrefix):
		return ResourceKeyValue, strings.TrimPrefix(stream, KVStreamPrefix)
	case strings.HasPrefix(stream, ObjectStoreStreamPrefix):
		return ResourceObjectStore, strings.TrimPrefix(stream, ObjectStoreStreamPrefix)
	}
	return ResourceOrdinaryStream, ""
}

// ErrBackingStreamNotProvisionable is returned when a stream-provisioning seam
// is handed a KV or ObjectStore backing-stream name. Sentinel so a boot path (or
// a test) can classify the refusal distinctly from a NATS-side create failure.
var ErrBackingStreamNotProvisionable = errors.New(
	"stream provisioning governs ordinary streams only; " +
		"KV and ObjectStore backing streams are not provisionable here")

// CheckOrdinaryStreamName is the fail-closed name guard shared by every
// stream-provisioning seam. Stream provisioning governs ORDINARY streams —
// JetStream streams SemStreams creates to carry time-shaped events. NATS also
// implements every KV bucket and every ObjectStore as a JetStream stream
// (KVStreamPrefix+<bucket> and ObjectStoreStreamPrefix+<bucket>), which puts
// those resources within reach of the plain stream API. They are not ordinary
// streams: KV buckets are acquired through the bucket descriptor catalog's
// acquisition seam or by the product component that owns them, content stores
// through the content-store constructor, and both retention contracts belong to
// graph-retention (ADR-068/073).
//
// The hazard is measured, not theoretical. Handing an EXISTING OBJ_ backing
// stream to a reconciling provisioner switches on age eviction, flips Discard to
// delete-oldest, AND replaces the store's real chunk/meta subjects — silently,
// returning nil. On a get-or-create seam the damage is name-squatting instead: a
// stream created under a bucket's reserved name carrying a foreign TTL and the
// wrong subjects, which the bucket's later catalog acquisition then collides
// with. Both shapes are refused here, at the seam.
//
// The rule is the NAME PREFIX, not descriptor-catalog membership, because every
// downstream safety net has a hole: ReconcileNoLifecycleRetention clears only
// MaxAge/MaxBytes and never a discard policy; a descriptor declared
// retention-unmanaged reconciles nothing at all; and product or sister-repo
// buckets outside the catalog have neither an acquisition seam nor a pre-start
// backstop to repair a stamp a provisioner applied. A guard covering only
// catalog members would leave all three open.
//
// The source argument names where the declaration came from — an operator
// config key, a component port, or the calling seam — so the operator can find
// and delete it. This function is pure and I/O-free, so it can run at config
// validation, before any JetStream call, and before any connection or
// circuit-breaker check.
func CheckOrdinaryStreamName(name, source string) error {
	kind, bucket := ClassifyBackingStream(name)
	switch kind {
	case ResourceKeyValue:
		return fmt.Errorf(
			"%w: %q (declared by %s) is the backing stream for KV bucket %q — a framework-owned KV bucket is "+
				"acquired through the bucket descriptor catalog's acquisition seam (graph.EnsureCatalogBucket "+
				"/ natsclient.EnsureFrameworkBucket); a product bucket outside that catalog is provisioned by "+
				"the component that owns it. Either way its retention contract belongs to graph-retention "+
				"(ADR-068/073), not to stream provisioning; remove this stream declaration",
			ErrBackingStreamNotProvisionable, name, source, bucket)
	case ResourceObjectStore:
		return fmt.Errorf(
			"%w: %q (declared by %s) is the backing stream for ObjectStore bucket %q — content stores are "+
				"provisioned by the content-store constructor (storage/objectstore.NewStoreWithConfig), and "+
				"their retention contract belongs to graph-retention (ADR-068/073), not to stream "+
				"provisioning; remove this stream declaration",
			ErrBackingStreamNotProvisionable, name, source, bucket)
	case ResourceOrdinaryStream:
	}
	return nil
}
