package hclschema

import (
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
)

// Result is everything a single check produced. It carries the parsed files so
// that a caller can render diagnostics with source snippets, which HCL's own
// text writer needs and which a plain diagnostic list cannot supply.
type Result struct {
	// Filename is the name the instance was checked under.
	Filename string

	// File is the parsed instance, or nil when parsing failed.
	File *hcl.File

	// Schema is the schema the instance was checked against, or nil when the
	// instance declared none.
	Schema *Schema

	// Files maps every filename involved, including the schema and anything it
	// imported, to its parsed form.
	Files map[string]*hcl.File

	// Diagnostics is everything the check reported.
	Diagnostics hcl.Diagnostics
}

// HasErrors reports whether any diagnostic is an error.
func (r Result) HasErrors() bool { return r.Diagnostics.HasErrors() }

// Check parses an instance and validates it against its schema, keeping hold
// of every file it touched.
func Check(src []byte, filename string, opts LoadOptions) Result {
	res := Result{Filename: filename, Files: map[string]*hcl.File{}}

	parser := hclparse.NewParser()
	file, diags := parseSource(parser, src, filename)
	res.Diagnostics = diags
	res.File = file
	if file != nil {
		res.Files[filename] = file
	}
	if diags.HasErrors() || file == nil {
		return res
	}

	// A schema document is checked against the meta-schema rather than against
	// a schema of its own.
	if strings.HasSuffix(filename, SchemaExtension) {
		schema, d := ParseSchemaOpts(src, filename, opts.ParseOptions)
		res.Schema = schema
		res.Diagnostics = append(res.Diagnostics, d...)
		return res
	}

	schema, sd := SchemaFor(file.Body, filename, opts)
	res.Diagnostics = append(res.Diagnostics, sd...)
	res.Schema = schema
	if schema == nil {
		return res
	}
	if schema.Filename != "" {
		if sf, ok := schemaFileOf(schema, opts.ParseOptions.Loader); ok {
			res.Files[schema.Filename] = sf
		}
	}
	res.Diagnostics = append(res.Diagnostics, schema.ValidateOpts(file.Body, opts.ValidateOptions)...)
	return res
}

// CheckFile reads an instance from disk and checks it.
func CheckFile(path string, opts LoadOptions) Result {
	loader := opts.ParseOptions.Loader
	if loader == nil {
		loader = DefaultLoader
	}
	src, resolved, diags := loader.Load(path, "", "")
	if diags.HasErrors() {
		return Result{Filename: path, Diagnostics: diags, Files: map[string]*hcl.File{}}
	}
	res := Check(src, resolved, opts)
	res.Diagnostics = append(diags, res.Diagnostics...)
	return res
}

// schemaFileOf re-parses a schema's source so that diagnostics pointing into
// the schema document can be rendered with a snippet.
//
// It goes through the caller's loader rather than the default one: a remote
// schema would otherwise be fetched again here, ignoring an offline setting
// the caller asked for.
func schemaFileOf(s *Schema, loader *Loader) (*hcl.File, bool) {
	if s == nil || s.Filename == "" {
		return nil, false
	}
	if loader == nil {
		loader = DefaultLoader
	}
	src, _, diags := loader.Load(s.Filename, "", "")
	if diags.HasErrors() {
		return nil, false
	}
	f, d := parseSource(hclparse.NewParser(), src, s.Filename)
	if d.HasErrors() || f == nil {
		return nil, false
	}
	return f, true
}
