package hclschema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
)

func td(name string) string { return filepath.Join("testdata", name) }

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func parseForTest(t *testing.T, path string) *hcl.File {
	t.Helper()
	f, diags := parseSource(hclparse.NewParser(), mustRead(t, path), path)
	if diags.HasErrors() {
		t.Fatalf("parse %s: %v", path, diags)
	}
	return f
}

// checkLocal validates an instance without touching the network. Every schema
// referenced from testdata is local, so an offline loader keeps the suite
// hermetic.
func checkLocal(path string) Result {
	return CheckFile(path, LoadOptions{
		ParseOptions: ParseOptions{Loader: &Loader{Offline: true}},
	})
}

func errorSummaries(diags hcl.Diagnostics) []string {
	var out []string
	for _, d := range diags {
		if d.Severity == hcl.DiagError {
			out = append(out, d.Summary)
		}
	}
	return out
}

func assertNoErrors(t *testing.T, diags hcl.Diagnostics) {
	t.Helper()
	if diags.HasErrors() {
		t.Fatalf("unexpected errors:\n%s", diags.Error())
	}
}

func assertErrorContaining(t *testing.T, diags hcl.Diagnostics, want string) {
	t.Helper()
	for _, d := range diags {
		if d.Severity != hcl.DiagError {
			continue
		}
		if strings.Contains(d.Summary, want) || strings.Contains(d.Detail, want) {
			return
		}
	}
	t.Fatalf("expected an error mentioning %q, got: %v", want, errorSummaries(diags))
}

// --- Draft 2025-10 compatibility -------------------------------------------
//
// These cover documents written before the type system existed. They must keep
// behaving exactly as they did.

func TestLegacySimpleSchemaParses(t *testing.T) {
	s, diags := ParseSchemaFile(td("simple.schema.hcl"))
	assertNoErrors(t, diags)
	if s.Draft != Draft2025_10 {
		t.Fatalf("draft = %v, want 2025-10", s.Draft)
	}

	myattr := s.Body.Attribute("myattr")
	if myattr == nil || !myattr.Required {
		t.Fatalf("myattr missing or not required: %+v", myattr)
	}
	tag := s.Body.Block("tag", 1)
	if tag == nil {
		t.Fatal("tag block not found")
	}
	if len(tag.LabelNames) != 1 || tag.LabelNames[0] != "name" {
		t.Fatalf("unexpected label names: %v", tag.LabelNames)
	}
	if tag.Body == nil {
		t.Fatal("tag has no nested body")
	}
}

func TestLegacyValidateSimple(t *testing.T) {
	assertNoErrors(t, checkLocal(td("simple_linked.hcl")).Diagnostics)
}

func TestLegacyValidateNested(t *testing.T) {
	assertNoErrors(t, checkLocal(td("nested_linked.hcl")).Diagnostics)
}

func TestLegacyExcessAttributeRejected(t *testing.T) {
	res := checkLocal(td("nested_linked_with_excess_attr.hcl"))
	assertErrorContaining(t, res.Diagnostics, "Unsupported argument")
}

func TestLegacyMissingRequiredAttribute(t *testing.T) {
	res := checkLocal(td("missing_required_attr.hcl"))
	assertErrorContaining(t, res.Diagnostics, "Missing required argument")
}

func TestLegacyDuplicateAttributeInInstance(t *testing.T) {
	res := checkLocal(td("duplicate_attr.hcl"))
	if !res.HasErrors() {
		t.Fatal("expected an error for a duplicated attribute")
	}
}

func TestLegacyLabelCountMismatch(t *testing.T) {
	res := checkLocal(td("label_count_mismatch.hcl"))
	if !res.HasErrors() {
		t.Fatal("expected an error for a label count mismatch")
	}
}

func TestLegacyMultipleBlockInstancesAllowed(t *testing.T) {
	assertNoErrors(t, checkLocal(td("multiple_tags.hcl")).Diagnostics)
}

// A block type declared twice at the same level was silently first-match-wins
// before 2026-09. It stays a warning there, so the document still parses.
func TestLegacyDuplicateBlockIsWarningNotError(t *testing.T) {
	s, diags := ParseSchemaFile(td("duplicate_same_level.schema.hcl"))
	assertNoErrors(t, diags)
	if s == nil || s.Body == nil {
		t.Fatal("expected a parsed body")
	}
	found := false
	for _, d := range diags {
		if d.Severity == hcl.DiagWarning && d.Summary == "Duplicate declaration" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a duplicate-declaration warning, got %v", diags)
	}
}

func TestLegacyDuplicateBlockTypesAtDifferentLevels(t *testing.T) {
	_, diags := ParseSchemaFile(td("duplicate_levels.schema.hcl"))
	assertNoErrors(t, diags)
}

func TestLegacyRefResolvesToSharedPointer(t *testing.T) {
	s, diags := ParseSchemaFile(td("ref_id_body.schema.hcl"))
	assertNoErrors(t, diags)
	foo := s.Body.Block("foo", 2)
	bar := s.Body.Block("bar", 0)
	if foo == nil || bar == nil {
		t.Fatalf("expected both foo and bar: %+v %+v", foo, bar)
	}
	if foo.Body != bar.Body {
		t.Fatal("ref should resolve to the very same body, not a copy")
	}
}

func TestUnresolvedRefIsReported(t *testing.T) {
	_, diags := ParseSchemaFile(td("ref_id_body_fail.schema.hcl"))
	assertErrorContaining(t, diags, "Unresolved ref")
}

// --- Bugs fixed --------------------------------------------------------------

// The link was previously found by scanning the text for "__schema", which
// matched inside a comment.
func TestSchemaLinkInCommentIsIgnored(t *testing.T) {
	res := checkLocal(td("comment_link.hcl"))
	assertNoErrors(t, res.Diagnostics)
	if res.Schema != nil {
		t.Fatal("a commented-out __schema must not be honoured")
	}
}

// `id=` and `ref=` written without surrounding spaces used to produce keys like
// "=block_header.foo".
func TestRefIsNotWhitespaceSensitive(t *testing.T) {
	assertNoErrors(t, checkLocal(td("nospace.hcl")).Diagnostics)
}

// A ref to an id declared later in the document used to fail.
func TestForwardRefResolves(t *testing.T) {
	assertNoErrors(t, checkLocal(td("forward_ref.hcl")).Diagnostics)
}

// hclparse.ParseHCLFile is native-syntax only; routing by extension is what
// makes .hcl.json work.
func TestJSONSyntaxInstance(t *testing.T) {
	assertNoErrors(t, checkLocal(td("json_instance.hcl.json")).Diagnostics)
}

// The meta-schema describes itself, so parsing it and then checking it as an
// instance of itself must both succeed.
func TestMetaSchemaSelfValidates(t *testing.T) {
	for _, draft := range []string{"2025-10", "2026-09"} {
		t.Run(draft, func(t *testing.T) {
			path := filepath.Join("..", "..", "schema", "draft", draft, ".schema.hcl")
			s, diags := ParseSchemaFile(path)
			assertNoErrors(t, diags)

			f := parseForTest(t, path)
			assertNoErrors(t, s.Validate(f.Body))
		})
	}
}

// --- Draft 2026-09 strictness ------------------------------------------------

func TestStrictClosesBodylessBlocks(t *testing.T) {
	res := checkLocal(td("strict_closed.hcl"))
	assertErrorContaining(t, res.Diagnostics, "Unsupported argument")
}

func TestOpenBodyAllowsUndeclaredContent(t *testing.T) {
	assertNoErrors(t, checkLocal(td("strict_ok.hcl")).Diagnostics)
}

func TestStrictRejectsDuplicateDeclaration(t *testing.T) {
	src := []byte(`
__schema = "` + Draft2026_09.URL() + `"
__id     = "local://dup"

body {
  attribute "a" {
    type = string
  }
  attribute "a" {
    type = number
  }
}
`)
	_, diags := ParseSchema(src, "dup.schema.hcl")
	assertErrorContaining(t, diags, "Duplicate declaration")
}

// The Strict option applies the new rules to a document that pins the old
// draft, which is how a repository migrates one file at a time.
func TestStrictOptionUpgradesOlderDraft(t *testing.T) {
	res := CheckFile(td("simple_linked.hcl"), LoadOptions{
		ParseOptions:    ParseOptions{Loader: &Loader{Offline: true}, Strict: true},
		ValidateOptions: ValidateOptions{Strict: true},
	})
	assertNoErrors(t, res.Diagnostics)
	if res.Schema.Draft.effective(true) != Draft2026_09 {
		t.Fatal("expected the strict option to raise the effective draft")
	}
}

// --- Types and value constraints ---------------------------------------------

func TestTypedInstanceAccepted(t *testing.T) {
	assertNoErrors(t, checkLocal(td("typed_ok.hcl")).Diagnostics)
}

func TestTypedInstanceRejected(t *testing.T) {
	diags := checkLocal(td("typed_bad.hcl")).Diagnostics
	for _, want := range []string{
		"Value does not match pattern", // name
		"Value above maximum",          // port
		"Value not permitted",          // mode
		"Incorrect attribute value type",
		"replicas must be odd",
	} {
		assertErrorContaining(t, diags, want)
	}
}

func TestDefaultOnRequiredAttributeIsRejected(t *testing.T) {
	src := []byte(`
__schema = "` + Draft2026_09.URL() + `"
__id     = "local://baddefault"

body {
  attribute "a" {
    type     = string
    required = true
    default  = "x"
  }
}
`)
	_, diags := ParseSchema(src, "bad.schema.hcl")
	assertErrorContaining(t, diags, "Default on a required attribute")
}

func TestDefaultMustMatchDeclaredType(t *testing.T) {
	src := []byte(`
__schema = "` + Draft2026_09.URL() + `"
__id     = "local://baddefault2"

body {
  attribute "a" {
    type    = number
    default = "not a number"
  }
}
`)
	_, diags := ParseSchema(src, "bad.schema.hcl")
	assertErrorContaining(t, diags, "Invalid default value")
}

// An expression the validator cannot evaluate statically is left alone rather
// than reported, because the embedding application may supply the context.
func TestUnevaluableExpressionIsNotReported(t *testing.T) {
	s, diags := ParseSchemaFile(td("typed.schema.hcl"))
	assertNoErrors(t, diags)

	f, pd := parseSource(hclparse.NewParser(), []byte(`
name = lower(var.service)
`), "x.hcl")
	assertNoErrors(t, pd)
	assertNoErrors(t, s.Validate(f.Body))
}

func TestSensitiveValueIsRedacted(t *testing.T) {
	src := []byte(`
__schema = "` + Draft2026_09.URL() + `"
__id     = "local://secret"

body {
  attribute "token" {
    type      = string
    sensitive = true
    pattern   = "^tok_"
  }
}
`)
	s, diags := ParseSchema(src, "secret.schema.hcl")
	assertNoErrors(t, diags)

	f, pd := parseSource(hclparse.NewParser(), []byte(`token = "hunter2"`), "x.hcl")
	assertNoErrors(t, pd)
	out := s.Validate(f.Body)
	if !out.HasErrors() {
		t.Fatal("expected a pattern error")
	}
	if strings.Contains(out.Error(), "hunter2") {
		t.Fatalf("a sensitive value leaked into diagnostics: %s", out.Error())
	}
}

func TestDeprecatedProducesWarningNotError(t *testing.T) {
	src := []byte(`
__schema = "` + Draft2026_09.URL() + `"
__id     = "local://dep"

body {
  attribute "old" {
    type       = string
    deprecated = "use new instead"
  }
}
`)
	s, diags := ParseSchema(src, "dep.schema.hcl")
	assertNoErrors(t, diags)

	f, pd := parseSource(hclparse.NewParser(), []byte(`old = "x"`), "x.hcl")
	assertNoErrors(t, pd)
	out := s.Validate(f.Body)
	assertNoErrors(t, out)
	if len(out) != 1 || out[0].Severity != hcl.DiagWarning {
		t.Fatalf("expected exactly one warning, got %v", out)
	}
}

// --- Cardinality and labels ---------------------------------------------------

func TestCardinalityAccepted(t *testing.T) {
	assertNoErrors(t, checkLocal(td("cardinality_ok.hcl")).Diagnostics)
}

func TestCardinalityRejected(t *testing.T) {
	diags := checkLocal(td("cardinality_bad.hcl")).Diagnostics
	for _, want := range []string{
		"Too many blocks",
		"Duplicate block labels",
		"Invalid block label",
	} {
		assertErrorContaining(t, diags, want)
	}
}

func TestMinItemsRequiresBlock(t *testing.T) {
	s, diags := ParseSchemaFile(td("cardinality.schema.hcl"))
	assertNoErrors(t, diags)

	f, pd := parseSource(hclparse.NewParser(), []byte("\n"), "empty.hcl")
	assertNoErrors(t, pd)
	assertErrorContaining(t, s.Validate(f.Body), "Insufficient blocks")
}

// --- Cross-field constraints ---------------------------------------------------

func TestCrossFieldConstraints(t *testing.T) {
	s, diags := ParseSchemaFile(td("crossfield.schema.hcl"))
	assertNoErrors(t, diags)

	cases := []struct {
		name string
		src  string
		want string
	}{
		{"conflicts", `inline = "a"` + "\n" + `from_file = "b"` + "\n" + `checksum = "c"`, "Conflicting attributes"},
		{"neither", "\n", "Exactly one"},
		// checksum declares required_with = ["from_file"], so setting the
		// checksum without a from_file is what the rule forbids.
		{"required_with", `inline = "a"` + "\n" + `checksum = "c"`, "requires"},
		{"ok", "inline = \"a\"\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, pd := parseSource(hclparse.NewParser(), []byte(tc.src), "x.hcl")
			assertNoErrors(t, pd)
			out := s.Validate(f.Body)
			if tc.want == "" {
				assertNoErrors(t, out)
				return
			}
			assertErrorContaining(t, out, tc.want)
		})
	}
}

// --- Variants -------------------------------------------------------------------

func TestVariantAccepted(t *testing.T) {
	assertNoErrors(t, checkLocal(td("variant_ok.hcl")).Diagnostics)
}

func TestVariantRejectedWhenNoneMatch(t *testing.T) {
	assertErrorContaining(t, checkLocal(td("variant_bad.hcl")).Diagnostics, "No matching variant")
}

// --- Imports ---------------------------------------------------------------------

func TestImportSharesDeclarations(t *testing.T) {
	assertNoErrors(t, checkLocal(td("importer.hcl")).Diagnostics)
}

func TestImportCycleIsReported(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.schema.hcl")
	b := filepath.Join(dir, "b.schema.hcl")
	write := func(path, other string) {
		src := "__schema = \"" + Draft2026_09.URL() + "\"\n" +
			"__id = \"local://" + filepath.Base(path) + "\"\n\n" +
			"import \"other\" {\n  source = \"./" + other + "\"\n}\n\nbody {\n}\n"
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(a, filepath.Base(b))
	write(b, filepath.Base(a))

	_, diags := ParseSchemaFileOpts(a, ParseOptions{Loader: &Loader{Offline: true}})
	assertErrorContaining(t, diags, "Import cycle")
}
