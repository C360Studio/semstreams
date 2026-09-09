# RDF Export

Serializes `[]message.Triple` to standard RDF formats using the vocabulary registry for IRI resolution.

## Supported Formats

| Format | Extension | Description |
|--------|-----------|-------------|
| `Turtle` | `.ttl` | Compact, human-readable with prefix declarations and subject grouping |
| `NTriples` | `.nt` | Line-based, one triple per line, fully expanded IRIs |
| `JSONLD` | `.jsonld` | JSON with `@context` and `@graph` for web APIs |

## Usage

```go
import "github.com/c360studio/semstreams/vocabulary/export"

triples := []message.Triple{
    {Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.battery.level", Object: 85.5},
    {Subject: "acme.ops.robotics.gcs.drone.001", Predicate: "robotics.flight.armed", Object: true},
}

// Write Turtle to a writer
err := export.Serialize(os.Stdout, triples, export.Turtle)

// Get N-Triples as a string
s, err := export.SerializeToString(triples, export.NTriples)
```

## IRI Resolution

The package resolves dotted-notation identifiers to IRIs automatically:

**Subjects** are converted based on entity ID structure:

- Valid 6-part entity IDs: `acme.ops.robotics.gcs.drone.001` becomes `{base}/entities/acme/ops/robotics/gcs/drone/001`
- Other subjects: dots are converted to slashes under `{base}/subjects/`

**Predicates** resolve through the vocabulary registry:

- Registered predicates use their `StandardIRI` (e.g., `robotics.battery.level` with a registered IRI maps to that IRI)
- Unregistered predicates generate `{base}/predicates/domain/category/property`

**Objects** are classified by a total order, most specific first (gh#1267):

1. **the triple's own `Datatype` hint** — a statement about THIS value;
2. **the predicate's declared `DataType`** — a statement about the PREDICATE, and the only place a fact the
   authoritative JSON round trip erased can come from;
3. **observation of the Go type** — what the serializer can see for itself.

Observation is strictly the fallback, never a competing authority — but a declaration the observed value cannot
carry is **ignored for that triple** rather than applied. This package is a serializer: it renders what is there,
and never emits a lexical form the observed value does not support.

Declared datatype → RDF. This table is the whole semantic-web surface of the declaration vocabulary; ADR-107 keeps
it here, at the edge, and out of the registry:

| Declared (`vocabulary.DataType*`) | RDF Datatype | Output |
|---|---|---|
| `entity_id` | Resource | `<iri>` |
| `string` | `xsd:string` | `"text"` (datatype omitted) |
| `int` | `xsd:integer` | `5` |
| `float` | `xsd:double` | `"85.5"^^xsd:double` |
| `bool` | `xsd:boolean` | `true` |
| `datetime` | `xsd:dateTime` | `"2024-01-15T10:30:00Z"^^xsd:dateTime` |
| `json` | `rdf:JSON` | `"{\"k\":1}"^^rdf:JSON` |

Go type → RDF, for a predicate that declares nothing:

| Go Type | RDF Datatype | Output |
|---------|-------------|--------|
| `string` (entity ID) | Resource | `<iri>` |
| `string` (absolute IRI: `http://`, `https://`, `urn:`) | Resource | `<iri>` |
| `string` (other) | `xsd:string` | `"text"` |
| `int`, `int64`, etc. | `xsd:integer` | `42` |
| `float64` | `xsd:double` | `"85.5"^^xsd:double` |
| `bool` | `xsd:boolean` | `true` |
| `time.Time` | `xsd:dateTime` | `"2024-01-15T10:30:00Z"^^xsd:dateTime` |

A per-triple `message.EntityReferenceDatatype` (`"@id"`) marks an object as an entity reference and serializes as an
IRI node, exactly as a predicate declared `entity_id` does. Prefixed datatypes are expanded for `xsd:` and `rdf:`;
any other prefix reaches the output unexpanded.

## Options

### `WithBaseIRI`

Override the default base IRI used for generated subject and predicate URIs:

```go
err := export.Serialize(w, triples, export.Turtle,
    export.WithBaseIRI("https://example.org"))
```

### `WithSubjectIRIFunc`

Provide a custom function to map entity ID strings to IRIs. When set, this replaces the default subject IRI generation entirely (`WithBaseIRI` has no effect on subjects):

```go
err := export.Serialize(w, triples, export.JSONLD,
    export.WithSubjectIRIFunc(func(subject string) string {
        return "https://example.org/entities/" + subject
    }))
```

## Prefix Management

Turtle and JSON-LD output automatically compact IRIs using well-known prefixes (OWL, SKOS, Dublin Core, PROV-O, Schema.org, FOAF, SSN/SOSA, XSD). Only prefixes that appear in the output are declared.

## API

| Function | Description |
|----------|-------------|
| `Serialize(w, triples, format, ...opts)` | Write triples to an `io.Writer` |
| `SerializeToString(triples, format, ...opts)` | Return serialized output as a string |

## Related Documentation

- [Vocabulary Package](../README.md) - Registry API, predicate registration, IRI mappings
- [Vocabulary Guide](../../docs/basics/04-vocabulary.md) - Predicate design and naming conventions
