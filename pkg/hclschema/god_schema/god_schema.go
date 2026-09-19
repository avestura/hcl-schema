// Package godschema describes the hcl-schema meta-schema: the shape of a
// `*.schema.hcl` document itself.
//
// Every key added after draft 2025-10 is declared optional, so a document
// written against the original draft still validates unchanged. Drafts differ
// in how strictly declarations are interpreted, not in which keys they accept.
package godschema

import "github.com/hashicorp/hcl/v2"

// GetRootSchema returns the shape of a schema document's top level.
func GetRootSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "__schema", Required: true},
			{Name: "__id", Required: true},
			{Name: "__schema_sha256", Required: false},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "body"},
			{Type: "import", LabelNames: []string{"alias"}},
		},
	}
}

// GetImportSchema returns the shape of an `import` block.
func GetImportSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "source", Required: true},
			{Name: "sha256", Required: false},
		},
	}
}

// GetBodySchema returns the shape of a `body` block.
func GetBodySchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "open", Required: false},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "attribute", LabelNames: []string{"attribute_name"}},
			{Type: "block_header", LabelNames: []string{"block_header_type"}},
			{Type: "one_of"},
		},
	}
}

// GetBlockHeaderSchema returns the shape of a `block_header` block.
func GetBlockHeaderSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "label_names", Required: false},
			{Name: "ref", Required: false},
			{Name: "id", Required: false},
			{Name: "description", Required: false},
			{Name: "deprecated", Required: false},
			{Name: "required", Required: false},
			{Name: "min_items", Required: false},
			{Name: "max_items", Required: false},
			{Name: "unique_labels", Required: false},
			{Name: "open", Required: false},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "body"},
			{Type: "label", LabelNames: []string{"label_name"}},
		},
	}
}

// GetLabelSchema returns the shape of a `label` block.
func GetLabelSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "description", Required: false},
			{Name: "pattern", Required: false},
			{Name: "enum", Required: false},
		},
	}
}

// GetAttributeSchema returns the shape of an `attribute` block.
func GetAttributeSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "required", Required: false},
			{Name: "type", Required: false},
			{Name: "default", Required: false},
			{Name: "description", Required: false},
			{Name: "deprecated", Required: false},
			{Name: "sensitive", Required: false},
			{Name: "enum", Required: false},
			{Name: "pattern", Required: false},
			{Name: "min", Required: false},
			{Name: "max", Required: false},
			{Name: "conflicts_with", Required: false},
			{Name: "required_with", Required: false},
			{Name: "exactly_one_of", Required: false},
			{Name: "at_least_one_of", Required: false},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "validation"},
		},
	}
}

// GetValidationSchema returns the shape of a `validation` block.
func GetValidationSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "condition", Required: true},
			{Name: "error_message", Required: true},
		},
	}
}

// GetOneOfSchema returns the shape of a `one_of` block.
func GetOneOfSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "variant", LabelNames: []string{"variant_name"}},
		},
	}
}

// GetVariantSchema returns the shape of a `variant` block.
func GetVariantSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "body"},
		},
	}
}
