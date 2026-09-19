package hclschema

import (
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	godschema "github.com/avestura/hcl-schema/pkg/hclschema/god_schema"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

// DefaultMaxImportDepth bounds how far `import` chains may be followed.
const DefaultMaxImportDepth = 16

// ParseOptions controls how a schema document is parsed.
type ParseOptions struct {
	// Loader resolves `import` sources. When nil, DefaultLoader is used.
	Loader *Loader

	// Strict applies draft 2026-09 rules regardless of the draft the document
	// pins through `__schema`.
	Strict bool

	// MaxDepth bounds `import` recursion. Zero means DefaultMaxImportDepth.
	MaxDepth int
}

// parseState is threaded through a single parse, including imports.
type parseState struct {
	opts     ParseOptions
	baseDir  string
	depth    int
	visiting map[string]bool

	// ids maps a reference key such as "block_header.foo" to the body it names.
	ids map[string]*FullBodySchema

	// pending records refs to resolve once the whole document is built.
	pending []*pendingRef

	// src is the document's source, used to capture expression text without
	// re-reading the file from disk.
	src []byte
}

// pendingRef names a block whose body comes from another declaration's id.
// It records where the block ended up rather than a pointer to it: blocks are
// stored by value, so a pointer taken while the block was still a local would
// write into a copy that never reaches the tree.
type pendingRef struct {
	key   string
	rng   hcl.Range
	owner *FullBodySchema
	index int
}

// ParseSchema parses a schema document from memory.
func ParseSchema(src []byte, filename string) (*Schema, hcl.Diagnostics) {
	return ParseSchemaOpts(src, filename, ParseOptions{})
}

// ParseSchemaOpts parses a schema document from memory with explicit options.
func ParseSchemaOpts(src []byte, filename string, opts ParseOptions) (*Schema, hcl.Diagnostics) {
	parser := hclparse.NewParser()
	file, diags := parseSource(parser, src, filename)
	if diags.HasErrors() {
		return nil, diags
	}
	base := dirOf(filename)
	if base == "." {
		base = ""
	}
	st := newParseState(opts, base)
	schema, d := parseSchemaFile(file, filename, st)
	return schema, append(diags, d...)
}

// ParseSchemaFile parses a schema document from disk.
//
// Note: as of draft 2026-09 this returns *Schema rather than the original
// *BlockHeaderAndBodySchema. Call Schema.Legacy for the previous shape.
func ParseSchemaFile(filename string) (*Schema, hcl.Diagnostics) {
	return ParseSchemaFileOpts(filename, ParseOptions{})
}

// ParseSchemaFileOpts parses a schema document from disk with explicit options.
func ParseSchemaFileOpts(filename string, opts ParseOptions) (*Schema, hcl.Diagnostics) {
	src, err := os.ReadFile(filename)
	if err != nil {
		return nil, hcl.Diagnostics{{
			Severity: hcl.DiagError,
			Summary:  "Failed to read file",
			Detail:   fmt.Sprintf("The schema file %q could not be read: %s.", filename, err),
		}}
	}
	return ParseSchemaOpts(src, filename, opts)
}

// ParseSchemaFS parses a schema document from an fs.FS. Imports with relative
// sources are resolved within the same filesystem.
func ParseSchemaFS(fsys fs.FS, name string, opts ParseOptions) (*Schema, hcl.Diagnostics) {
	src, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, hcl.Diagnostics{{
			Severity: hcl.DiagError,
			Summary:  "Failed to read file",
			Detail:   fmt.Sprintf("The schema file %q could not be read: %s.", name, err),
		}}
	}
	if opts.Loader == nil {
		clone := *DefaultLoader
		opts.Loader = &clone
	}
	opts.Loader.FS = fsys
	return ParseSchemaOpts(src, name, opts)
}

func newParseState(opts ParseOptions, baseDir string) *parseState {
	if opts.Loader == nil {
		opts.Loader = DefaultLoader
	}
	if opts.MaxDepth == 0 {
		opts.MaxDepth = DefaultMaxImportDepth
	}
	return &parseState{
		opts:     opts,
		baseDir:  baseDir,
		visiting: map[string]bool{},
		ids:      map[string]*FullBodySchema{},
	}
}

// parseSource routes to the JSON or native-syntax parser by filename, the same
// way hclparse.Parser.ParseFile does.
func parseSource(parser *hclparse.Parser, src []byte, filename string) (*hcl.File, hcl.Diagnostics) {
	if strings.HasSuffix(filename, ".json") {
		return parser.ParseJSON(src, filename)
	}
	return parser.ParseHCL(src, filename)
}

func parseSchemaFile(file *hcl.File, filename string, st *parseState) (*Schema, hcl.Diagnostics) {
	content, diags := file.Body.Content(godschema.GetRootSchema())
	if content == nil {
		return nil, diags
	}

	st.src = file.Bytes
	schema := &Schema{Filename: filename, Imports: map[string]*Schema{}}

	if a, ok := content.Attributes["__schema"]; ok {
		v, d := stringAttr(a)
		diags = append(diags, d...)
		schema.SchemaRef = v
	}
	if a, ok := content.Attributes["__id"]; ok {
		v, d := stringAttr(a)
		diags = append(diags, d...)
		schema.ID = v
	}
	schema.Draft = DraftFromRef(schema.SchemaRef).effective(st.opts.Strict)

	// Imports are resolved before bodies so that their ids are available to
	// refs anywhere in this document.
	for _, blk := range content.Blocks {
		if blk.Type != "import" {
			continue
		}
		diags = append(diags, st.parseImport(schema, blk)...)
	}

	var root *FullBodySchema
	bodyCount := 0
	for _, blk := range content.Blocks {
		if blk.Type != "body" {
			continue
		}
		bodyCount++
		if bodyCount > 1 {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Duplicate body block",
				Detail:   "A schema document declares exactly one root `body` block.",
				Subject:  blk.DefRange.Ptr(),
			})
			continue
		}
		var d hcl.Diagnostics
		root, d = st.parseBody(blk.Body, blk.DefRange, schema.Draft)
		diags = append(diags, d...)
	}
	if root == nil {
		root = &FullBodySchema{}
	}
	schema.Body = root

	diags = append(diags, st.resolveRefs()...)
	return schema, diags
}

func (st *parseState) parseImport(schema *Schema, blk *hcl.Block) hcl.Diagnostics {
	content, diags := blk.Body.Content(godschema.GetImportSchema())
	if content == nil {
		return diags
	}
	alias := ""
	if len(blk.Labels) > 0 {
		alias = blk.Labels[0]
	}
	var source, sum string
	if a, ok := content.Attributes["source"]; ok {
		v, d := stringAttr(a)
		diags = append(diags, d...)
		source = v
	}
	if a, ok := content.Attributes["sha256"]; ok {
		v, d := stringAttr(a)
		diags = append(diags, d...)
		sum = v
	}
	if alias == "" || source == "" {
		return diags
	}

	if st.depth+1 > st.opts.MaxDepth {
		return append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Import depth exceeded",
			Detail:   fmt.Sprintf("Imports are nested more than %d levels deep, which usually means a cycle.", st.opts.MaxDepth),
			Subject:  blk.DefRange.Ptr(),
		})
	}

	key := st.opts.Loader.canonicalKey(source, st.baseDir)
	if st.visiting[key] {
		return append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Import cycle",
			Detail:   fmt.Sprintf("Schema %q is already being imported further up the chain.", source),
			Subject:  blk.DefRange.Ptr(),
		})
	}

	src, resolvedName, d := st.opts.Loader.Load(source, st.baseDir, sum)
	diags = append(diags, d...)
	if d.HasErrors() {
		return diags
	}

	sub := newParseState(st.opts, dirOf(resolvedName))
	sub.depth = st.depth + 1
	for k, v := range st.visiting {
		sub.visiting[k] = v
	}
	sub.visiting[key] = true

	parser := hclparse.NewParser()
	file, pd := parseSource(parser, src, resolvedName)
	diags = append(diags, pd...)
	if pd.HasErrors() {
		return diags
	}
	imported, id := parseSchemaFile(file, resolvedName, sub)
	diags = append(diags, id...)
	if imported == nil {
		return diags
	}
	schema.Imports[alias] = imported

	// Re-export the imported document's ids under the alias.
	for k, v := range sub.ids {
		st.ids[alias+"."+k] = v
	}
	return diags
}

func (st *parseState) parseBody(body hcl.Body, declRange hcl.Range, draft Draft) (*FullBodySchema, hcl.Diagnostics) {
	fbs := &FullBodySchema{DeclRange: declRange}
	return fbs, st.fillBody(fbs, body, draft)
}

// fillBody populates an existing FullBodySchema. Filling in place, rather than
// returning a fresh value, is what lets a block register its id before its own
// body is parsed, which is how self-recursive schemas resolve.
func (st *parseState) fillBody(fbs *FullBodySchema, body hcl.Body, draft Draft) hcl.Diagnostics {
	content, diags := body.Content(godschema.GetBodySchema())
	if content == nil {
		return diags
	}

	if a, ok := content.Attributes["open"]; ok {
		v, d := boolAttr(a)
		diags = append(diags, d...)
		fbs.Open = v
	}

	seenAttr := map[string]hcl.Range{}
	seenBlock := map[string]hcl.Range{}

	for _, blk := range content.Blocks {
		switch blk.Type {
		case "attribute":
			attr, d := st.parseAttribute(blk, draft)
			diags = append(diags, d...)
			if attr == nil {
				continue
			}
			if prev, dup := seenAttr[attr.Name]; dup {
				diags = append(diags, duplicateDiag(draft, "attribute", attr.Name, blk.DefRange, prev))
				if draft.Strict() {
					continue
				}
			}
			seenAttr[attr.Name] = blk.DefRange
			fbs.Attributes = append(fbs.Attributes, *attr)

		case "block_header":
			blkSchema, ref, d := st.parseBlockHeader(blk, draft)
			diags = append(diags, d...)
			if blkSchema == nil {
				continue
			}
			// A block type may be declared only once. hcl.BodySchema cannot
			// represent the same type at two label arities, so allowing it
			// would mean advertising a rule the parser cannot enforce.
			if prev, dup := seenBlock[blkSchema.Type]; dup {
				name := fmt.Sprintf("%q", blkSchema.Type)
				diags = append(diags, duplicateDiag(draft, "block", name, blk.DefRange, prev))
				if draft.Strict() {
					continue
				}
			}
			seenBlock[blkSchema.Type] = blk.DefRange
			fbs.Blocks = append(fbs.Blocks, *blkSchema)
			if ref != nil {
				ref.owner = fbs
				ref.index = len(fbs.Blocks) - 1
				st.pending = append(st.pending, ref)
			}

		case "one_of":
			variants, d := st.parseOneOf(blk, draft)
			diags = append(diags, d...)
			fbs.Variants = append(fbs.Variants, variants...)
		}
	}
	return diags
}

// duplicateDiag reports a duplicate declaration: an error from draft 2026-09,
// a warning before it, since earlier drafts silently took the first match.
func duplicateDiag(draft Draft, kind, name string, at, prev hcl.Range) *hcl.Diagnostic {
	if draft.Strict() {
		return &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Duplicate declaration",
			Detail:   fmt.Sprintf("A %s named %s is already declared in this body at %s.", kind, name, prev),
			Subject:  at.Ptr(),
		}
	}
	return &hcl.Diagnostic{
		Severity: hcl.DiagWarning,
		Summary:  "Duplicate declaration",
		Detail: fmt.Sprintf("A %s named %s is already declared in this body at %s, so the first declaration wins and this one has no effect. "+
			"This is an error from draft 2026-09 onwards.", kind, name, prev),
		Subject: at.Ptr(),
	}
}

func (st *parseState) parseAttribute(blk *hcl.Block, draft Draft) (*AttributeSchema, hcl.Diagnostics) {
	content, diags := blk.Body.Content(godschema.GetAttributeSchema())
	if content == nil {
		return nil, diags
	}
	attr := &AttributeSchema{DeclRange: blk.DefRange, Type: cty.NilType, Default: cty.NilVal}
	if len(blk.Labels) > 0 {
		attr.Name = blk.Labels[0]
	}

	if a, ok := content.Attributes["required"]; ok {
		v, d := boolAttr(a)
		diags = append(diags, d...)
		attr.Required = v
	}
	if a, ok := content.Attributes["type"]; ok {
		t, d := typeexpr.TypeConstraint(a.Expr)
		diags = append(diags, d...)
		if !d.HasErrors() {
			attr.Type = t
		}
	}
	if a, ok := content.Attributes["description"]; ok {
		v, d := stringAttr(a)
		diags = append(diags, d...)
		attr.Description = v
	}
	if a, ok := content.Attributes["deprecated"]; ok {
		v, d := stringAttr(a)
		diags = append(diags, d...)
		attr.Deprecated = v
	}
	if a, ok := content.Attributes["sensitive"]; ok {
		v, d := boolAttr(a)
		diags = append(diags, d...)
		attr.Sensitive = v
	}
	if a, ok := content.Attributes["default"]; ok {
		v, d := evalAttr(a)
		diags = append(diags, d...)
		if v != cty.NilVal {
			if attr.Type != cty.NilType {
				conv, err := convert.Convert(v, attr.Type)
				if err != nil {
					diags = append(diags, &hcl.Diagnostic{
						Severity: hcl.DiagError,
						Summary:  "Invalid default value",
						Detail:   fmt.Sprintf("The default for %q does not match its declared type: %s.", attr.Name, err),
						Subject:  a.Expr.Range().Ptr(),
					})
				} else {
					v = conv
				}
			}
			attr.Default = v
		}
		if attr.Required {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Default on a required attribute",
				Detail:   fmt.Sprintf("Attribute %q is required, so its default can never apply. Make it optional or drop the default.", attr.Name),
				Subject:  a.Expr.Range().Ptr(),
			})
		}
	}
	if a, ok := content.Attributes["enum"]; ok {
		vals, d := valueListAttr(a)
		diags = append(diags, d...)
		attr.Enum = vals
	}
	if a, ok := content.Attributes["pattern"]; ok {
		v, d := stringAttr(a)
		diags = append(diags, d...)
		attr.Pattern = v
		if v != "" {
			re, err := regexp.Compile(v)
			if err != nil {
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Invalid pattern",
					Detail:   fmt.Sprintf("The pattern for %q is not a valid regular expression: %s.", attr.Name, err),
					Subject:  a.Expr.Range().Ptr(),
				})
			} else {
				attr.compiledPattern = re
			}
		}
	}
	if a, ok := content.Attributes["min"]; ok {
		v, d := floatAttr(a)
		diags = append(diags, d...)
		attr.Min = v
	}
	if a, ok := content.Attributes["max"]; ok {
		v, d := floatAttr(a)
		diags = append(diags, d...)
		attr.Max = v
	}
	for _, pair := range []struct {
		key string
		dst *[]string
	}{
		{"conflicts_with", &attr.ConflictsWith},
		{"required_with", &attr.RequiredWith},
		{"exactly_one_of", &attr.ExactlyOneOf},
		{"at_least_one_of", &attr.AtLeastOneOf},
	} {
		if a, ok := content.Attributes[pair.key]; ok {
			v, d := stringListAttr(a)
			diags = append(diags, d...)
			*pair.dst = v
		}
	}

	for _, vb := range content.Blocks {
		if vb.Type != "validation" {
			continue
		}
		vc, d := vb.Body.Content(godschema.GetValidationSchema())
		diags = append(diags, d...)
		if vc == nil {
			continue
		}
		v := Validation{DeclRange: vb.DefRange}
		if a, ok := vc.Attributes["condition"]; ok {
			v.Condition = a.Expr
			v.ConditionSrc = exprSource(a.Expr, st.src)
		}
		if a, ok := vc.Attributes["error_message"]; ok {
			s, sd := stringAttr(a)
			diags = append(diags, sd...)
			v.ErrorMessage = s
		}
		if v.Condition != nil {
			attr.Validations = append(attr.Validations, v)
		}
	}

	return attr, diags
}

// parseBlockHeader returns the block declaration and, when it names one, the
// unresolved ref for the caller to register once the block has been stored.
func (st *parseState) parseBlockHeader(blk *hcl.Block, draft Draft) (*BlockSchema, *pendingRef, hcl.Diagnostics) {
	content, diags := blk.Body.Content(godschema.GetBlockHeaderSchema())
	if content == nil {
		return nil, nil, diags
	}
	bs := &BlockSchema{DeclRange: blk.DefRange}
	if len(blk.Labels) > 0 {
		bs.Type = blk.Labels[0]
	}

	if a, ok := content.Attributes["label_names"]; ok {
		v, d := stringListAttr(a)
		diags = append(diags, d...)
		bs.LabelNames = v
	}
	if a, ok := content.Attributes["description"]; ok {
		v, d := stringAttr(a)
		diags = append(diags, d...)
		bs.Description = v
	}
	if a, ok := content.Attributes["deprecated"]; ok {
		v, d := stringAttr(a)
		diags = append(diags, d...)
		bs.Deprecated = v
	}
	if a, ok := content.Attributes["required"]; ok {
		v, d := boolAttr(a)
		diags = append(diags, d...)
		bs.Required = v
	}
	if a, ok := content.Attributes["min_items"]; ok {
		v, d := intAttr(a)
		diags = append(diags, d...)
		bs.MinItems = v
	}
	if a, ok := content.Attributes["max_items"]; ok {
		v, d := intAttr(a)
		diags = append(diags, d...)
		bs.MaxItems = v
	}
	if a, ok := content.Attributes["unique_labels"]; ok {
		v, d := boolAttr(a)
		diags = append(diags, d...)
		bs.UniqueLabels = v
	}
	if bs.Required && bs.MinItems == 0 {
		bs.MinItems = 1
	}
	if bs.MaxItems > 0 && bs.MinItems > bs.MaxItems {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Contradictory item bounds",
			Detail:   fmt.Sprintf("Block %q declares min_items = %d but max_items = %d.", bs.Type, bs.MinItems, bs.MaxItems),
			Subject:  blk.DefRange.Ptr(),
		})
	}

	openBody := false
	if a, ok := content.Attributes["open"]; ok {
		v, d := boolAttr(a)
		diags = append(diags, d...)
		openBody = v
	}

	// Per-label constraints. Declared `label` blocks must line up with
	// label_names when both are given.
	var labels []LabelSchema
	for _, lb := range content.Blocks {
		if lb.Type != "label" {
			continue
		}
		lc, d := lb.Body.Content(godschema.GetLabelSchema())
		diags = append(diags, d...)
		if lc == nil {
			continue
		}
		ls := LabelSchema{}
		if len(lb.Labels) > 0 {
			ls.Name = lb.Labels[0]
		}
		if a, ok := lc.Attributes["description"]; ok {
			v, sd := stringAttr(a)
			diags = append(diags, sd...)
			ls.Description = v
		}
		if a, ok := lc.Attributes["pattern"]; ok {
			v, sd := stringAttr(a)
			diags = append(diags, sd...)
			ls.Pattern = v
			if v != "" {
				re, err := regexp.Compile(v)
				if err != nil {
					diags = append(diags, &hcl.Diagnostic{
						Severity: hcl.DiagError,
						Summary:  "Invalid label pattern",
						Detail:   fmt.Sprintf("The pattern for label %q is not a valid regular expression: %s.", ls.Name, err),
						Subject:  a.Expr.Range().Ptr(),
					})
				} else {
					ls.compiledPattern = re
				}
			}
		}
		if a, ok := lc.Attributes["enum"]; ok {
			v, sd := stringListAttr(a)
			diags = append(diags, sd...)
			ls.Enum = v
		}
		labels = append(labels, ls)
	}
	if len(labels) > 0 {
		if len(bs.LabelNames) == 0 {
			for _, l := range labels {
				bs.LabelNames = append(bs.LabelNames, l.Name)
			}
		} else if len(labels) != len(bs.LabelNames) {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Label declarations do not match label_names",
				Detail:   fmt.Sprintf("Block %q declares %d label_names but %d label blocks.", bs.Type, len(bs.LabelNames), len(labels)),
				Subject:  blk.DefRange.Ptr(),
			})
		}
		bs.Labels = labels
	}

	// `id` registers this block's body so that a `ref` elsewhere can reuse it.
	// Registration happens before the body is parsed so a body can refer to
	// itself.
	var placeholder *FullBodySchema
	if a, ok := content.Attributes["id"]; ok {
		idVal, d := refKeyFromExpr(a.Expr, "id")
		diags = append(diags, d...)
		if idVal != "" {
			key := "block_header." + idVal
			if _, exists := st.ids[key]; exists {
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Duplicate schema id",
					Detail:   fmt.Sprintf("The id %q is already registered in this document.", idVal),
					Subject:  a.Expr.Range().Ptr(),
				})
			} else {
				placeholder = &FullBodySchema{DeclRange: blk.DefRange}
				st.ids[key] = placeholder
			}
		}
	}

	hasBody := false
	for _, inner := range content.Blocks {
		if inner.Type != "body" {
			continue
		}
		if hasBody {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Duplicate body block",
				Detail:   fmt.Sprintf("Block %q declares more than one `body`.", bs.Type),
				Subject:  inner.DefRange.Ptr(),
			})
			continue
		}
		hasBody = true
		target := placeholder
		if target == nil {
			target = &FullBodySchema{DeclRange: inner.DefRange}
		} else {
			target.DeclRange = inner.DefRange
		}
		diags = append(diags, st.fillBody(target, inner.Body, draft)...)
		bs.Body = target
	}

	var ref *pendingRef
	if refAttr, ok := content.Attributes["ref"]; ok {
		key, d := refKeyFromExpr(refAttr.Expr, "ref")
		diags = append(diags, d...)
		if key != "" {
			switch {
			case hasBody:
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Both ref and body declared",
					Detail:   fmt.Sprintf("Block %q declares a `ref` and its own `body`; they cannot both apply.", bs.Type),
					Subject:  refAttr.Expr.Range().Ptr(),
				})
			case openBody:
				// A referenced body is shared with every other user of that
				// id, so opening it here would silently open it everywhere.
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Cannot open a referenced body",
					Detail:   fmt.Sprintf("Block %q sets `open` alongside a `ref`. The referenced body is shared, so declare `open` where that body is defined.", bs.Type),
					Subject:  refAttr.Expr.Range().Ptr(),
				})
			default:
				ref = &pendingRef{key: key, rng: refAttr.Expr.Range()}
			}
		}
	}

	if openBody && ref == nil {
		if bs.Body == nil {
			bs.Body = &FullBodySchema{DeclRange: blk.DefRange}
		}
		bs.Body.Open = true
	}

	return bs, ref, diags
}

func (st *parseState) parseOneOf(blk *hcl.Block, draft Draft) ([]VariantSchema, hcl.Diagnostics) {
	content, diags := blk.Body.Content(godschema.GetOneOfSchema())
	if content == nil {
		return nil, diags
	}
	var out []VariantSchema
	for _, vb := range content.Blocks {
		if vb.Type != "variant" {
			continue
		}
		vc, d := vb.Body.Content(godschema.GetVariantSchema())
		diags = append(diags, d...)
		if vc == nil {
			continue
		}
		v := VariantSchema{DeclRange: vb.DefRange}
		if len(vb.Labels) > 0 {
			v.Name = vb.Labels[0]
		}
		for _, bb := range vc.Blocks {
			if bb.Type != "body" {
				continue
			}
			body, bd := st.parseBody(bb.Body, bb.DefRange, draft)
			diags = append(diags, bd...)
			v.Body = body
		}
		if v.Body == nil {
			v.Body = &FullBodySchema{DeclRange: vb.DefRange}
		}
		out = append(out, v)
	}
	if len(out) < 2 {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Degenerate one_of block",
			Detail:   "A `one_of` block needs at least two `variant` blocks to choose between.",
			Subject:  blk.DefRange.Ptr(),
		})
	}
	return out, diags
}

// resolveRefs is the second pass. Deferring every ref to the end is what makes
// forward references work: the id registry is complete by the time any ref is
// looked up.
func (st *parseState) resolveRefs() hcl.Diagnostics {
	var diags hcl.Diagnostics
	for _, p := range st.pending {
		resolved, ok := st.ids[p.key]
		if !ok {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Unresolved ref",
				Detail:   fmt.Sprintf("No declaration with id %q exists in this document. %s", p.key, knownIDs(st.ids)),
				Subject:  p.rng.Ptr(),
			})
			continue
		}
		p.owner.Blocks[p.index].Body = resolved
	}
	st.pending = nil
	return diags
}

func knownIDs(ids map[string]*FullBodySchema) string {
	if len(ids) == 0 {
		return "This document declares no ids."
	}
	known := make([]string, 0, len(ids))
	for k := range ids {
		known = append(known, k)
	}
	sortStrings(known)
	return "Known ids: " + strings.Join(known, ", ") + "."
}

// refKeyFromExpr reads an `id` or `ref` expression. Both a bare traversal
// (ref = block_header.foo) and a quoted string (id = "foo") are accepted, and
// neither depends on the source text's whitespace.
func refKeyFromExpr(expr hcl.Expression, kind string) (string, hcl.Diagnostics) {
	if trav, d := hcl.AbsTraversalForExpr(expr); !d.HasErrors() {
		parts := make([]string, 0, len(trav))
		for _, step := range trav {
			switch s := step.(type) {
			case hcl.TraverseRoot:
				parts = append(parts, s.Name)
			case hcl.TraverseAttr:
				parts = append(parts, s.Name)
			default:
				return "", hcl.Diagnostics{{
					Severity: hcl.DiagError,
					Summary:  "Invalid " + kind,
					Detail:   "Only dotted names such as block_header.foo are accepted here.",
					Subject:  expr.Range().Ptr(),
				}}
			}
		}
		key := strings.Join(parts, ".")
		if kind == "id" {
			// `id = foo` is shorthand for `id = "foo"`.
			return strings.TrimPrefix(key, "block_header."), nil
		}
		return key, nil
	}

	v, diags := expr.Value(nil)
	if diags.HasErrors() {
		return "", diags
	}
	if v.IsNull() || v.Type() != cty.String {
		return "", hcl.Diagnostics{{
			Severity: hcl.DiagError,
			Summary:  "Invalid " + kind,
			Detail:   "Expected a dotted name such as block_header.foo, or a quoted string.",
			Subject:  expr.Range().Ptr(),
		}}
	}
	return v.AsString(), nil
}

// Legacy projects onto the type ParseSchemaFile returned before draft 2026-09.
func (s *Schema) Legacy() *BlockHeaderAndBodySchema {
	if s == nil {
		return nil
	}
	return &BlockHeaderAndBodySchema{BodySchema: s.Body}
}
