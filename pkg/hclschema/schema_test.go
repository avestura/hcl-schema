package hclschema

import (
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
