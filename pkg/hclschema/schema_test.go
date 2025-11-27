package hclschema

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/hcl/v2/hclparse"
)

func TestParseSimpleSchema(t *testing.T) {
	path := filepath.Join("testdata", "simple.schema.hcl")
	res, diags := ParseSchemaFile(path)
	if diags.HasErrors() {
		t.Fatalf("diagnostics had errors: %v", diags)
	}
	if res == nil || res.BodySchema == nil {
		t.Fatalf("expected non-nil resulting body schema")
	}

	found := false
	for _, a := range res.BodySchema.Attributes {
		if a.Name == "myattr" {
			found = true
			if !a.Required {
				t.Fatalf("expected myattr to be required")
			}
		}
	}
	if !found {
		t.Fatalf("attribute myattr not found")
	}

	foundBlock := false
	for _, b := range res.BodySchema.Blocks {
		if b.Type == "tag" {
			foundBlock = true
			if len(b.LabelNames) != 1 || b.LabelNames[0] != "name" {
				t.Fatalf("unexpected label names for tag: %v", b.LabelNames)
			}
			if b.BodySchema == nil {
				t.Fatalf("expected nested body schema for tag block")
			}
		}
	}
	if !foundBlock {
		t.Fatalf("tag block not found")
	}
}

func TestValidateSimpleHCL(t *testing.T) {
	schemaPath := filepath.Join("testdata", "simple.schema.hcl")
	res, diags := ParseSchemaFile(schemaPath)
	if diags.HasErrors() {
		t.Fatalf("schema parse diagnostics had errors: %v", diags)
	}
	if res == nil || res.BodySchema == nil {
		t.Fatalf("expected non-nil resulting body schema")
	}

	schema := res.BodySchema.AsBodySchema()

	parser := hclparse.NewParser()
	filePath := filepath.Join("testdata", "simple.hcl")
	file, diags := parser.ParseHCLFile(filePath)
	if diags.HasErrors() {
		t.Fatalf("failed parsing hcl file: %v", diags)
	}

	_, diags = file.Body.Content(schema)
	if diags.HasErrors() {
		t.Fatalf("hcl validation diagnostics: %v", diags)
	}
}

func TestValidateNestedHCL(t *testing.T) {
	schemaPath := filepath.Join("testdata", "nested.schema.hcl")
	res, diags := ParseSchemaFile(schemaPath)
	if diags.HasErrors() {
		t.Fatalf("schema parse diagnostics had errors: %v", diags)
	}
	if res == nil || res.BodySchema == nil {
		t.Fatalf("expected non-nil resulting body schema")
	}

	schema := res.BodySchema.AsBodySchema()

	parser := hclparse.NewParser()
	filePath := filepath.Join("testdata", "nested.hcl")
	file, diags := parser.ParseHCLFile(filePath)
	if diags.HasErrors() {
		t.Fatalf("failed parsing hcl file: %v", diags)
	}

	_, diags = file.Body.Content(schema)
	if diags.HasErrors() {
		t.Fatalf("hcl validation diagnostics: %v", diags)
	}
}

func TestValidateFileWithSchema_Simple(t *testing.T) {
	schemaPath := filepath.Join("testdata", "simple.schema.hcl")
	hclPath := filepath.Join("testdata", "simple.hcl")

	diags := ValidateFileWithSchema(schemaPath, hclPath)
	if diags.HasErrors() {
		t.Fatalf("ValidateFileWithSchema returned errors: %v", diags)
	}
}

func TestValidateFileWithSchema_Nested(t *testing.T) {
	schemaPath := filepath.Join("testdata", "nested.schema.hcl")
	hclPath := filepath.Join("testdata", "nested.hcl")

	diags := ValidateFileWithSchema(schemaPath, hclPath)
	if diags.HasErrors() {
		t.Fatalf("ValidateFileWithSchema returned errors: %v", diags)
	}
}

func TestDetectAndValidate_SimpleLinked(t *testing.T) {
	hclPath := filepath.Join("testdata", "simple_linked.hcl")
	diags := ValidateHCLWithLinkedSchema(hclPath)
	if diags.HasErrors() {
		t.Fatalf("DetectAndValidate reported errors: %v", diags)
	}
}

func TestDetectAndValidate_NestedLinked(t *testing.T) {
	hclPath := filepath.Join("testdata", "nested_linked.hcl")
	diags := ValidateHCLWithLinkedSchema(hclPath)
	if diags.HasErrors() {
		t.Fatalf("DetectAndValidate reported errors: %v", diags)
	}
}

func TestDetectAndValidate_NestedLinked_WithExcessAttr(t *testing.T) {
	hclPath := filepath.Join("testdata", "nested_linked_with_excess_attr.hcl")
	diags := ValidateHCLWithLinkedSchema(hclPath)
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics for excess attribute, got none")
	}
}

func TestParseNestedSchema(t *testing.T) {
	path := filepath.Join("testdata", "nested.schema.hcl")
	res, diags := ParseSchemaFile(path)
	if diags.HasErrors() {
		t.Fatalf("diagnostics had errors: %v", diags)
	}
	if res == nil || res.BodySchema == nil {
		t.Fatalf("expected non-nil resulting body schema")
	}

	foundA := false
	for _, a := range res.BodySchema.Attributes {
		if a.Name == "a" {
			foundA = true
		}
	}
	if !foundA {
		t.Fatalf("attribute a not found")
	}

	var outer *BlockHeaderAndBodySchema
	for _, b := range res.BodySchema.Blocks {
		if b.Type == "outer" {
			outer = &b
		}
	}
	if outer == nil {
		t.Fatalf("outer block not found")
	}
	if outer.BodySchema == nil {
		t.Fatalf("outer block missing nested body schema")
	}

	foundInner := false
	for _, ib := range outer.BodySchema.Blocks {
		if ib.Type == "inner" {
			foundInner = true
			if len(ib.LabelNames) != 1 || ib.LabelNames[0] != "i" {
				t.Fatalf("unexpected inner label names: %v", ib.LabelNames)
			}
		}
	}
	if !foundInner {
		t.Fatalf("inner block not found inside outer")
	}
}

func TestDuplicateBlockTypesDifferentLevels(t *testing.T) {
	path := filepath.Join("testdata", "duplicate_levels.schema.hcl")
	res, diags := ParseSchemaFile(path)
	if diags.HasErrors() {
		t.Fatalf("expected no errors parsing nested duplicate block types: %v", diags)
	}
	if res == nil || res.BodySchema == nil {
		t.Fatalf("expected non-nil resulting body schema")
	}
}

func TestDuplicateBlockTypesSameLevel(t *testing.T) {
	path := filepath.Join("testdata", "duplicate_same_level.schema.hcl")
	res, diags := ParseSchemaFile(path)
	if diags.HasErrors() {
		t.Fatalf("expected no errors parsing schema with same-level duplicate block headers: %v", diags)
	}
	if res == nil || res.BodySchema == nil {
		t.Fatalf("expected non-nil resulting body schema")
	}
}

func TestDuplicateAttributeInInstance(t *testing.T) {
	hclPath := filepath.Join("testdata", "duplicate_attr.hcl")
	diags := ValidateHCLWithLinkedSchema(hclPath)
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics for duplicate attribute in HCL instance, got none")
	}
}

func TestLabelCountMismatch(t *testing.T) {
	hclPath := filepath.Join("testdata", "label_count_mismatch.hcl")
	diags := ValidateHCLWithLinkedSchema(hclPath)
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics for label count mismatch, got none")
	}
}

func TestMissingRequiredAttribute(t *testing.T) {
	hclPath := filepath.Join("testdata", "missing_required_attr.hcl")
	diags := ValidateHCLWithLinkedSchema(hclPath)
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics for missing required attribute, got none")
	}
}

func TestMultipleBlockInstancesAllowed(t *testing.T) {
	hclPath := filepath.Join("testdata", "multiple_tags.hcl")
	diags := ValidateHCLWithLinkedSchema(hclPath)
	if diags.HasErrors() {
		t.Fatalf("expected no diagnostics for multiple allowed block instances, got: %v", diags)
	}
}

func TestFetchRemoteSchemaHTTPS(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "schema", "draft", "2025-10", ".schema.hcl")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("failed to read local schema fixture: %v", err)
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(data)
	}))
	defer srv.Close()

	oldClient := httpClient
	httpClient = srv.Client()
	defer func() { httpClient = oldClient }()

	url := srv.URL + "/.schema.hcl"

	local, diags := fetchRemoteSchema(url)
	if diags.HasErrors() {
		t.Fatalf("fetchRemoteSchema returned diagnostics: %v", diags)
	}
	if local == "" {
		t.Fatalf("expected a local cached path, got empty string")
	}
	if _, err := os.Stat(local); err != nil {
		t.Fatalf("expected cached file to exist at %s: %v", local, err)
	}

	parser := hclparse.NewParser()
	_, pd := parser.ParseHCLFile(local)
	if pd.HasErrors() {
		t.Fatalf("failed to parse cached remote schema: %v", pd)
	}
}
