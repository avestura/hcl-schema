package hclschema

import (
	"encoding/json"
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

// DecodeSpec builds the hcldec.Spec that corresponds to this schema.
//
// This is the projection onto HCL's decoding layer, the counterpart of
// FullBodySchema.AsBodySchema. Where AsBodySchema answers "what shape is this
// body", the spec additionally carries types, defaults and cardinality, and so
// can produce a value rather than only a verdict.
//
// A schema that is recursive through `ref` cannot be represented as a spec,
// because a spec is a finite tree. Recursion is cut at the point it closes and
// reported as a warning.
func (s *Schema) DecodeSpec() (hcldec.Spec, hcl.Diagnostics) {
	if s == nil || s.Body == nil {
		return hcldec.ObjectSpec{}, nil
	}
	b := &specBuilder{onPath: map[*FullBodySchema]bool{}}
	spec := b.body(s.Body, "")
	return spec, b.diags
}

type specBuilder struct {
	onPath map[*FullBodySchema]bool
	diags  hcl.Diagnostics
}

func (b *specBuilder) body(fbs *FullBodySchema, path string) hcldec.Spec {
	out := hcldec.ObjectSpec{}
	if fbs == nil {
		return out
	}
	if b.onPath[fbs] {
		b.diags = append(b.diags, &hcl.Diagnostic{
			Severity: hcl.DiagWarning,
			Summary:  "Recursive schema truncated",
			Detail: fmt.Sprintf("The schema recurses at %q. A decode spec is a finite tree, so the recursion is cut here and nested content is not decoded.",
				path),
			Subject: fbs.DeclRange.Ptr(),
		})
		return out
	}
	b.onPath[fbs] = true
	defer delete(b.onPath, fbs)

	if len(fbs.Variants) > 0 {
		b.diags = append(b.diags, &hcl.Diagnostic{
			Severity: hcl.DiagWarning,
			Summary:  "Variants not decoded",
			Detail:   "A `one_of` block constrains validation but has no single decoded shape, so its variant-only declarations are omitted from the spec.",
			Subject:  fbs.DeclRange.Ptr(),
		})
	}

	for i := range fbs.Attributes {
		a := &fbs.Attributes[i]
		typ := a.Type
		if typ == cty.NilType {
			typ = cty.DynamicPseudoType
		}
		var spec hcldec.Spec = &hcldec.AttrSpec{
			Name:     a.Name,
			Type:     typ,
			Required: a.Required,
		}
		if a.Default != cty.NilVal {
			spec = &hcldec.DefaultSpec{
				Primary: spec,
				Default: &hcldec.LiteralSpec{Value: a.Default},
			}
		}
		out[a.Name] = spec
	}

	for i := range fbs.Blocks {
		blk := &fbs.Blocks[i]
		child := path + "/" + blk.Type

		nested := hcldec.ObjectSpec{}
		if inner, ok := b.body(blk.Body, child).(hcldec.ObjectSpec); ok {
			for k, v := range inner {
				nested[k] = v
			}
		}
		for li, name := range blk.LabelNames {
			if name == "" {
				name = fmt.Sprintf("label%d", li)
			}
			nested[name] = &hcldec.BlockLabelSpec{Index: li, Name: name}
		}

		// A block capped at one instance decodes to a single object; anything
		// else decodes to a list, which is what BlockListSpec's MinItems and
		// MaxItems are for.
		if blk.MaxItems == 1 {
			out[blk.Type] = &hcldec.BlockSpec{
				TypeName: blk.Type,
				Nested:   nested,
				Required: blk.MinItems > 0,
			}
			continue
		}
		out[blk.Type] = &hcldec.BlockListSpec{
			TypeName: blk.Type,
			Nested:   nested,
			MinItems: blk.MinItems,
			MaxItems: blk.MaxItems,
		}
	}

	return out
}

// Decode produces a cty value for a body, applying declared types and
// defaults. ctx may be nil.
func (s *Schema) Decode(body hcl.Body, ctx *hcl.EvalContext) (cty.Value, hcl.Diagnostics) {
	spec, diags := s.DecodeSpec()
	if diags.HasErrors() {
		return cty.NilVal, diags
	}
	// The reserved link attributes are not part of the decoded shape, so they
	// are peeled off before decoding rather than surfacing as unexpected
	// arguments.
	body = withoutLinkAttributes(body)
	val, d := hcldec.Decode(body, spec, ctx)
	return val, append(diags, d...)
}

// DecodeInto decodes a body and unmarshals the result into target, which is
// any value encoding/json can populate. Field names follow the schema's
// attribute and block names unless json tags say otherwise.
func (s *Schema) DecodeInto(body hcl.Body, ctx *hcl.EvalContext, target any) hcl.Diagnostics {
	val, diags := s.Decode(body, ctx)
	if diags.HasErrors() {
		return diags
	}
	raw, err := ctyjson.Marshal(val, val.Type())
	if err != nil {
		return append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Failed to encode decoded value",
			Detail:   err.Error(),
		})
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Failed to decode into target",
			Detail:   err.Error(),
		})
	}
	return diags
}

// withoutLinkAttributes hides the reserved `__`-prefixed link attributes from
// a decode pass.
func withoutLinkAttributes(body hcl.Body) hcl.Body {
	probe := &hcl.BodySchema{}
	for _, name := range linkAttributes {
		probe.Attributes = append(probe.Attributes, hcl.AttributeSchema{Name: name})
	}
	_, remain, _ := body.PartialContent(probe)
	if remain == nil {
		return body
	}
	return remain
}
