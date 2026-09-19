---
id: go-library
title: Go library
sidebar_position: 5
---

# Go library

```bash
go get github.com/avestura/hcl-schema
```

```go
import "github.com/avestura/hcl-schema/pkg/hclschema"
```

## Loading a schema

Every loader takes a source rather than only a path, so a schema can come from
memory, an embedded filesystem, or disk:

```go
func ParseSchema(src []byte, filename string) (*Schema, hcl.Diagnostics)
func ParseSchemaFile(filename string) (*Schema, hcl.Diagnostics)
func ParseSchemaFS(fsys fs.FS, name string, opts ParseOptions) (*Schema, hcl.Diagnostics)

func ParseSchemaOpts(src []byte, filename string, opts ParseOptions) (*Schema, hcl.Diagnostics)
func ParseSchemaFileOpts(filename string, opts ParseOptions) (*Schema, hcl.Diagnostics)
```

```go
//go:embed schemas
var schemas embed.FS

schema, diags := hclschema.ParseSchemaFS(schemas, "schemas/service.schema.hcl", hclschema.ParseOptions{})
```

`ParseOptions` carries the `Loader`, the `Strict` switch and `MaxDepth` for
import recursion.

## Checking a document

`Check` is the usual entry point. It returns everything one check produced,
including the parsed files, which is what you need to render diagnostics with
source snippets:

```go
res := hclschema.CheckFile("service.hcl", hclschema.LoadOptions{})
if res.HasErrors() {
    wr := hcl.NewDiagnosticTextWriter(os.Stderr, res.Files, 80, true)
    for _, d := range res.Diagnostics {
        wr.WriteDiagnostic(d)
    }
}
```

```go
type Result struct {
    Filename    string
    File        *hcl.File
    Schema      *Schema           // nil when the instance links to none
    Files       map[string]*hcl.File
    Diagnostics hcl.Diagnostics
}

func Check(src []byte, filename string, opts LoadOptions) Result
func CheckFile(path string, opts LoadOptions) Result
```

To validate against a schema you already hold:

```go
func (s *Schema) Validate(body hcl.Body) hcl.Diagnostics
func (s *Schema) ValidateFile(f *hcl.File) hcl.Diagnostics
func (s *Schema) ValidateOpts(body hcl.Body, opts ValidateOptions) hcl.Diagnostics
```

`ValidateOptions.EvalContext` supplies variables and functions. Without one,
an expression the validator cannot evaluate statically is left unchecked rather
than reported, because only your application knows what `var.foo` means.

Reading the link without validating:

```go
func FindSchemaLink(body hcl.Body) (SchemaLink, hcl.Diagnostics)
func SchemaFor(body hcl.Body, filename string, opts LoadOptions) (*Schema, hcl.Diagnostics)
```

## Decoding

The same schema produces an
[`hcldec.Spec`](https://pkg.go.dev/github.com/hashicorp/hcl/v2/hcldec), so one
document both validates and decodes:

```go
func (s *Schema) DecodeSpec() (hcldec.Spec, hcl.Diagnostics)
func (s *Schema) Decode(body hcl.Body, ctx *hcl.EvalContext) (cty.Value, hcl.Diagnostics)
func (s *Schema) DecodeInto(body hcl.Body, ctx *hcl.EvalContext, target any) hcl.Diagnostics
```

```go
var cfg struct {
    Name     string `json:"name"`
    Listener []struct {
        Name string  `json:"name"`
        Port float64 `json:"port"`
    } `json:"listener"`
}
diags := schema.DecodeInto(file.Body, nil, &cfg)
```

Declared `default` values are substituted during decoding. Block labels become
fields named after `label_names`. A block with `max_items = 1` decodes to a
single object; anything else decodes to a list.

A recursive schema cannot become a finite spec, so `DecodeSpec` cuts the
recursion where it closes and reports a warning rather than hanging.

## The projections

A `FullBodySchema` projects onto both HCL layers:

```go
func (fbs *FullBodySchema) AsBodySchema() *hcl.BodySchema   // the shallow shape
func (s *Schema) DecodeSpec() (hcldec.Spec, hcl.Diagnostics) // shape + types + cardinality
```

`AsBodySchema` is the original core profile and is stable: a schema using
nothing beyond `required` and `label_names` round-trips through it unchanged.

## Loading remote schemas

```go
type Loader struct {
    Cache                  Cache
    HTTPClient             *http.Client
    FS                     fs.FS
    Offline                bool
    MaxSize                int64
    TTL                    time.Duration
    Timeout                time.Duration
    AllowCrossHostRedirect bool
}
```

The defaults are covered in [Security](./security.md). `Cache` is an interface,
with `DiskCache` and `MemoryCache` provided; a long-running process such as the
language server puts a `MemoryCache` in front.

```go
loader := &hclschema.Loader{
    Cache:   hclschema.NewMemoryCache(),
    Offline: true,
}
res := hclschema.CheckFile("service.hcl", hclschema.LoadOptions{
    ParseOptions: hclschema.ParseOptions{Loader: loader},
})
```

## Generating

```go
func Infer(files []*hcl.File, opts InferOptions) (*Schema, hcl.Diagnostics)
func InferFromPaths(paths []string, opts InferOptions) (*Schema, hcl.Diagnostics)

func (s *Schema) Bytes() []byte
func (s *Schema) WriteHCL(w io.Writer) error
func (s *Schema) Bundle() ([]byte, hcl.Diagnostics)
func (s *Schema) Markdown(opts MarkdownOptions) []byte
```

`Bytes` renders through `hclwrite`, so the output is canonically formatted and
rendering is idempotent — which is what makes `hclschema fmt --check`
meaningful. Bodies shared through `ref`, including recursive ones, are emitted
once with a generated `id`.

## Editor support

`Schema.At` resolves what the schema says about a position, using HCL's own
`BlocksAtPos` and `AttributeAtPos`. It is the basis for the language server and
is usable directly:

```go
cursor := schema.At(file, hcl.Pos{Line: 12, Column: 3, Byte: 210})

for _, c := range cursor.Completions() {
    fmt.Println(c.Label, c.Kind, c.Detail)
}

fmt.Println(cursor.Doc())                  // Markdown hover text
rng, ok := cursor.DefinitionRange()        // where it is declared in the schema
```

`Completions` offers the declarations available in the enclosing body, or — when
the cursor is on an attribute with an `enum` — that attribute's permitted
values.

## Compatibility

`ValidateFileWithSchema` and `ValidateHCLWithLinkedSchema` keep their original
signatures and behaviour.

`ParseSchemaFile` now returns `*Schema` rather than
`*BlockHeaderAndBodySchema`. `Schema.Legacy()` returns the old shape:

```go
schema, diags := hclschema.ParseSchemaFile("service.schema.hcl")
legacy := schema.Legacy()            // *BlockHeaderAndBodySchema
body := legacy.BodySchema            // *FullBodySchema, as before
```
