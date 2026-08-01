package natsclient

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// SubjectCapture reports one stream subject filter that swallows a subject the
// deployment serves request/reply on.
type SubjectCapture struct {
	// Stream is the stream whose filter captures the subject.
	Stream string
	// Filter is the specific subject filter that matches.
	Filter string
	// Subject is the captured request/reply subject.
	Subject string
}

// Error renders the collision with the three facts an operator needs: which
// stream, which subject, and what to do. "Subject collision detected" without
// all three sends someone reading configs by hand — the tax a bare
// "put community failed" imposed elsewhere.
func (c SubjectCapture) Error() string {
	return fmt.Sprintf(
		"stream %q (filter %q) captures request/reply subject %q — requests to it are answered by JetStream with a publish ack, "+
			"not by the responder, so the caller silently receives an empty result; narrow the stream's subjects or move the subject "+
			"(e.g. declare the port with an explicit subject no stream covers)",
		c.Stream, c.Filter, c.Subject)
}

// SubjectFilterCaptures reports whether a NATS subject filter matches a
// concrete subject, using NATS token wildcard semantics.
//
// `*` matches exactly ONE token; `>` matches one or more trailing tokens and is
// only meaningful as the final token. `tool.>` captures `tool.list`;
// `tool.*` captures `tool.list` but not `tool.list.v2`; `tool.list` captures
// only itself.
//
// # Why this cannot be a prefix test
//
// The obvious `strings.HasPrefix(subject, trimmed)` shortcut is wrong in both
// directions. `tool.>` would appear to capture `toolbox.list` (it does not —
// `>` follows a token boundary), and `tool.*` would appear to capture
// `tool.list.v2` (it does not — `*` is exactly one token). Both errors are
// silent: the first invents collisions that block valid deployments, the second
// misses real ones. Token-position semantics are the whole point, which is the
// same lesson predicate_index.go records about suffix matching.
func SubjectFilterCaptures(filter, subject string) bool {
	if filter == "" || subject == "" {
		return false
	}
	f := strings.Split(filter, ".")
	s := strings.Split(subject, ".")

	for i, token := range f {
		if token == ">" {
			// `>` is only a wildcard as the FINAL token, and it requires at
			// least one token to consume. A literal ">" earlier in the filter
			// is not a wildcard.
			return i == len(f)-1 && len(s) > i
		}
		if i >= len(s) {
			return false
		}
		if token == "*" {
			continue
		}
		if token != s[i] {
			return false
		}
	}
	// Every filter token matched; it captures only if the subject has no extra
	// trailing tokens.
	return len(f) == len(s)
}

// FindSubjectCaptures returns every collision between the given stream subject
// filters and the declared request/reply subjects, sorted for stable reporting.
//
// # Why the guard is derived rather than listed
//
// It takes both sets as inputs so neither side has to know about the other. A
// stream shape added later, or a request/reply subject added later, is covered
// without anyone updating a table of known-bad pairs — which is what makes this
// close the CLASS rather than the one instance gh#810 found. A hand-maintained
// list would have needed an entry for `tool.>` × `tool.list` written by someone
// who already understood the failure, and the whole problem is that nobody did
// until an e2e stage tripped over it.
func FindSubjectCaptures(streamName string, filters []string, declaredSubjects []string) []SubjectCapture {
	var found []SubjectCapture
	for _, filter := range filters {
		for _, subject := range declaredSubjects {
			if SubjectFilterCaptures(filter, subject) {
				found = append(found, SubjectCapture{Stream: streamName, Filter: filter, Subject: subject})
			}
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].Filter != found[j].Filter {
			return found[i].Filter < found[j].Filter
		}
		return found[i].Subject < found[j].Subject
	})
	return found
}

// ReportSubjectCaptures logs every provisioned stream that captures one of the
// given request/reply subjects, and returns what it found.
//
// # Why this reports rather than refuses
//
// Refusing to start would turn a deployment that is currently running — badly,
// but running — into a boot failure, for a condition that this change has
// already made loud at the point of use: DecodeQueryReply now rejects the
// publish ack such a deployment receives, so the runtime symptom is a typed
// error naming the cause rather than a silently empty result. The precondition
// the framework's fail-closed rule protects is correctness, and correctness is
// restored by that rejection; what remains here is telling an operator WHY,
// early, with the remedy attached.
//
// The asymmetry matters: a missing warning is recoverable by anyone who reads
// the runtime error, while a refusal to boot on a config an operator cannot
// immediately change is not.
//
// # Why subscribe time, not only provisioning time
//
// Streams are typically provisioned BEFORE components subscribe, so a check
// that only ran when a stream is created would see no declared subjects yet and
// catch nothing. Checking when a subject is declared covers the ordering that
// actually occurs, and gh#810's own deployment is that ordering.
func (c *Client) ReportSubjectCaptures(ctx context.Context, subjects []string) []SubjectCapture {
	if c == nil || c.js == nil || len(subjects) == 0 {
		return nil
	}
	var found []SubjectCapture
	lister := c.js.ListStreams(ctx)
	for info := range lister.Info() {
		if info == nil {
			continue
		}
		found = append(found, FindSubjectCaptures(info.Config.Name, info.Config.Subjects, subjects)...)
	}
	if err := lister.Err(); err != nil {
		// Advisory: a listing failure must not be mistaken for "no collisions".
		c.logger.Warn("natsclient: could not list streams to check request/reply subject capture — "+
			"a collision would go unreported rather than absent", "error", err, "subjects", subjects)
		return found
	}
	for _, capture := range found {
		c.logger.Error("natsclient: request/reply subject is captured by a JetStream stream", "detail", capture.Error())
	}
	return found
}
