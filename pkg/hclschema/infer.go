package hclschema

import (
	"fmt"
	"os"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// InferOptions controls schema inference.
type InferOptions struct {
	// ID becomes the generated schema's `__id`.
	ID string

	// RequireUbiquitous marks an attribute required when every sample carries
	// it. With a single sample that makes everything required, which is rarely
	// what you want, so it is off by default.
	RequireUbiquitous bool
}

// Infer derives a starting schema from existing HCL documents.
//
// The result is a draft, not an answer: inference can only describe what the
// samples happen to contain. It exists so that adopting hcl-schema for a
// config format that already has files does not begin with a blank page.
//
// Inference reads the native-syntax parse tree directly, so JSON-syntax
// samples are reported and skipped.
func Infer(files []*hcl.File, opts InferOptions) (*Schema, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	bodies := make([]*hclsyntax.Body, 0, len(files))
	for _, f := range files {
		if f == nil {
			continue
		}
		b, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagWarning,
				Summary:  "Sample skipped",
				Detail:   "Inference reads the native-syntax parse tree, so JSON-syntax samples are ignored.",
			})
			continue
		}
		bodies = append(bodies, b)
	}
	if len(bodies) == 0 {
		return nil, append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Nothing to infer from",
			Detail:   "No native-syntax HCL samples were supplied.",
		})
	}

	id := opts.ID
	if id == "" {
		id = "local://inferred"
	}
	schema := &Schema{
		Draft:     LatestDraft,
		SchemaRef: LatestDraft.URL(),
		ID:        id,
		Imports:   map[string]*Schema{},
		Body:      inferBody(bodies, opts),
	}
	return schema, diags
}

// InferFromPaths parses each path and infers a schema from the set.
func InferFromPaths(paths []string, opts InferOptions) (*Schema, hcl.Diagnostics) {
	var (
		diags hcl.Diagnostics
		files []*hcl.File
	)
	parser := hclparse.NewParser()
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Failed to read file",
				Detail:   fmt.Sprintf("%s could not be read: %s.", p, err),
			})
			continue
		}
		f, d := parseSource(parser, src, p)
		diags = append(diags, d...)
		if !d.HasErrors() {
			files = append(files, f)
		}
	}
	if diags.HasErrors() {
		return nil, diags
	}
	schema, d := Infer(files, opts)
	return schema, append(diags, d...)
}

func inferBody(bodies []*hclsyntax.Body, opts InferOptions) *FullBodySchema {
	out := &FullBodySchema{}

	type attrInfo struct {
		count int
		types []cty.Type
		order int
	}
	attrs := map[string]*attrInfo{}

	type blockInfo struct {
		labelCount  int
		samples     []*hclsyntax.Body
		filesWithIt int
		order       int
	}
	blocks := map[string]*blockInfo{}

	next := 0
	for _, body := range bodies {
		names := make([]string, 0, len(body.Attributes))
		for name := range body.Attributes {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			if isLinkAttribute(name) {
				continue
			}
			info := attrs[name]
			if info == nil {
				info = &attrInfo{order: next}
				next++
				attrs[name] = info
			}
			info.count++
			if v, d := body.Attributes[name].Expr.Value(nil); !d.HasErrors() && !v.IsNull() {
				info.types = append(info.types, v.Type())
			}
		}

		seenHere := map[string]bool{}
		for _, blk := range body.Blocks {
			info := blocks[blk.Type]
			if info == nil {
				info = &blockInfo{labelCount: len(blk.Labels), order: next}
				next++
				blocks[blk.Type] = info
			}
			if len(blk.Labels) != info.labelCount {
				// Arities disagree across samples; HCL can only describe one,
				// so the smaller is kept and the difference is left to the
				// author.
				if len(blk.Labels) < info.labelCount {
					info.labelCount = len(blk.Labels)
				}
			}
			info.samples = append(info.samples, blk.Body)
			if !seenHere[blk.Type] {
				seenHere[blk.Type] = true
				info.filesWithIt++
			}
		}
	}

	ordered := make([]string, 0, len(attrs))
	for name := range attrs {
		ordered = append(ordered, name)
	}
	sort.Slice(ordered, func(i, j int) bool { return attrs[ordered[i]].order < attrs[ordered[j]].order })
	for _, name := range ordered {
		info := attrs[name]
		a := AttributeSchema{
			Name:     name,
			Type:     unifyTypes(info.types),
			Default:  cty.NilVal,
			Required: opts.RequireUbiquitous && info.count == len(bodies),
		}
		out.Attributes = append(out.Attributes, a)
	}

	orderedBlocks := make([]string, 0, len(blocks))
	for typ := range blocks {
		orderedBlocks = append(orderedBlocks, typ)
	}
	sort.Slice(orderedBlocks, func(i, j int) bool {
		return blocks[orderedBlocks[i]].order < blocks[orderedBlocks[j]].order
	})
	for _, typ := range orderedBlocks {
		info := blocks[typ]
		b := BlockSchema{
			Type:       typ,
			LabelNames: inferLabelNames(info.labelCount),
			Body:       inferBody(info.samples, opts),
			Required:   opts.RequireUbiquitous && info.filesWithIt == len(bodies),
		}
		if b.Required {
			b.MinItems = 1
		}
		out.Blocks = append(out.Blocks, b)
	}

	return out
}

func inferLabelNames(n int) []string {
	switch n {
	case 0:
		return nil
	case 1:
		return []string{"name"}
	}
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, fmt.Sprintf("label%d", i))
	}
	return out
}

// unifyTypes returns a type only when every observed value agreed on one.
// Guessing a broader type from disagreeing samples would bake a wrong
// constraint into the generated schema.
func unifyTypes(types []cty.Type) cty.Type {
	if len(types) == 0 {
		return cty.NilType
	}
	first := types[0]
	for _, t := range types[1:] {
		if !t.Equals(first) {
			return cty.NilType
		}
	}
	if first == cty.DynamicPseudoType {
		return cty.NilType
	}
	return first
}

func isLinkAttribute(name string) bool {
	for _, l := range linkAttributes {
		if l == name {
			return true
		}
	}
	return false
}
