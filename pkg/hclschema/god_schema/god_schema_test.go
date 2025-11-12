package godschema_test

import (
	"os"
	"path/filepath"
	"testing"

	godschema "github.com/avestura/hcl-schema/pkg/hclschema/god_schema"
	"github.com/hashicorp/hcl/v2/hclparse"
)

func TestValidateGodSchema(t *testing.T) {
	path := filepath.Join("../../..", "schema", "draft", "2025-10", ".schema.hcl")

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read schema file: %v", err)
	}

	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(content, path)
	if diags.HasErrors() {
		t.Fatalf("failed to parse HCL: %v", diags)
	}

	diags = godschema.ValidateSchema(file)
	if diags.HasErrors() {
		t.Errorf("schema validation failed:\n%s", diags.Error())
	}
}
