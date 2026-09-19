package hclschema

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
)

// SchemaLink is the `__schema` reference an instance carries, together with
// the optional digest that pins it.
type SchemaLink struct {
	Ref    string
	Sum    string
	Range  hcl.Range
	Found  bool
	Digest bool
}

// linkProbeSchema is used with PartialContent so that reading the link never
// reports anything about the rest of the file.
var linkProbeSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{Name: "__schema"},
		{Name: "__schema_sha256"},
	},
}

// FindSchemaLink reads the `__schema` attribute from a parsed body.
//
// This replaces the original text scan for the string "__schema", which
// matched inside comments and string values because it never consulted the
// parse tree.
func FindSchemaLink(body hcl.Body) (SchemaLink, hcl.Diagnostics) {
	var link SchemaLink
	content, _, diags := body.PartialContent(linkProbeSchema)
	if content == nil {
		return link, diags
	}
	if a, ok := content.Attributes["__schema"]; ok {
		v, d := stringAttr(a)
		diags = append(diags, d...)
		link.Ref = v
		link.Range = a.Expr.Range()
		link.Found = v != ""
	}
	if a, ok := content.Attributes["__schema_sha256"]; ok {
		v, d := stringAttr(a)
		diags = append(diags, d...)
		link.Sum = v
		link.Digest = v != ""
	}
	return link, diags
}

// LoadOptions controls how an instance finds and loads its schema.
type LoadOptions struct {
	ParseOptions
	ValidateOptions
}

// SchemaFor resolves and parses the schema an instance links to. It returns a
// nil schema without diagnostics when the instance carries no `__schema`.
func SchemaFor(body hcl.Body, filename string, opts LoadOptions) (*Schema, hcl.Diagnostics) {
	link, diags := FindSchemaLink(body)
	if diags.HasErrors() || !link.Found {
		return nil, diags
	}

	loader := opts.ParseOptions.Loader
	if loader == nil {
		loader = DefaultLoader
	}
	src, resolved, d := loader.Load(link.Ref, dirOf(filename), link.Sum)
	diags = append(diags, withSubject(d, link.Range)...)
	if d.HasErrors() {
		return nil, diags
	}

	po := opts.ParseOptions
	po.Loader = loader
	schema, sd := ParseSchemaOpts(src, resolved, po)
	diags = append(diags, sd...)
	return schema, diags
}

// withSubject anchors diagnostics that have no source range of their own at
// the place in the instance that caused them, so an editor can show them.
func withSubject(diags hcl.Diagnostics, rng hcl.Range) hcl.Diagnostics {
	out := make(hcl.Diagnostics, 0, len(diags))
	for _, d := range diags {
		if d == nil {
			continue
		}
		if d.Subject == nil {
			clone := *d
			clone.Subject = rng.Ptr()
			out = append(out, &clone)
			continue
		}
		out = append(out, d)
	}
	return out
}

// ValidateSource checks an in-memory instance, resolving its schema through
// `__schema`. filename is used to resolve relative references and to report
// positions.
func ValidateSource(src []byte, filename string, opts LoadOptions) hcl.Diagnostics {
	parser := hclparse.NewParser()
	file, diags := parseSource(parser, src, filename)
	if diags.HasErrors() || file == nil {
		return diags
	}

	// A schema document is checked against the meta-schema rather than against
	// a schema of its own.
	if strings.HasSuffix(filename, SchemaExtension) {
		_, d := ParseSchemaOpts(src, filename, opts.ParseOptions)
		return append(diags, d...)
	}

	schema, sd := SchemaFor(file.Body, filename, opts)
	diags = append(diags, sd...)
	if schema == nil {
		return diags
	}
	return append(diags, schema.ValidateOpts(file.Body, opts.ValidateOptions)...)
}

// ValidateFileLinked reads an instance from disk and checks it against the
// schema named by its `__schema` attribute. A file without one is reported as
// having no diagnostics.
func ValidateFileLinked(path string, opts LoadOptions) hcl.Diagnostics {
	loader := opts.ParseOptions.Loader
	if loader == nil {
		loader = DefaultLoader
	}
	src, resolved, diags := loader.Load(path, "", "")
	if diags.HasErrors() {
		return diags
	}
	return append(diags, ValidateSource(src, resolved, opts)...)
}

// ValidateHCLWithLinkedSchema checks an instance on disk against the schema
// named by its `__schema` attribute.
func ValidateHCLWithLinkedSchema(hclPath string) hcl.Diagnostics {
	return ValidateFileLinked(hclPath, LoadOptions{})
}

// ValidateFileWithSchema checks an instance on disk against an explicit
// schema.
//
// When the instance is itself a `*.schema.hcl`, it is checked against the
// meta-schema and schemaPath is ignored; that was the original behaviour and
// is now reported as a warning rather than passing silently.
func ValidateFileWithSchema(schemaPath, hclPath string) hcl.Diagnostics {
	return ValidateWithSchema(schemaPath, hclPath, LoadOptions{})
}

// ValidateWithSchema checks an instance against an explicit schema, with
// options.
func ValidateWithSchema(schemaPath, hclPath string, opts LoadOptions) hcl.Diagnostics {
	loader := opts.ParseOptions.Loader
	if loader == nil {
		loader = DefaultLoader
	}

	if strings.HasSuffix(hclPath, SchemaExtension) {
		diags := hcl.Diagnostics{{
			Severity: hcl.DiagWarning,
			Summary:  "Explicit schema ignored",
			Detail: fmt.Sprintf("%s is itself a schema document, so it is checked against the meta-schema and %s is not used.",
				hclPath, schemaPath),
		}}
		src, resolved, d := loader.Load(hclPath, "", "")
		diags = append(diags, d...)
		if d.HasErrors() {
			return diags
		}
		_, pd := ParseSchemaOpts(src, resolved, opts.ParseOptions)
		return append(diags, pd...)
	}

	schemaSrc, schemaName, diags := loader.Load(schemaPath, "", "")
	if diags.HasErrors() {
		return diags
	}
	po := opts.ParseOptions
	po.Loader = loader
	schema, sd := ParseSchemaOpts(schemaSrc, schemaName, po)
	diags = append(diags, sd...)
	if schema == nil {
		return diags
	}

	src, resolved, d := loader.Load(hclPath, "", "")
	diags = append(diags, d...)
	if d.HasErrors() {
		return diags
	}
	parser := hclparse.NewParser()
	file, pd := parseSource(parser, src, resolved)
	diags = append(diags, pd...)
	if pd.HasErrors() || file == nil {
		return diags
	}
	return append(diags, schema.ValidateOpts(file.Body, opts.ValidateOptions)...)
}
