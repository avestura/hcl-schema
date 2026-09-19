package hclschema

import (
	"regexp"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

// SchemaExtension is the suffix that identifies a file as an hcl-schema
// document rather than an instance of one.
const SchemaExtension = ".schema.hcl"

// Schema is a parsed `*.schema.hcl` document.
type Schema struct {
	// Draft is the meta-schema version this document was written against.
	Draft Draft

	// SchemaRef and ID are the document's `__schema` and `__id` attributes.
	SchemaRef string
	ID        string

	// Body is the root body schema.
	Body *FullBodySchema

	// Filename is the name the document was parsed under.
	Filename string

	// Imports maps an alias to the schema imported under that alias.
	Imports map[string]*Schema
}

// FullBodySchema is the recursive counterpart of hcl.BodySchema: where
// hcl.BodySchema describes only the shallow shape of a body, this carries the
// full tree along with the type and constraint information that belongs to
// HCL's hcldec layer.
type FullBodySchema struct {
	Attributes []AttributeSchema
	Blocks     []BlockSchema

	// Open allows attributes and blocks that the schema does not declare.
	Open bool

	// Variants, when non-empty, requires the body to match exactly one of the
	// listed alternatives in addition to the declarations above.
	Variants []VariantSchema

	// DeclRange locates the `body` block that produced this schema.
	DeclRange hcl.Range
}

// VariantSchema is one alternative of a `one_of` block.
type VariantSchema struct {
	Name      string
	Body      *FullBodySchema
	DeclRange hcl.Range
}

// AttributeSchema describes a single attribute. Name and Required mirror
// hcl.AttributeSchema; Type and Default mirror hcldec.AttrSpec and
// hcldec.DefaultSpec; the remainder are hcl-schema's own additions.
type AttributeSchema struct {
	Name     string
	Required bool

	// Type is the type constraint, parsed from a `type` expression by
	// ext/typeexpr. cty.NilType means the attribute is untyped, which is how
	// every attribute behaved before draft 2026-09.
	Type cty.Type

	// Default is substituted when the attribute is absent. Only meaningful for
	// optional attributes.
	Default cty.Value

	Description string
	Deprecated  string
	Sensitive   bool

	// Enum restricts the value to one of a fixed set.
	Enum []cty.Value

	// Pattern is a regular expression the value must match. String-typed
	// attributes only.
	Pattern string

	// Min and Max bound a numeric value, a string's length, or a collection's
	// element count, depending on the attribute's type.
	Min *float64
	Max *float64

	// Cross-field constraints, each naming sibling attributes.
	ConflictsWith []string
	RequiredWith  []string
	ExactlyOneOf  []string
	AtLeastOneOf  []string

	// Validations are custom predicates evaluated with `self` bound to the
	// attribute's value.
	Validations []Validation

	DeclRange hcl.Range

	compiledPattern *regexp.Regexp
}

// Validation is a custom predicate attached to an attribute.
type Validation struct {
	Condition    hcl.Expression
	ErrorMessage string
	DeclRange    hcl.Range

	// ConditionSrc is the condition's original source text, captured at parse
	// time so the rule can be rendered back out without re-reading the file.
	ConditionSrc string
}

// BlockSchema describes a block. Type and LabelNames mirror
// hcl.BlockHeaderSchema; Required, MinItems and MaxItems mirror
// hcldec.BlockSpec and hcldec.BlockListSpec.
type BlockSchema struct {
	Type       string
	LabelNames []string

	// Labels carries per-label constraints. When present it has the same
	// length as LabelNames.
	Labels []LabelSchema

	// Body is the nested body schema, or nil when the block declares none.
	// Under draft 2025-10 a nil Body accepts arbitrary content; from draft
	// 2026-09 it means the block must be empty.
	Body *FullBodySchema

	Required     bool
	MinItems     int
	MaxItems     int
	UniqueLabels bool

	Description string
	Deprecated  string

	DeclRange hcl.Range
}

// LabelSchema constrains one positional label of a block.
type LabelSchema struct {
	Name        string
	Description string
	Pattern     string
	Enum        []string

	compiledPattern *regexp.Regexp
}

// AsBodySchema projects onto HCL's own shallow body schema. This is the
// original core profile and is deliberately lossless in the other direction:
// a schema that uses no keys beyond `required` and `label_names` round-trips
// through it unchanged.
func (fbs *FullBodySchema) AsBodySchema() *hcl.BodySchema {
	if fbs == nil {
		return &hcl.BodySchema{}
	}
	attrs := make([]hcl.AttributeSchema, 0, len(fbs.Attributes))
	for _, a := range fbs.Attributes {
		attrs = append(attrs, hcl.AttributeSchema{Name: a.Name, Required: a.Required})
	}
	// hcl.BodySchema keys blocks by type alone (hclsyntax builds a
	// map[string]BlockHeaderSchema), so a type can appear only once. Keeping
	// the first declaration matches what Block reports.
	blocks := make([]hcl.BlockHeaderSchema, 0, len(fbs.Blocks))
	seen := make(map[string]bool, len(fbs.Blocks))
	for _, b := range fbs.Blocks {
		if seen[b.Type] {
			continue
		}
		seen[b.Type] = true
		blocks = append(blocks, hcl.BlockHeaderSchema{Type: b.Type, LabelNames: b.LabelNames})
	}
	return &hcl.BodySchema{Attributes: attrs, Blocks: blocks}
}

// Attribute returns the declaration of the named attribute, or nil.
func (fbs *FullBodySchema) Attribute(name string) *AttributeSchema {
	if fbs == nil {
		return nil
	}
	for i := range fbs.Attributes {
		if fbs.Attributes[i].Name == name {
			return &fbs.Attributes[i]
		}
	}
	return nil
}

// Block returns the declaration for a block type, preferring one whose label
// count matches. The fallback exists only for draft 2025-10 documents that
// declare a type more than once; from draft 2026-09 that is an error.
func (fbs *FullBodySchema) Block(typ string, labelCount int) *BlockSchema {
	if fbs == nil {
		return nil
	}
	var fallback *BlockSchema
	for i := range fbs.Blocks {
		if fbs.Blocks[i].Type != typ {
			continue
		}
		if len(fbs.Blocks[i].LabelNames) == labelCount {
			return &fbs.Blocks[i]
		}
		if fallback == nil {
			fallback = &fbs.Blocks[i]
		}
	}
	return fallback
}

// BlocksByType returns every declaration for a block type, across arities.
func (fbs *FullBodySchema) BlocksByType(typ string) []*BlockSchema {
	if fbs == nil {
		return nil
	}
	var out []*BlockSchema
	for i := range fbs.Blocks {
		if fbs.Blocks[i].Type == typ {
			out = append(out, &fbs.Blocks[i])
		}
	}
	return out
}

// BlockHeaderAndBodySchema is the original root type returned by
// ParseSchemaFile.
//
// Deprecated: use Schema, which additionally carries the draft, the document
// identity and any imports. This type is retained so that code written against
// the pre-2026-09 API keeps compiling.
type BlockHeaderAndBodySchema struct {
	hcl.BlockHeaderSchema

	BodySchema *FullBodySchema
}
