package godschema

import "github.com/hashicorp/hcl/v2"

func GetRootSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "__schema", Required: true},
			{Name: "__id", Required: true},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "body"},
		},
	}
}

func GetBodySchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "attribute", LabelNames: []string{"attribute_name"}},
			{Type: "block_header", LabelNames: []string{"block_header_type"}},
		},
	}
}

func GetBlockHeaderSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "label_names", Required: false},
			{Name: "ref", Required: false},
			{Name: "id", Required: false},
		},
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "body"},
		},
	}
}

func GetAttributeSchema() *hcl.BodySchema {
	return &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "required", Required: false},
		},
	}
}
