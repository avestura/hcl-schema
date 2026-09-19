package hclschema

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

// linkAttributes are the reserved attributes an instance may carry at its root
// to point at the schema it should be checked against.
var linkAttributes = []string{"__schema", "__id", "__schema_sha256"}

// ValidateOptions controls how an instance is checked.
type ValidateOptions struct {
	// Strict applies draft 2026-09 rules regardless of the draft the schema
	// pins.
	Strict bool

	// EvalContext is used to evaluate attribute values. When nil, values are
	// evaluated without variables or functions, and any expression that needs
	// them is left unchecked rather than reported as an error.
	EvalContext *hcl.EvalContext
}

// Validate checks a body against the schema.
func (s *Schema) Validate(body hcl.Body) hcl.Diagnostics {
	return s.ValidateOpts(body, ValidateOptions{})
}

// ValidateFile checks a parsed file against the schema.
func (s *Schema) ValidateFile(f *hcl.File) hcl.Diagnostics {
	if f == nil {
		return nil
	}
	return s.Validate(f.Body)
}

// ValidateOpts checks a body against the schema with explicit options.
func (s *Schema) ValidateOpts(body hcl.Body, opts ValidateOptions) hcl.Diagnostics {
	if s == nil || s.Body == nil {
		return nil
	}
	v := &validator{draft: s.Draft.effective(opts.Strict), ctx: opts.EvalContext}
	return v.checkBody(body, s.Body, true)
}

type validator struct {
	draft Draft
	ctx   *hcl.EvalContext
}

func (v *validator) checkBody(body hcl.Body, fbs *FullBodySchema, root bool) hcl.Diagnostics {
	if fbs == nil {
		fbs = &FullBodySchema{}
	}

	// With variants the base declarations alone cannot be exhaustive, so the
	// base pass is permissive and each variant supplies the closed check.
	if len(fbs.Variants) > 0 {
		return v.checkVariants(body, fbs, root)
	}

	bs := fbs.AsBodySchema()
	if root {
		// Only add a link attribute the schema does not declare itself.
		// hcl.BodySchema keys attributes by name, so appending unconditionally
		// would override a schema's own `__schema` declaration and quietly
		// drop its Required flag.
		for _, name := range linkAttributes {
			if fbs.Attribute(name) == nil {
				bs.Attributes = append(bs.Attributes, hcl.AttributeSchema{Name: name})
			}
		}
	}

	var content *hcl.BodyContent
	var diags hcl.Diagnostics
	if fbs.Open {
		content, _, diags = body.PartialContent(bs)
	} else {
		content, diags = body.Content(bs)
	}
	if content == nil {
		return diags
	}

	diags = append(diags, v.checkAttributes(content, fbs)...)
	diags = append(diags, v.checkBlocks(content, fbs)...)
	return diags
}

func (v *validator) checkAttributes(content *hcl.BodyContent, fbs *FullBodySchema) hcl.Diagnostics {
	var diags hcl.Diagnostics

	present := make(map[string]*hcl.Attribute, len(content.Attributes))
	for name, a := range content.Attributes {
		present[name] = a
	}

	for i := range fbs.Attributes {
		decl := &fbs.Attributes[i]
		a, ok := present[decl.Name]
		if !ok {
			continue
		}
		if decl.Deprecated != "" {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagWarning,
				Summary:  "Deprecated attribute",
				Detail:   fmt.Sprintf("%q is deprecated: %s", decl.Name, decl.Deprecated),
				Subject:  a.NameRange.Ptr(),
			})
		}
		diags = append(diags, v.checkValue(decl, a)...)
	}

	diags = append(diags, v.checkCrossField(present, fbs)...)
	return diags
}

// checkValue applies the type constraint and the value constraints. An
// expression that cannot be evaluated statically is skipped rather than
// reported, because an instance may legitimately reference variables or call
// functions that only the embedding application can supply.
func (v *validator) checkValue(decl *AttributeSchema, a *hcl.Attribute) hcl.Diagnostics {
	if decl.Type == cty.NilType && decl.Enum == nil && decl.compiledPattern == nil &&
		decl.Min == nil && decl.Max == nil && len(decl.Validations) == 0 {
		return nil
	}

	val, evalDiags := a.Expr.Value(v.ctx)
	if evalDiags.HasErrors() {
		if v.ctx != nil {
			return evalDiags
		}
		return nil
	}
	if !val.IsWhollyKnown() || val.IsNull() {
		return nil
	}

	var diags hcl.Diagnostics
	rng := a.Expr.Range().Ptr()

	if decl.Type != cty.NilType {
		conv, err := convert.Convert(val, decl.Type)
		if err != nil {
			return append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Incorrect attribute value type",
				Detail: fmt.Sprintf("Inappropriate value for %q: %s.",
					decl.Name, err),
				Subject:    rng,
				Expression: a.Expr,
			})
		}
		val = conv
	}

	if len(decl.Enum) > 0 {
		matched := false
		for _, want := range decl.Enum {
			cand := want
			if decl.Type != cty.NilType {
				if c, err := convert.Convert(want, decl.Type); err == nil {
					cand = c
				}
			}
			if cand.Type().Equals(val.Type()) && cand.Equals(val).True() {
				matched = true
				break
			}
		}
		if !matched {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Value not permitted",
				Detail: fmt.Sprintf("%q must be one of %s, but %s was given.",
					decl.Name, formatEnum(decl.Enum), redact(decl, val)),
				Subject:    rng,
				Expression: a.Expr,
			})
		}
	}

	if decl.compiledPattern != nil {
		if s, ok := asString(val); ok {
			if !decl.compiledPattern.MatchString(s) {
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Value does not match pattern",
					Detail: fmt.Sprintf("%q must match %s, but %s was given.",
						decl.Name, decl.Pattern, redact(decl, val)),
					Subject:    rng,
					Expression: a.Expr,
				})
			}
		} else {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagWarning,
				Summary:  "Pattern ignored",
				Detail:   fmt.Sprintf("A pattern is declared for %q, but its value is not a string.", decl.Name),
				Subject:  rng,
			})
		}
	}

	if decl.Min != nil || decl.Max != nil {
		diags = append(diags, checkBounds(decl, val, rng)...)
	}

	for _, rule := range decl.Validations {
		ctx := v.selfContext(val)
		res, rd := rule.Condition.Value(ctx)
		if rd.HasErrors() {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid validation condition",
				Detail: fmt.Sprintf("The condition for %q could not be evaluated: %s",
					decl.Name, rd.Error()),
				Subject: rule.Condition.Range().Ptr(),
			})
			continue
		}
		b, err := convert.Convert(res, cty.Bool)
		if err != nil || b.IsNull() {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid validation condition",
				Detail:   fmt.Sprintf("The condition for %q must produce a boolean.", decl.Name),
				Subject:  rule.Condition.Range().Ptr(),
			})
			continue
		}
		if !b.True() {
			msg := rule.ErrorMessage
			if msg == "" {
				msg = fmt.Sprintf("%q failed a validation rule.", decl.Name)
			}
			diags = append(diags, &hcl.Diagnostic{
				Severity:   hcl.DiagError,
				Summary:    "Invalid value",
				Detail:     msg,
				Subject:    rng,
				Expression: a.Expr,
			})
		}
	}

	return diags
}

// selfContext binds `self` to the value under test, so a validation condition
// reads as a predicate over the attribute.
func (v *validator) selfContext(val cty.Value) *hcl.EvalContext {
	var ctx *hcl.EvalContext
	if v.ctx != nil {
		ctx = v.ctx.NewChild()
	} else {
		ctx = &hcl.EvalContext{}
	}
	ctx.Variables = map[string]cty.Value{"self": val}
	return ctx
}

// checkBounds applies min and max. What they bound depends on the value: a
// number's magnitude, a string's length, or a collection's element count.
func checkBounds(decl *AttributeSchema, val cty.Value, rng *hcl.Range) hcl.Diagnostics {
	var (
		measure float64
		noun    string
		ok      bool
	)
	switch {
	case val.Type() == cty.Number:
		f, _ := val.AsBigFloat().Float64()
		measure, noun, ok = f, "", true
	case val.Type() == cty.String:
		measure, noun, ok = float64(len([]rune(val.AsString()))), "length ", true
	case val.CanIterateElements():
		measure, noun, ok = float64(val.LengthInt()), "element count ", true
	}
	if !ok {
		return hcl.Diagnostics{{
			Severity: hcl.DiagWarning,
			Summary:  "Bounds ignored",
			Detail:   fmt.Sprintf("min/max are declared for %q, but its value has no measurable size.", decl.Name),
			Subject:  rng,
		}}
	}

	var diags hcl.Diagnostics
	if decl.Min != nil && measure < *decl.Min {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Value below minimum",
			Detail:   fmt.Sprintf("%q has %s%s, below the minimum of %s.", decl.Name, noun, formatNum(measure), formatNum(*decl.Min)),
			Subject:  rng,
		})
	}
	if decl.Max != nil && measure > *decl.Max {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Value above maximum",
			Detail:   fmt.Sprintf("%q has %s%s, above the maximum of %s.", decl.Name, noun, formatNum(measure), formatNum(*decl.Max)),
			Subject:  rng,
		})
	}
	return diags
}

func (v *validator) checkCrossField(present map[string]*hcl.Attribute, fbs *FullBodySchema) hcl.Diagnostics {
	var diags hcl.Diagnostics

	// exactly_one_of and at_least_one_of describe a group rather than a single
	// attribute, so each distinct group is reported once.
	reportedExactly := map[string]bool{}
	reportedAtLeast := map[string]bool{}

	for i := range fbs.Attributes {
		decl := &fbs.Attributes[i]
		_, here := present[decl.Name]

		if here {
			for _, other := range decl.ConflictsWith {
				if a, ok := present[other]; ok {
					diags = append(diags, &hcl.Diagnostic{
						Severity: hcl.DiagError,
						Summary:  "Conflicting attributes",
						Detail:   fmt.Sprintf("%q and %q cannot both be set.", decl.Name, other),
						Subject:  a.NameRange.Ptr(),
					})
				}
			}
			for _, other := range decl.RequiredWith {
				if _, ok := present[other]; !ok {
					diags = append(diags, &hcl.Diagnostic{
						Severity: hcl.DiagError,
						Summary:  "Missing required attribute",
						Detail:   fmt.Sprintf("%q requires %q to be set as well.", decl.Name, other),
						Subject:  present[decl.Name].NameRange.Ptr(),
					})
				}
			}
		}

		if len(decl.ExactlyOneOf) > 0 {
			group := normalizeGroup(decl.ExactlyOneOf, decl.Name)
			key := strings.Join(group, "\x00")
			if !reportedExactly[key] {
				reportedExactly[key] = true
				diags = append(diags, checkGroup(present, group, fbs, 1, 1, "exactly one")...)
			}
		}
		if len(decl.AtLeastOneOf) > 0 {
			group := normalizeGroup(decl.AtLeastOneOf, decl.Name)
			key := strings.Join(group, "\x00")
			if !reportedAtLeast[key] {
				reportedAtLeast[key] = true
				diags = append(diags, checkGroup(present, group, fbs, 1, 0, "at least one")...)
			}
		}
	}
	return diags
}

// normalizeGroup includes the declaring attribute in its own group, so
// `exactly_one_of = ["b"]` on `a` means one of a or b.
func normalizeGroup(group []string, self string) []string {
	out := append([]string{}, group...)
	found := false
	for _, g := range out {
		if g == self {
			found = true
			break
		}
	}
	if !found {
		out = append(out, self)
	}
	sort.Strings(out)
	return out
}

func checkGroup(present map[string]*hcl.Attribute, group []string, fbs *FullBodySchema, min, max int, phrase string) hcl.Diagnostics {
	set := 0
	for _, name := range group {
		if _, ok := present[name]; ok {
			set++
		}
	}
	if set >= min && (max == 0 || set <= max) {
		return nil
	}
	subject := fbs.DeclRange.Ptr()
	for _, name := range group {
		if a, ok := present[name]; ok {
			subject = a.NameRange.Ptr()
			break
		}
	}
	return hcl.Diagnostics{{
		Severity: hcl.DiagError,
		Summary:  "Invalid attribute combination",
		Detail:   fmt.Sprintf("%s of %s must be set; %d were found.", capitalize(phrase), quoteList(group), set),
		Subject:  subject,
	}}
}

func (v *validator) checkBlocks(content *hcl.BodyContent, fbs *FullBodySchema) hcl.Diagnostics {
	var diags hcl.Diagnostics

	counts := map[string]int{}
	labelsSeen := map[string]map[string]hcl.Range{}

	for _, blk := range content.Blocks {
		decl := fbs.Block(blk.Type, len(blk.Labels))
		if decl == nil {
			// An undeclared block in an open body is allowed and unchecked.
			continue
		}
		counts[decl.Type]++

		if decl.Deprecated != "" {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagWarning,
				Summary:  "Deprecated block",
				Detail:   fmt.Sprintf("%q is deprecated: %s", decl.Type, decl.Deprecated),
				Subject:  blk.DefRange.Ptr(),
			})
		}

		diags = append(diags, checkLabels(decl, blk)...)

		if decl.UniqueLabels && len(blk.Labels) > 0 {
			key := strings.Join(blk.Labels, "\x00")
			if labelsSeen[decl.Type] == nil {
				labelsSeen[decl.Type] = map[string]hcl.Range{}
			}
			if prev, dup := labelsSeen[decl.Type][key]; dup {
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Duplicate block labels",
					Detail: fmt.Sprintf("Another %q block with labels %s is declared at %s.",
						decl.Type, quoteList(blk.Labels), prev),
					Subject: blk.DefRange.Ptr(),
				})
			} else {
				labelsSeen[decl.Type][key] = blk.DefRange
			}
		}

		switch {
		case decl.Body != nil:
			diags = append(diags, v.checkBody(blk.Body, decl.Body, false)...)
		case v.draft.Strict():
			// From draft 2026-09 a block that declares no body must be empty.
			// Before that, its contents went entirely unchecked.
			diags = append(diags, v.checkBody(blk.Body, &FullBodySchema{}, false)...)
		}
	}

	for i := range fbs.Blocks {
		decl := &fbs.Blocks[i]
		n := counts[decl.Type]
		if decl.MinItems > 0 && n < decl.MinItems {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Insufficient blocks",
				Detail: fmt.Sprintf("At least %d %q block(s) are required, but %d were found.",
					decl.MinItems, decl.Type, n),
				Subject: fbs.DeclRange.Ptr(),
			})
		}
		if decl.MaxItems > 0 && n > decl.MaxItems {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Too many blocks",
				Detail: fmt.Sprintf("At most %d %q block(s) are allowed, but %d were found.",
					decl.MaxItems, decl.Type, n),
				Subject: fbs.DeclRange.Ptr(),
			})
		}
	}

	return diags
}

func checkLabels(decl *BlockSchema, blk *hcl.Block) hcl.Diagnostics {
	if len(decl.Labels) == 0 {
		return nil
	}
	var diags hcl.Diagnostics
	for i, ls := range decl.Labels {
		if i >= len(blk.Labels) {
			break
		}
		got := blk.Labels[i]
		rng := blk.DefRange.Ptr()
		if i < len(blk.LabelRanges) {
			rng = blk.LabelRanges[i].Ptr()
		}
		if ls.compiledPattern != nil && !ls.compiledPattern.MatchString(got) {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid block label",
				Detail: fmt.Sprintf("The %s label of %q must match %s, but %q was given.",
					ls.Name, decl.Type, ls.Pattern, got),
				Subject: rng,
			})
		}
		if len(ls.Enum) > 0 {
			allowed := false
			for _, e := range ls.Enum {
				if e == got {
					allowed = true
					break
				}
			}
			if !allowed {
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Invalid block label",
					Detail: fmt.Sprintf("The %s label of %q must be one of %s, but %q was given.",
						ls.Name, decl.Type, quoteList(ls.Enum), got),
					Subject: rng,
				})
			}
		}
	}
	return diags
}

// checkVariants requires the body to satisfy exactly one alternative. Each
// variant is tried against the base declarations merged with its own, and the
// closest match supplies the diagnostics when none fits.
func (v *validator) checkVariants(body hcl.Body, fbs *FullBodySchema, root bool) hcl.Diagnostics {
	base := *fbs
	base.Variants = nil

	type attempt struct {
		name  string
		diags hcl.Diagnostics
		errs  int
	}
	attempts := make([]attempt, 0, len(fbs.Variants))
	matched := 0

	for _, variant := range fbs.Variants {
		merged := mergeBodies(&base, variant.Body)
		d := v.checkBody(body, merged, root)
		errs := 0
		for _, dg := range d {
			if dg.Severity == hcl.DiagError {
				errs++
			}
		}
		if errs == 0 {
			matched++
		}
		attempts = append(attempts, attempt{name: variant.Name, diags: d, errs: errs})
	}

	switch {
	case matched == 1:
		for _, a := range attempts {
			if a.errs == 0 {
				return a.diags
			}
		}
		return nil
	case matched > 1:
		names := make([]string, 0, matched)
		for _, a := range attempts {
			if a.errs == 0 {
				names = append(names, a.name)
			}
		}
		return hcl.Diagnostics{{
			Severity: hcl.DiagError,
			Summary:  "Ambiguous variant",
			Detail: fmt.Sprintf("This body satisfies more than one variant (%s); exactly one must apply.",
				quoteList(names)),
			Subject: fbs.DeclRange.Ptr(),
		}}
	}

	best := attempts[0]
	for _, a := range attempts[1:] {
		if a.errs < best.errs {
			best = a
		}
	}
	names := make([]string, 0, len(attempts))
	for _, a := range attempts {
		names = append(names, a.name)
	}
	out := hcl.Diagnostics{{
		Severity: hcl.DiagError,
		Summary:  "No matching variant",
		Detail: fmt.Sprintf("This body must match one of %s. The closest is %q; its errors follow.",
			quoteList(names), best.name),
		Subject: fbs.DeclRange.Ptr(),
	}}
	return append(out, best.diags...)
}

// mergeBodies overlays a variant's declarations on the base ones. A variant
// may narrow a base declaration by redeclaring it under the same name.
func mergeBodies(base, overlay *FullBodySchema) *FullBodySchema {
	if overlay == nil {
		clone := *base
		return &clone
	}
	out := &FullBodySchema{
		Open:      base.Open || overlay.Open,
		DeclRange: base.DeclRange,
	}
	out.Attributes = append(out.Attributes, base.Attributes...)
	for _, a := range overlay.Attributes {
		replaced := false
		for i := range out.Attributes {
			if out.Attributes[i].Name == a.Name {
				out.Attributes[i] = a
				replaced = true
				break
			}
		}
		if !replaced {
			out.Attributes = append(out.Attributes, a)
		}
	}
	out.Blocks = append(out.Blocks, base.Blocks...)
	for _, b := range overlay.Blocks {
		replaced := false
		for i := range out.Blocks {
			if out.Blocks[i].Type == b.Type {
				out.Blocks[i] = b
				replaced = true
				break
			}
		}
		if !replaced {
			out.Blocks = append(out.Blocks, b)
		}
	}
	return out
}

func asString(v cty.Value) (string, bool) {
	conv, err := convert.Convert(v, cty.String)
	if err != nil || conv.IsNull() {
		return "", false
	}
	return conv.AsString(), true
}

// redact keeps a sensitive attribute's value out of diagnostics, which are
// routinely written to CI logs.
func redact(decl *AttributeSchema, v cty.Value) string {
	if decl.Sensitive {
		return "(sensitive value)"
	}
	return formatValue(v)
}

func formatValue(v cty.Value) string {
	if v.IsNull() {
		return "null"
	}
	switch v.Type() {
	case cty.String:
		return fmt.Sprintf("%q", v.AsString())
	case cty.Number:
		f, _ := v.AsBigFloat().Float64()
		return formatNum(f)
	case cty.Bool:
		if v.True() {
			return "true"
		}
		return "false"
	}
	return v.GoString()
}

func formatNum(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", f), "0"), ".")
}

func formatEnum(vals []cty.Value) string {
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		parts = append(parts, formatValue(v))
	}
	return strings.Join(parts, ", ")
}

func quoteList(items []string) string {
	parts := make([]string, 0, len(items))
	for _, i := range items {
		parts = append(parts, fmt.Sprintf("%q", i))
	}
	return strings.Join(parts, ", ")
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
