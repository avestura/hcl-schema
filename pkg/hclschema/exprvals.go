package hclschema

import (
	"fmt"
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

// The helpers below read meta-schema attribute values. They all evaluate with
// a nil EvalContext, so a schema document cannot reference variables or call
// functions; every value must be a literal. Evaluating the expression is what
// replaced reading the source text off disk, which is why parsing no longer
// depends on whitespace or on the file still being on disk.

func evalAttr(a *hcl.Attribute) (cty.Value, hcl.Diagnostics) {
	v, diags := a.Expr.Value(nil)
	if diags.HasErrors() {
		return cty.NilVal, diags
	}
	return v, diags
}

func stringAttr(a *hcl.Attribute) (string, hcl.Diagnostics) {
	v, diags := evalAttr(a)
	if diags.HasErrors() || v == cty.NilVal {
		return "", diags
	}
	if v.IsNull() {
		return "", diags
	}
	conv, err := convert.Convert(v, cty.String)
	if err != nil {
		return "", append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Invalid value",
			Detail:   fmt.Sprintf("%s must be a string: %s.", a.Name, err),
			Subject:  a.Expr.Range().Ptr(),
		})
	}
	return conv.AsString(), diags
}

func boolAttr(a *hcl.Attribute) (bool, hcl.Diagnostics) {
	v, diags := evalAttr(a)
	if diags.HasErrors() || v == cty.NilVal || v.IsNull() {
		return false, diags
	}
	conv, err := convert.Convert(v, cty.Bool)
	if err != nil {
		return false, append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Invalid value",
			Detail:   fmt.Sprintf("%s must be a boolean: %s.", a.Name, err),
			Subject:  a.Expr.Range().Ptr(),
		})
	}
	return conv.True(), diags
}

func floatAttr(a *hcl.Attribute) (*float64, hcl.Diagnostics) {
	v, diags := evalAttr(a)
	if diags.HasErrors() || v == cty.NilVal || v.IsNull() {
		return nil, diags
	}
	conv, err := convert.Convert(v, cty.Number)
	if err != nil {
		return nil, append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Invalid value",
			Detail:   fmt.Sprintf("%s must be a number: %s.", a.Name, err),
			Subject:  a.Expr.Range().Ptr(),
		})
	}
	f, _ := conv.AsBigFloat().Float64()
	return &f, diags
}

func intAttr(a *hcl.Attribute) (int, hcl.Diagnostics) {
	f, diags := floatAttr(a)
	if f == nil {
		return 0, diags
	}
	return int(*f), diags
}

func stringListAttr(a *hcl.Attribute) ([]string, hcl.Diagnostics) {
	vals, diags := valueListAttr(a)
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		conv, err := convert.Convert(v, cty.String)
		if err != nil {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Invalid value",
				Detail:   fmt.Sprintf("%s must be a list of strings: %s.", a.Name, err),
				Subject:  a.Expr.Range().Ptr(),
			})
			continue
		}
		out = append(out, conv.AsString())
	}
	return out, diags
}

func valueListAttr(a *hcl.Attribute) ([]cty.Value, hcl.Diagnostics) {
	v, diags := evalAttr(a)
	if diags.HasErrors() || v == cty.NilVal || v.IsNull() {
		return nil, diags
	}
	if !v.CanIterateElements() {
		return nil, append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Invalid value",
			Detail:   fmt.Sprintf("%s must be a list.", a.Name),
			Subject:  a.Expr.Range().Ptr(),
		})
	}
	var out []cty.Value
	it := v.ElementIterator()
	for it.Next() {
		_, ev := it.Element()
		out = append(out, ev)
	}
	return out, diags
}

func sortStrings(s []string) { sort.Strings(s) }

// exprSource recovers an expression's original text from the document source.
// Slicing the in-memory source keeps parsing independent of the file still
// being on disk, and independent of how the expression was spaced.
func exprSource(expr hcl.Expression, src []byte) string {
	if expr == nil || src == nil {
		return ""
	}
	r := expr.Range()
	start, end := r.Start.Byte, r.End.Byte
	if start < 0 || end > len(src) || start >= end {
		return ""
	}
	return string(src[start:end])
}
