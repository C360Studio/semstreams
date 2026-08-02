package natsclient

import (
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
)

// ErrPayloadTooLarge is the sentinel for a payload refused (or rejected by the
// server) because it exceeds the wire limit. It classifies PERMANENT
// (errs.ErrorInvalid) at every seam: a payload the server will never accept is
// not transient by any retry — retrying it forever was the gh#857 pathology.
// Match with errors.Is.
var ErrPayloadTooLarge = errors.New("payload exceeds the server's maximum payload size")

// serverPayloadLimit returns the wire limit in bytes. A live connection's
// advertisement is authoritative and is cached (causally — only a value the
// server actually advertised is ever stored); when the connection is down the
// cached advertisement answers. Before the FIRST connection there is nothing
// to answer with: the limit is 0, meaning UNKNOWN, and every guard built on
// it disables itself — an unknown limit must never produce a permanent size
// verdict, so pre-connect sends fall through to connection-state errors
// (ErrNotConnected) instead of a false-permanent refusal. There is
// deliberately NO compiled-in fallback: the payload-bounds spec forbids the
// framework carrying its own copy of the wire limit, and a guessed number
// here turned into permanent verdicts against a server we never talked to.
func (m *Client) serverPayloadLimit() int {
	m.mu.RLock()
	conn := m.conn
	m.mu.RUnlock()
	if conn != nil {
		if mp := conn.MaxPayload(); mp > 0 {
			m.advertisedPayloadLimit.Store(mp)
			return int(mp)
		}
	}
	return int(m.advertisedPayloadLimit.Load())
}

// ServerPayloadLimit reports the wire limit in bytes: the connected server's
// advertised max_payload, the cached advertisement when the connection is
// down, or 0 when no server has ever advertised one (UNKNOWN — see
// serverPayloadLimit). Exported for components that derive OFFLOAD thresholds
// from the wire bound (agentic-loop result offload, gh#857 D4) — the
// derivation, not the number, is the contract: a raised server limit
// propagates to every threshold with no code or config change. Callers MUST
// treat 0 as "no known bound" (skip the derivation), never as a real limit.
// Returns the answer (a byte count), never the connection. New exported
// surface recorded on the payload-size-chokepoints conformance table.
func (m *Client) ServerPayloadLimit() int {
	return m.serverPayloadLimit()
}

// checkPayloadSize is the ONE shared seam guard (payload-bounds spec; gh#857
// D1). It refuses a payload exceeding limit with a permanent classified error
// carrying the three operator facts (size, limit, target) and the remedy.
// limit <= 0 disables the check: an unknown limit must never produce a
// permanent verdict (connection-state errors win until a server advertises).
// Equality passes: the server accepts a payload of exactly its limit.
func checkPayloadSize(size, limit int, seam, target string) error {
	return checkPayloadBound(size, limit, "server limit", seam, target)
}

// checkPayloadBound is checkPayloadSize with an explicit bound name, so a
// refusal produced by a LOCAL bound (the KVOptions.MaxValueSize override)
// names its real source instead of blaming the server.
func checkPayloadBound(size, limit int, bound, seam, target string) error {
	if limit <= 0 || size <= limit {
		return nil
	}
	return errs.WrapInvalid(
		fmt.Errorf("%w: %d bytes > %d-byte %s for %s — offload bulky content "+
			"(ContentStorable/ObjectStore), narrow the query, or page",
			ErrPayloadTooLarge, size, limit, bound, target),
		"Client", seam, "refuse oversized payload")
}

// CheckReplySize is the exported face of the seam guard for components that
// serve request/reply on a RAW subscription (outside SubscribeForRequests,
// which guards its own replies). It exists so a raw responder can answer
// "too large" typed instead of letting the caller time out — objectstore's
// API responder is the known user. New exported surface recorded on the
// payload-size-chokepoints conformance table.
func (m *Client) CheckReplySize(size int, subject string) error {
	return checkPayloadSize(size, m.serverPayloadLimit(), "CheckReplySize", "reply on "+subject)
}

// classifyMaxPayload upgrades a RAW server/client-library oversize rejection
// to the same permanent classified error the seam guard produces. The guard
// prevents most oversize sends before I/O; this catches the residue (e.g.
// header overhead pushing a payload past the limit the guard measured against
// data alone, or a send racing the first advertisement while the limit is
// still unknown), so no path leaves nats.ErrMaxPayload unclassified — where
// the default classifier would call it transient and retry it forever.
//
// This is the D2 mechanism: classification at the natsclient boundary rather
// than an arm inside errs.Classify, because pkg/errs is dependency-free and
// must not import nats. Recorded as a deviation row on the change's
// conformance table.
func classifyMaxPayload(err error, seam, target string) error {
	if err == nil || !errors.Is(err, nats.ErrMaxPayload) {
		return err
	}
	return errs.WrapInvalid(
		fmt.Errorf("%w: server refused %s: %v", ErrPayloadTooLarge, target, err),
		"Client", seam, "oversized payload rejected by server")
}
