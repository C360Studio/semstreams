package natsclient

import (
	"errors"
	"fmt"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
)

// defaultMaxPayloadBytes is the FALLBACK wire limit, used only when no live
// connection can advertise the server's real one (e.g. a guard running before
// connect). It matches the NATS server default. It is deliberately unexported
// and deliberately not configuration: the server owns this number, and a
// second copy in framework config would drift — see the payload-bounds spec
// ("the payload limit MUST be derived from the server, never compiled in").
const defaultMaxPayloadBytes = 1024 * 1024

// ErrPayloadTooLarge is the sentinel for a payload refused (or rejected by the
// server) because it exceeds the wire limit. It classifies PERMANENT
// (errs.ErrorInvalid) at every seam: a payload the server will never accept is
// not transient by any retry — retrying it forever was the gh#857 pathology.
// Match with errors.Is.
var ErrPayloadTooLarge = errors.New("payload exceeds the server's maximum payload size")

// serverPayloadLimit returns the wire limit the CONNECTED server advertises,
// falling back to the NATS default when no connection is available. Reading it
// live (not caching at construction) means an operator who raises the server's
// max_payload is honored framework-wide with no code or config change — the
// mechanism that retires the per-surface workarounds sisters shipped.
func (m *Client) serverPayloadLimit() int {
	m.mu.RLock()
	conn := m.conn
	m.mu.RUnlock()
	if conn != nil {
		if mp := conn.MaxPayload(); mp > 0 {
			return int(mp)
		}
	}
	return defaultMaxPayloadBytes
}

// ServerPayloadLimit reports the wire limit in bytes: the connected server's
// advertised max_payload, or the NATS default when no connection is live.
// Exported for components that derive OFFLOAD thresholds from the wire bound
// (agentic-loop result offload, gh#857 D4) — the derivation, not the number,
// is the contract: a raised server limit propagates to every threshold with
// no code or config change. Returns the answer (a byte count), never the
// connection. New exported surface recorded on the payload-size-chokepoints
// conformance table.
func (m *Client) ServerPayloadLimit() int {
	return m.serverPayloadLimit()
}

// checkPayloadSize is the ONE shared seam guard (payload-bounds spec; gh#857
// D1). It refuses a payload exceeding limit with a permanent classified error
// carrying the three operator facts (size, limit, target) and the remedy.
// limit <= 0 disables the check (callers must not pass 0 except by explicit
// override intent). Equality passes: the server accepts a payload of exactly
// its limit.
func checkPayloadSize(size, limit int, seam, target string) error {
	if limit <= 0 || size <= limit {
		return nil
	}
	return errs.WrapInvalid(
		fmt.Errorf("%w: %d bytes > %d-byte server limit for %s — offload bulky content "+
			"(ContentStorable/ObjectStore), narrow the query, or page",
			ErrPayloadTooLarge, size, limit, target),
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
// data alone), so no path leaves nats.ErrMaxPayload unclassified — where the
// default classifier would call it transient and retry it forever.
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
