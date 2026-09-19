package hclschema

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

// --- Decoding -----------------------------------------------------------------

func TestDecodeAppliesTypesAndDefaults(t *testing.T) {
	s, diags := ParseSchemaFile(td("typed.schema.hcl"))
	assertNoErrors(t, diags)

	f := parseForTest(t, td("typed_ok.hcl"))
	val, d := s.Decode(f.Body, nil)
	assertNoErrors(t, d)

	m := val.AsValueMap()
	if got := m["name"]; got.AsString() != "web" {
		t.Fatalf("name = %v", got)
	}
	port, _ := m["port"].AsBigFloat().Float64()
	if port != 443 {
		t.Fatalf("port = %v", port)
	}
	if !m["debug"].True() {
		t.Fatal("debug should decode as true")
	}
}

func TestDecodeSubstitutesDefault(t *testing.T) {
	s, diags := ParseSchemaFile(td("typed.schema.hcl"))
	assertNoErrors(t, diags)

	f, pd := parseSource(hclparse.NewParser(), []byte(`name = "web"`), "x.hcl")
	assertNoErrors(t, pd)

	val, d := s.Decode(f.Body, nil)
	assertNoErrors(t, d)
	port, _ := val.AsValueMap()["port"].AsBigFloat().Float64()
	if port != 8080 {
		t.Fatalf("default not applied: port = %v", port)
	}
}

func TestDecodeInto(t *testing.T) {
	s, diags := ParseSchemaFile(td("cardinality.schema.hcl"))
	assertNoErrors(t, diags)

	f := parseForTest(t, td("cardinality_ok.hcl"))
	var target struct {
		Listener []struct {
			Name string  `json:"name"`
			Port float64 `json:"port"`
		} `json:"listener"`
	}
	assertNoErrors(t, s.DecodeInto(f.Body, nil, &target))

	if len(target.Listener) != 2 {
		t.Fatalf("decoded %d listeners, want 2", len(target.Listener))
	}
	if target.Listener[0].Name != "http" || target.Listener[0].Port != 80 {
		t.Fatalf("unexpected first listener: %+v", target.Listener[0])
	}
}

// A schema that recurses cannot become a finite spec, so the recursion is cut
// and reported rather than hanging.
func TestDecodeSpecTruncatesRecursion(t *testing.T) {
	path := filepath.Join("..", "..", "schema", "draft", "2026-09", ".schema.hcl")
	s, diags := ParseSchemaFile(path)
	assertNoErrors(t, diags)

	_, d := s.DecodeSpec()
	found := false
	for _, dg := range d {
		if dg.Summary == "Recursive schema truncated" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a truncation warning, got %v", d)
	}
	if d.HasErrors() {
		t.Fatalf("truncation must not be an error: %v", d)
	}
}

// --- Rendering and bundling ------------------------------------------------------

func TestBytesRoundTrips(t *testing.T) {
	for _, name := range []string{
		"typed.schema.hcl",
		"cardinality.schema.hcl",
		"crossfield.schema.hcl",
		"variant.schema.hcl",
	} {
		t.Run(name, func(t *testing.T) {
			original, diags := ParseSchemaFile(td(name))
			assertNoErrors(t, diags)

			rendered := original.Bytes()
			reparsed, d := ParseSchema(rendered, td(name))
			assertNoErrors(t, d)

			// Rendering again must be a fixed point, which is what makes
			// `hclschema fmt` idempotent.
			if !bytes.Equal(rendered, reparsed.Bytes()) {
				t.Fatalf("render is not idempotent for %s:\n--- first ---\n%s\n--- second ---\n%s",
					name, rendered, reparsed.Bytes())
			}
		})
	}
}

// Writing a self-recursive schema used to loop forever; shared bodies get a
// generated id instead.
func TestBytesHandlesRecursiveSchema(t *testing.T) {
	path := filepath.Join("..", "..", "schema", "draft", "2026-09", ".schema.hcl")
	s, diags := ParseSchemaFile(path)
	assertNoErrors(t, diags)

	rendered := s.Bytes()
	if !strings.Contains(string(rendered), "ref = block_header.") {
		t.Fatalf("expected a generated ref in the output:\n%s", rendered)
	}
	reparsed, d := ParseSchema(rendered, "rendered.schema.hcl")
	assertNoErrors(t, d)

	// The rendered copy must still accept the original document.
	f := parseForTest(t, path)
	assertNoErrors(t, reparsed.Validate(f.Body))
}

func TestBundleRemovesImports(t *testing.T) {
	out, diags := BundleFile(td("importer.schema.hcl"), ParseOptions{Loader: &Loader{Offline: true}})
	assertNoErrors(t, diags)
	if strings.Contains(string(out), "import ") {
		t.Fatalf("bundle still has imports:\n%s", out)
	}

	bundled, d := ParseSchema(out, "bundled.schema.hcl")
	assertNoErrors(t, d)

	f := parseForTest(t, td("importer.hcl"))
	assertNoErrors(t, bundled.Validate(f.Body))
}

// --- Inference ---------------------------------------------------------------------

func TestInferProducesUsableSchema(t *testing.T) {
	schema, diags := InferFromPaths([]string{td("typed_ok.hcl")}, InferOptions{ID: "local://inferred"})
	assertNoErrors(t, diags)

	rendered := schema.Bytes()
	reparsed, d := ParseSchema(rendered, "inferred.schema.hcl")
	assertNoErrors(t, d)

	if reparsed.Body.Attribute("name") == nil {
		t.Fatalf("inferred schema lost an attribute:\n%s", rendered)
	}
	// The link attribute belongs to the instance, not to the inferred shape.
	if reparsed.Body.Attribute("__schema") != nil {
		t.Fatal("__schema must not be inferred as a declared attribute")
	}

	f := parseForTest(t, td("typed_ok.hcl"))
	assertNoErrors(t, reparsed.Validate(f.Body))
}

func TestInferPicksUpBlocksAndLabels(t *testing.T) {
	schema, diags := InferFromPaths([]string{td("cardinality_ok.hcl")}, InferOptions{})
	assertNoErrors(t, diags)

	listener := schema.Body.Block("listener", 1)
	if listener == nil {
		t.Fatalf("listener block not inferred: %s", schema.Bytes())
	}
	if listener.Body.Attribute("port") == nil {
		t.Fatal("nested port attribute not inferred")
	}
}

// Disagreeing samples must not produce a type constraint that would then
// reject one of them.
func TestInferDoesNotGuessConflictingTypes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := writeFilePrivate(p, []byte(body)); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := write("a.hcl", "x = 1\n")
	b := write("b.hcl", "x = \"one\"\n")

	schema, diags := InferFromPaths([]string{a, b}, InferOptions{})
	assertNoErrors(t, diags)
	if got := schema.Body.Attribute("x"); got == nil || got.Type != cty.NilType {
		t.Fatalf("expected no type constraint, got %v", got)
	}
}

// --- Documentation --------------------------------------------------------------------

func TestMarkdownIncludesDescriptions(t *testing.T) {
	path := filepath.Join("..", "..", "schema", "draft", "2026-09", ".schema.hcl")
	s, diags := ParseSchemaFile(path)
	assertNoErrors(t, diags)

	md := string(s.Markdown(MarkdownOptions{Title: "Meta-schema"}))
	for _, want := range []string{
		"# Meta-schema",
		"| Attribute | Type | Required | Default | Description |",
		"A type constraint expression",
		"`body`",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("generated docs missing %q:\n%s", want, md)
		}
	}
}

func TestMarkdownTerminatesOnRecursiveSchema(t *testing.T) {
	path := filepath.Join("..", "..", "schema", "draft", "2026-09", ".schema.hcl")
	s, diags := ParseSchemaFile(path)
	assertNoErrors(t, diags)
	if len(s.Markdown(MarkdownOptions{})) == 0 {
		t.Fatal("expected output")
	}
}

// --- Cursor (editor support) --------------------------------------------------------

func TestCursorCompletionAtRoot(t *testing.T) {
	s, diags := ParseSchemaFile(td("typed.schema.hcl"))
	assertNoErrors(t, diags)

	f, pd := parseSource(hclparse.NewParser(), []byte("name = \"a\"\n"), "x.hcl")
	assertNoErrors(t, pd)

	cursor := s.At(f, hcl.Pos{Line: 2, Column: 1, Byte: 11})
	labels := map[string]bool{}
	for _, c := range cursor.Completions() {
		labels[c.Label] = true
	}
	for _, want := range []string{"name", "port", "mode", "tags"} {
		if !labels[want] {
			t.Fatalf("completion %q missing from %v", want, labels)
		}
	}
}

func TestCursorCompletionInsideBlock(t *testing.T) {
	s, diags := ParseSchemaFile(td("cardinality.schema.hcl"))
	assertNoErrors(t, diags)

	src := []byte("listener \"http\" {\n  \n}\n")
	f, pd := parseSource(hclparse.NewParser(), src, "x.hcl")
	assertNoErrors(t, pd)

	cursor := s.At(f, hcl.Pos{Line: 2, Column: 3, Byte: 20})
	if cursor.Block == nil || cursor.Block.Type != "listener" {
		t.Fatalf("cursor did not land in the listener block: %+v", cursor.Block)
	}
	got := cursor.Completions()
	if len(got) != 1 || got[0].Label != "port" {
		t.Fatalf("expected only port, got %+v", got)
	}
}

func TestCursorEnumCompletion(t *testing.T) {
	s, diags := ParseSchemaFile(td("typed.schema.hcl"))
	assertNoErrors(t, diags)

	src := []byte(`mode = "fast"`)
	f, pd := parseSource(hclparse.NewParser(), src, "x.hcl")
	assertNoErrors(t, pd)

	cursor := s.At(f, hcl.Pos{Line: 1, Column: 10, Byte: 9})
	got := cursor.Completions()
	if len(got) != 2 {
		t.Fatalf("expected the two enum values, got %+v", got)
	}
	for _, c := range got {
		if c.Kind != CompletionValue {
			t.Fatalf("expected value completions, got %v", c.Kind)
		}
	}
}

func TestCursorHoverAndDefinition(t *testing.T) {
	s, diags := ParseSchemaFile(td("typed.schema.hcl"))
	assertNoErrors(t, diags)

	f, pd := parseSource(hclparse.NewParser(), []byte(`port = 80`), "x.hcl")
	assertNoErrors(t, pd)

	cursor := s.At(f, hcl.Pos{Line: 1, Column: 2, Byte: 1})
	doc := cursor.Doc()
	if !strings.Contains(doc, "port") || !strings.Contains(doc, "number") {
		t.Fatalf("unexpected hover text: %q", doc)
	}
	if !strings.Contains(doc, "8080") {
		t.Fatalf("hover should mention the default: %q", doc)
	}

	rng, ok := cursor.DefinitionRange()
	if !ok {
		t.Fatal("expected a definition range")
	}
	if !strings.HasSuffix(filepath.ToSlash(rng.Filename), "typed.schema.hcl") {
		t.Fatalf("definition points at %q", rng.Filename)
	}
}

// --- Legacy API shim -----------------------------------------------------------------

func TestLegacyProjection(t *testing.T) {
	s, diags := ParseSchemaFile(td("simple.schema.hcl"))
	assertNoErrors(t, diags)

	legacy := s.Legacy()
	if legacy == nil || legacy.BodySchema != s.Body {
		t.Fatal("Legacy should expose the same body")
	}

	bs := legacy.BodySchema.AsBodySchema()
	if len(bs.Attributes) == 0 || len(bs.Blocks) == 0 {
		t.Fatalf("AsBodySchema lost content: %+v", bs)
	}
}

// AsBodySchema must not emit a block type twice; hcl.BodySchema keys by type
// and would silently keep the last one.
func TestAsBodySchemaDeduplicatesBlockTypes(t *testing.T) {
	s, diags := ParseSchemaFile(td("duplicate_same_level.schema.hcl"))
	assertNoErrors(t, diags)

	bs := s.Body.AsBodySchema()
	seen := map[string]int{}
	for _, b := range bs.Blocks {
		seen[b.Type]++
	}
	for typ, n := range seen {
		if n > 1 {
			t.Fatalf("block type %q emitted %d times", typ, n)
		}
	}
}

func TestValidateFileWithSchemaWarnsForSchemaTarget(t *testing.T) {
	diags := ValidateFileWithSchema(td("simple.schema.hcl"), td("nested.schema.hcl"))
	found := false
	for _, d := range diags {
		if d.Summary == "Explicit schema ignored" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a warning that the explicit schema was ignored, got %v", diags)
	}
}
