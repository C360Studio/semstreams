package swecommon

// ComponentKind is the SWE Common data-component discriminator. Every
// concrete [DataComponent] reports one of these so encoders and
// decoders can dispatch without reflection. The string values match
// the `type` discriminator field in OGC SWE Common JSON Encoding
// (22-022), so they double as the wire-format type tag.
type ComponentKind string

// SWE Common data-component kinds. KindRecord is the only composite
// kind covered in Phase 1; DataArray and DataChoice are deferred.
const (
	KindRecord   ComponentKind = "DataRecord"
	KindQuantity ComponentKind = "Quantity"
	KindCount    ComponentKind = "Count"
	KindTime     ComponentKind = "Time"
	KindBoolean  ComponentKind = "Boolean"
	KindText     ComponentKind = "Text"
	KindCategory ComponentKind = "Category"
)

// DataComponent is the sealed supertype for SWE Common data
// components. Implementations live in this package only — adding a
// new kind requires updating the encoders, decoders, and the
// register tables in schema.go, which would not compile otherwise.
type DataComponent interface {
	// Kind reports which SWE Common component this is.
	Kind() ComponentKind

	// Label is the human-readable name; empty when unset.
	Label() string

	// Definition is the IRI of the controlled concept that defines
	// this component's semantics (e.g. a QUDT QuantityKind or a
	// SWEET property concept). Empty when unset.
	Definition() string

	// sealed is unexported so external packages cannot implement
	// DataComponent. The sealed pattern is load-bearing for the
	// encoder dispatch table: every kind must be handled in this
	// package.
	sealed()
}

// CommonFields is the embedded base for every concrete component. It
// carries the label + definition + nilValue stand-in that every SWE
// component may optionally declare. JSON tag rules: omit when empty
// so a minimal Quantity{} round-trips to `{"type":"Quantity"}`.
type CommonFields struct {
	LabelValue      string `json:"label,omitempty"`
	DefinitionValue string `json:"definition,omitempty"`

	// NilValue is the wire-side stand-in token a producer emits when
	// the field is absent. Decoders treat any value equal to this
	// token (after format-specific normalization) as Go `nil`.
	// Per SWE Common §7.5 NilValues — Phase 1 supports a single
	// stand-in; the multi-reason NilValues block is deferred.
	NilValue string `json:"nilValue,omitempty"`
}

// Label implements [DataComponent].
func (c CommonFields) Label() string { return c.LabelValue }

// Definition implements [DataComponent].
func (c CommonFields) Definition() string { return c.DefinitionValue }

// Nil returns the optional wire-side nil-stand-in token.
func (c CommonFields) Nil() string { return c.NilValue }

// Quantity is a numeric value with a unit of measure (SWE Common
// §7.6.4). Round-trips through float64; producers needing higher
// precision (int64-fidelity counts, decimal-fixed) should declare a
// [Count] or carry the value as [Text] instead.
type Quantity struct {
	CommonFields

	// UoM is the unit of measure. Carry a UCUM symbol (e.g. "Cel",
	// "m/s") via Code OR a QUDT/OGC unit IRI via Href. Both empty
	// = dimensionless quantity, which is legal but lossy through
	// the SWE+csv encoding.
	UoMCode string `json:"uomCode,omitempty"`
	UoMHref string `json:"uomHref,omitempty"`
}

// Kind reports [KindQuantity].
func (Quantity) Kind() ComponentKind { return KindQuantity }
func (Quantity) sealed()             {}

// Count is a discrete integer count with no unit of measure (SWE
// Common §7.6.6). Round-trips through int64.
type Count struct {
	CommonFields
}

// Kind reports [KindCount].
func (Count) Kind() ComponentKind { return KindCount }
func (Count) sealed()             {}

// Time is a temporal instant (SWE Common §7.6.5). Round-trips
// through ISO 8601 / RFC 3339 strings — Phase 1 does not encode
// epoch-seconds doubles. Time-zone-frame and reference-frame are
// carried as IRIs; missing frame is treated as UTC by convention.
type Time struct {
	CommonFields

	// UoMHref carries the temporal reference IRI (e.g.
	// "http://www.opengis.net/def/uom/ISO-8601/0/Gregorian"). The
	// SWE JSON encoding emits this as uom.href on the wire.
	UoMHref string `json:"uomHref,omitempty"`

	// ReferenceFrame is the optional temporal frame IRI for
	// non-UTC time values. Empty = UTC.
	ReferenceFrame string `json:"referenceFrame,omitempty"`
}

// Kind reports [KindTime].
func (Time) Kind() ComponentKind { return KindTime }
func (Time) sealed()             {}

// Boolean is a true/false value (SWE Common §7.6.2). Round-trips
// through Go bool.
type Boolean struct {
	CommonFields
}

// Kind reports [KindBoolean].
func (Boolean) Kind() ComponentKind { return KindBoolean }
func (Boolean) sealed()             {}

// Text is a free-text string (SWE Common §7.6.3). Round-trips
// through Go string; binary encoding length-prefixes the UTF-8
// bytes with a uint32.
type Text struct {
	CommonFields
}

// Kind reports [KindText].
func (Text) Kind() ComponentKind { return KindText }
func (Text) sealed()             {}

// Category is a controlled-vocabulary token (SWE Common §7.6.7).
// CodeSpace is the IRI of the code list; values are short tokens
// drawn from that list. Round-trips through Go string.
type Category struct {
	CommonFields

	// CodeSpace is the IRI of the code list this category draws
	// from. Empty = free-form token (legal but limits validation).
	CodeSpace string `json:"codeSpace,omitempty"`
}

// Kind reports [KindCategory].
func (Category) Kind() ComponentKind { return KindCategory }
func (Category) sealed()             {}
