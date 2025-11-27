package godschema

import (
	"github.com/hashicorp/hcl/v2"
)

func ValidateBody(body hcl.Body, schema *hcl.BodySchema, ctx *hcl.EvalContext) hcl.Diagnostics {
	content, diags := body.Content(schema)
	if diags.HasErrors() {
		return diags
	}

	bodySchema := GetBodySchema()
	attrSchema := GetAttributeSchema()
	blockHeaderSchema := GetBlockHeaderSchema()

	for _, block := range content.Blocks {
		switch block.Type {

		case "body":
			diags = append(diags, ValidateBody(block.Body, bodySchema, ctx)...)

		case "attribute":
			diags = append(diags, ValidateBody(block.Body, attrSchema, ctx)...)

		case "block_header":
			diags = append(diags, ValidateBody(block.Body, blockHeaderSchema, ctx)...)

			content, diag := block.Body.Content(blockHeaderSchema)
			diags = append(diags, diag...)
			for _, inner := range content.Blocks {
				if inner.Type == "body" {
					diags = append(diags, ValidateBody(inner.Body, bodySchema, ctx)...)
				}
			}
		}
	}

	return diags
}

func ValidateSchema(file *hcl.File) hcl.Diagnostics {
	rootSchema := GetRootSchema()
	ctx := &hcl.EvalContext{}

	diags := ValidateBody(file.Body, rootSchema, ctx)
	return diags
}
