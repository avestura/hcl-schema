package hclschema

import (
	"fmt"
	"io"
	"sort"

	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// Bytes renders the schema as a formatted `*.schema.hcl` document.
//
// Rendering goes through hclwrite, so the output is canonically formatted and
// a schema produced by Infer can be written straight to disk.
//
// The output is self-contained: bodies shared through `ref`, including ones
// that recurse, are emitted once with a generated `id` and referenced
// thereafter, so a schema assembled from imports round-trips without them.
func (s *Schema) Bytes() []byte {
	f := hclwrite.NewEmptyFile()
	root := f.Body()

	ref := s.SchemaRef
	if ref == "" {
		ref = LatestDraft.URL()
	}
	root.SetAttributeValue("__schema", cty.StringVal(ref))
	if s.ID != "" {
		root.SetAttributeValue("__id", cty.StringVal(s.ID))
	}
	root.AppendNewline()

	w := newSchemaWriter(s.Body)
	body := root.AppendNewBlock("body", nil)
	w.body(body.Body(), s.Body)
	return hclwrite.Format(f.Bytes())
}

// WriteHCL renders the schema to w.
func (s *Schema) WriteHCL(w io.Writer) error {
	_, err := w.Write(s.Bytes())
	return err
}

// schemaWriter renders a resolved schema tree. Because `ref` resolution
// replaces names with pointers, the same body can appear at several places in
// the tree, or contain itself. The writer finds those bodies first and gives
// each a stable id, then emits the body once and a `ref` everywhere else.
type schemaWriter struct {
	ids     map[*FullBodySchema]string
	written map[*FullBodySchema]bool
}

func newSchemaWriter(root *FullBodySchema) *schemaWriter {
	w := &schemaWriter{
		ids:     map[*FullBodySchema]string{},
		written: map[*FullBodySchema]bool{},
	}
	seen := map[*FullBodySchema]int{}
	shared := map[*FullBodySchema]bool{}
	onPath := map[*FullBodySchema]bool{}
	names := map[*FullBodySchema]string{}

	var walk func(fbs *FullBodySchema, name string)
	walk = func(fbs *FullBodySchema, name string) {
		if fbs == nil {
			return
		}
		seen[fbs]++
		if _, ok := names[fbs]; !ok {
			names[fbs] = name
		}
		if onPath[fbs] {
			shared[fbs] = true
			return
		}
		if seen[fbs] > 1 {
			shared[fbs] = true
			return
		}
		onPath[fbs] = true
		for i := range fbs.Blocks {
			walk(fbs.Blocks[i].Body, fbs.Blocks[i].Type)
		}
		for i := range fbs.Variants {
			walk(fbs.Variants[i].Body, fbs.Variants[i].Name)
		}
		delete(onPath, fbs)
	}
	walk(root, "root")

	// Sort so that generated ids are stable across runs.
	ordered := make([]*FullBodySchema, 0, len(shared))
	for fbs := range shared {
		ordered = append(ordered, fbs)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return names[ordered[i]]+ordered[i].DeclRange.String() < names[ordered[j]]+ordered[j].DeclRange.String()
	})
	used := map[string]bool{}
	for _, fbs := range ordered {
		base := names[fbs]
		if base == "" {
			base = "body"
		}
		candidate := base + "Ref"
		for n := 2; used[candidate]; n++ {
			candidate = fmt.Sprintf("%sRef%d", base, n)
		}
		used[candidate] = true
		w.ids[fbs] = candidate
	}
	return w
}

func (w *schemaWriter) body(out *hclwrite.Body, fbs *FullBodySchema) {
	if fbs == nil {
		return
	}
	if fbs.Open {
		out.SetAttributeValue("open", cty.BoolVal(true))
	}

	for i := range fbs.Attributes {
		a := &fbs.Attributes[i]
		blk := out.AppendNewBlock("attribute", []string{a.Name})
		writeAttribute(blk.Body(), a)
	}

	for i := range fbs.Blocks {
		b := &fbs.Blocks[i]
		blk := out.AppendNewBlock("block_header", []string{b.Type})
		w.block(blk.Body(), b)
	}

	if len(fbs.Variants) > 0 {
		oneOf := out.AppendNewBlock("one_of", nil)
		for i := range fbs.Variants {
			v := &fbs.Variants[i]
			vb := oneOf.Body().AppendNewBlock("variant", []string{v.Name})
			inner := vb.Body().AppendNewBlock("body", nil)
			w.body(inner.Body(), v.Body)
		}
	}
}

func (w *schemaWriter) block(out *hclwrite.Body, b *BlockSchema) {
	if b.Description != "" {
		out.SetAttributeValue("description", cty.StringVal(b.Description))
	}
	setStringList(out, "label_names", b.LabelNames)
	if b.Required {
		out.SetAttributeValue("required", cty.BoolVal(true))
	} else if b.MinItems > 0 {
		out.SetAttributeValue("min_items", cty.NumberIntVal(int64(b.MinItems)))
	}
	if b.MaxItems > 0 {
		out.SetAttributeValue("max_items", cty.NumberIntVal(int64(b.MaxItems)))
	}
	if b.UniqueLabels {
		out.SetAttributeValue("unique_labels", cty.BoolVal(true))
	}
	if b.Deprecated != "" {
		out.SetAttributeValue("deprecated", cty.StringVal(b.Deprecated))
	}

	for _, l := range b.Labels {
		if l.Pattern == "" && len(l.Enum) == 0 && l.Description == "" {
			continue
		}
		blk := out.AppendNewBlock("label", []string{l.Name})
		if l.Description != "" {
			blk.Body().SetAttributeValue("description", cty.StringVal(l.Description))
		}
		if l.Pattern != "" {
			blk.Body().SetAttributeValue("pattern", cty.StringVal(l.Pattern))
		}
		setStringList(blk.Body(), "enum", l.Enum)
	}

	if b.Body == nil {
		return
	}

	if id, shared := w.ids[b.Body]; shared {
		if w.written[b.Body] {
			out.SetAttributeRaw("ref", rawTokens("block_header."+id))
			return
		}
		w.written[b.Body] = true
		out.SetAttributeValue("id", cty.StringVal(id))
	}

	inner := out.AppendNewBlock("body", nil)
	w.body(inner.Body(), b.Body)
}

func writeAttribute(out *hclwrite.Body, a *AttributeSchema) {
	if a.Description != "" {
		out.SetAttributeValue("description", cty.StringVal(a.Description))
	}
	if a.Type != cty.NilType {
		out.SetAttributeRaw("type", rawTokens(typeexpr.TypeString(a.Type)))
	}
	if a.Required {
		out.SetAttributeValue("required", cty.BoolVal(true))
	}
	if a.Default != cty.NilVal {
		out.SetAttributeValue("default", a.Default)
	}
	if a.Sensitive {
		out.SetAttributeValue("sensitive", cty.BoolVal(true))
	}
	if a.Deprecated != "" {
		out.SetAttributeValue("deprecated", cty.StringVal(a.Deprecated))
	}
	if len(a.Enum) > 0 {
		out.SetAttributeValue("enum", cty.TupleVal(a.Enum))
	}
	if a.Pattern != "" {
		out.SetAttributeValue("pattern", cty.StringVal(a.Pattern))
	}
	if a.Min != nil {
		out.SetAttributeValue("min", cty.NumberFloatVal(*a.Min))
	}
	if a.Max != nil {
		out.SetAttributeValue("max", cty.NumberFloatVal(*a.Max))
	}
	setStringList(out, "conflicts_with", a.ConflictsWith)
	setStringList(out, "required_with", a.RequiredWith)
	setStringList(out, "exactly_one_of", a.ExactlyOneOf)
	setStringList(out, "at_least_one_of", a.AtLeastOneOf)

	for _, v := range a.Validations {
		blk := out.AppendNewBlock("validation", nil)
		if v.ConditionSrc != "" {
			blk.Body().SetAttributeRaw("condition", rawTokens(v.ConditionSrc))
		}
		blk.Body().SetAttributeValue("error_message", cty.StringVal(v.ErrorMessage))
	}
}

func setStringList(out *hclwrite.Body, name string, items []string) {
	if len(items) == 0 {
		return
	}
	vals := make([]cty.Value, 0, len(items))
	for _, i := range items {
		vals = append(vals, cty.StringVal(i))
	}
	out.SetAttributeValue(name, cty.TupleVal(vals))
}

func rawTokens(src string) hclwrite.Tokens {
	return hclwrite.Tokens{{
		Type:  hclsyntax.TokenIdent,
		Bytes: []byte(src),
	}}
}
