package export

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

// Format identifies an RDF serialization format.
type Format int

const (
	// Turtle is the Terse RDF Triple Language format (.ttl).
	// Compact and human-readable with prefix declarations and subject grouping.
	Turtle Format = iota

	// NTriples is the N-Triples format (.nt).
	// Line-based, one triple per line, fully expanded IRIs.
	NTriples

	// JSONLD is the JSON-LD format (.jsonld).
	// JSON with @context and @graph for web API consumption.
	JSONLD
)

// String returns the format name.
func (f Format) String() string {
	switch f {
	case Turtle:
		return "Turtle"
	case NTriples:
		return "N-Triples"
	case JSONLD:
		return "JSON-LD"
	default:
		return fmt.Sprintf("Format(%d)", int(f))
	}
}

// Option configures serialization behavior.
type Option func(*options)

type options struct {
	baseIRI            string
	subjectIRIFn       func(string) string
	customSubjectIRIFn bool
}

func defaultOptions() options {
	return options{
		baseIRI: vocabulary.SemStreamsBase,
	}
}

// WithBaseIRI overrides the default SemStreamsBase IRI used for generated URIs.
func WithBaseIRI(base string) Option {
	return func(o *options) {
		o.baseIRI = base
	}
}

// WithSubjectIRIFunc provides a custom function to map entity ID strings to IRIs.
// When set, this replaces the default subject IRI generation logic entirely;
// WithBaseIRI has no effect on subjects when a custom function is provided.
func WithSubjectIRIFunc(fn func(string) string) Option {
	return func(o *options) {
		o.subjectIRIFn = fn
		o.customSubjectIRIFn = true
	}
}

// Serialize writes triples in the specified format to the writer.
// Invalid triples (empty subject or predicate) are silently skipped.
func Serialize(w io.Writer, triples []message.Triple, format Format, opts ...Option) error {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}

	switch format {
	case Turtle:
		return writeTurtle(w, triples, &o)
	case NTriples:
		return writeNTriples(w, triples, &o)
	case JSONLD:
		return writeJSONLD(w, triples, &o)
	default:
		return fmt.Errorf("unsupported format: %v", format)
	}
}

// SerializeToString returns the serialized output as a string.
func SerializeToString(triples []message.Triple, format Format, opts ...Option) (string, error) {
	var buf bytes.Buffer
	if err := Serialize(&buf, triples, format, opts...); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// defaultSubjectIRI converts an entity ID or arbitrary subject string to an IRI.
// For valid 6-part entity IDs, it produces:
//
//	{base}/entities/{org}/{platform}/{system}/{domain}/{type}/{instance}
//
// The path follows the canonical entity-ID order from the named fields
// (owner item O-10 on #1095): the exporter is not a second interpreter of
// position order.
//
// For other subjects, it falls back to dot-to-slash conversion:
//
//	{base}/subjects/{dots→slashes}
func defaultSubjectIRI(subject string) string {
	return subjectToIRI(subject, vocabulary.SemStreamsBase)
}

// subjectToIRI converts a subject string to an IRI using the given base.
func subjectToIRI(subject, base string) string {
	if subject == "" {
		return ""
	}

	eid, err := message.ParseEntityID(subject)
	if err == nil {
		return fmt.Sprintf("%s/entities/%s/%s/%s/%s/%s/%s",
			base, eid.Org, eid.Platform, eid.System, eid.Domain, eid.Type, eid.Instance)
	}

	// Fallback: dot-to-slash
	path := strings.ReplaceAll(subject, ".", "/")
	return fmt.Sprintf("%s/subjects/%s", base, path)
}

// resolveSubjectIRI resolves a subject string to an IRI using the configured options.
func resolveSubjectIRI(subject string, opts *options) string {
	if opts.customSubjectIRIFn {
		return opts.subjectIRIFn(subject)
	}
	return subjectToIRI(subject, opts.baseIRI)
}

// resolvedTriple holds a triple that has been resolved to IRIs.
type resolvedTriple struct {
	predicateIRI string
	obj          classifiedObject
}

// groupTriples classifies and groups triples by subject IRI, preserving input order.
func groupTriples(triples []message.Triple, opts *options) (map[string][]resolvedTriple, []string) {
	groups := make(map[string][]resolvedTriple)
	var subjectOrder []string
	seen := make(map[string]bool)

	for _, t := range triples {
		if t.Subject == "" || t.Predicate == "" {
			continue
		}
		obj := classifyObject(t, opts)
		if obj.kind == objectInvalid {
			continue
		}

		subIRI := resolveSubjectIRI(t.Subject, opts)
		predIRI := resolvePredicateIRI(t.Predicate, opts.baseIRI)

		groups[subIRI] = append(groups[subIRI], resolvedTriple{
			predicateIRI: predIRI,
			obj:          obj,
		})

		if !seen[subIRI] {
			subjectOrder = append(subjectOrder, subIRI)
			seen[subIRI] = true
		}
	}
	return groups, subjectOrder
}

// scanPrefixes does a dry-run compaction of all IRIs in the groups to determine
// which prefixes need to be declared.
func scanPrefixes(groups map[string][]resolvedTriple, pm *prefixMap) {
	for subIRI, rts := range groups {
		pm.compact(subIRI)
		for _, rt := range rts {
			pm.compact(rt.predicateIRI)
			if rt.obj.kind == objectResource {
				pm.compact(rt.obj.iri)
			}
			if rt.obj.kind == objectLiteral && rt.obj.datatype != "" && rt.obj.datatype != xsdString {
				pm.compact(rt.obj.datatype)
			}
		}
	}
}

// resolvePredicateIRI converts a dotted predicate string to a full IRI.
// It first checks the vocabulary registry for a StandardIRI mapping.
// If not found, it generates a SemStreams predicate IRI from the dotted notation.
func resolvePredicateIRI(predicate, base string) string {
	if predicate == "" {
		return ""
	}

	// Check registry for standard mapping
	meta := vocabulary.GetPredicateMetadata(predicate)
	if meta != nil && meta.StandardIRI != "" {
		return meta.StandardIRI
	}

	// Generate from dotted notation: domain.category.property -> {base}/predicates/domain/category/property
	path := strings.ReplaceAll(predicate, ".", "/")
	return fmt.Sprintf("%s/predicates/%s", base, path)
}
