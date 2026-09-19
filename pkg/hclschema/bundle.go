package hclschema

import (
	"github.com/hashicorp/hcl/v2"
)

// Bundle renders the schema as a single self-contained document with no
// `import` blocks.
//
// Because `ref` resolution replaces names with pointers, a parsed schema
// already holds everything its imports contributed. Rendering it therefore
// produces a file that validates identically without touching the network,
// which is what makes a schema vendorable into a repository and checkable in
// an offline CI job.
func (s *Schema) Bundle() ([]byte, hcl.Diagnostics) {
	if s == nil || s.Body == nil {
		return nil, hcl.Diagnostics{{
			Severity: hcl.DiagError,
			Summary:  "Nothing to bundle",
			Detail:   "The schema is empty.",
		}}
	}
	flat := &Schema{
		Draft:     s.Draft,
		SchemaRef: s.SchemaRef,
		ID:        s.ID,
		Body:      s.Body,
		Filename:  s.Filename,
		Imports:   map[string]*Schema{},
	}
	return flat.Bytes(), nil
}

// BundleFile reads a schema from disk and renders a self-contained copy.
func BundleFile(path string, opts ParseOptions) ([]byte, hcl.Diagnostics) {
	schema, diags := ParseSchemaFileOpts(path, opts)
	if diags.HasErrors() {
		return nil, diags
	}
	out, d := schema.Bundle()
	return out, append(diags, d...)
}
