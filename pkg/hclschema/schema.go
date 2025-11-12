package hclschema

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
)

const SchemaExtension = ".schema.hcl"

type BlockHeaderAndBodySchema struct {
	hcl.BlockHeaderSchema

	BodySchema *FullBodySchema
}

type FullBodySchema struct {
	Attributes []hcl.AttributeSchema
	Blocks     []BlockHeaderAndBodySchema
}

func (fbs *FullBodySchema) AsBodySchema() *hcl.BodySchema {
	originalHclBlocks := make([]hcl.BlockHeaderSchema, 0, len(fbs.Blocks))
	for i, blk := range fbs.Blocks {
		originalHclBlocks[i] = blk.BlockHeaderSchema
	}
	return &hcl.BodySchema{
		Attributes: fbs.Attributes,
		Blocks:     originalHclBlocks,
	}
}

func ParseSchema(filename string) error {
	parser := hclparse.NewParser()
	_, diag := parser.ParseHCLFile(filename)
	if diag.HasErrors() {
		return diag
	}
	return nil
}
