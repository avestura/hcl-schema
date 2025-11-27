package hclschema

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	godschema "github.com/avestura/hcl-schema/pkg/hclschema/god_schema"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

var httpClient = http.DefaultClient

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
	for _, blk := range fbs.Blocks {
		originalHclBlocks = append(originalHclBlocks, blk.BlockHeaderSchema)
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

func GetInnerDefaultBlocks() []hcl.BlockHeaderSchema {
	return []hcl.BlockHeaderSchema{
		{Type: "attribute", LabelNames: []string{"attribute_name"}},
		{Type: "block_header", LabelNames: []string{"block_header_type"}},
		{Type: "body"},
	}
}

func ParseSchemaFile(filename string) (*BlockHeaderAndBodySchema, hcl.Diagnostics) {
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCLFile(filename)
	if diags.HasErrors() {
		return nil, diags
	}
	defaultSchema := godschema.GetRootSchema()

	fbs, d := parseBody(file.Body, defaultSchema)
	diags = append(diags, d...)
	if diags.HasErrors() {
		return nil, diags
	}

	return &BlockHeaderAndBodySchema{BodySchema: fbs}, diags
}

func parseBody(body hcl.Body, schema *hcl.BodySchema) (*FullBodySchema, hcl.Diagnostics) {
	content, diags := body.Content(schema)
	if diags.HasErrors() {
		return nil, diags
	}

	fbs := &FullBodySchema{}
	attrs := make([]hcl.AttributeSchema, 0)
	blocks := make([]BlockHeaderAndBodySchema, 0)

	ctx := &hcl.EvalContext{}

	innerDefault := godschema.GetBodySchema()

	for _, block := range content.Blocks {
		switch block.Type {
		case "attribute":
			name := ""
			if len(block.Labels) > 0 {
				name = block.Labels[0]
			}

			innerSchema := &hcl.BodySchema{Attributes: []hcl.AttributeSchema{{Name: "required"}}}
			innerContent, d := block.Body.Content(innerSchema)
			diags = append(diags, d...)

			required := false
			if a, ok := innerContent.Attributes["required"]; ok {
				val, err := a.Expr.Value(ctx)
				if err != nil {
					diags = append(diags, &hcl.Diagnostic{Severity: hcl.DiagError, Summary: "failed to evaluate attribute 'required'", Detail: err.Error()})
				} else if val.Type() == cty.Bool {
					required = val.True()
				}
			}
			attrs = append(attrs, hcl.AttributeSchema{Name: name, Required: required})

		case "block_header":
			typ := ""
			if len(block.Labels) > 0 {
				typ = block.Labels[0]
			}

			innerSchema := godschema.GetBlockHeaderSchema()
			innerContent, d := block.Body.Content(innerSchema)
			diags = append(diags, d...)

			labelNames := make([]string, 0)
			if a, ok := innerContent.Attributes["label_names"]; ok {
				val, err := a.Expr.Value(ctx)
				if err != nil {
					diags = append(diags, &hcl.Diagnostic{Severity: hcl.DiagError, Summary: "failed to evaluate 'label_names'", Detail: err.Error()})
				} else if val.CanIterateElements() {
					it := val.ElementIterator()
					for it.Next() {
						_, v := it.Element()
						if v.Type() == cty.String {
							labelNames = append(labelNames, v.AsString())
						}
					}
				}
			}

			var nested *FullBodySchema
			for _, inner := range innerContent.Blocks {
				if inner.Type == "body" {
					nb, d := parseBody(inner.Body, innerDefault)
					diags = append(diags, d...)
					nested = nb
				}
			}

			bhs := hcl.BlockHeaderSchema{Type: typ, LabelNames: labelNames}
			blocks = append(blocks, BlockHeaderAndBodySchema{BlockHeaderSchema: bhs, BodySchema: nested})

		case "body":
			nb, d := parseBody(block.Body, innerDefault)
			diags = append(diags, d...)
			if nb != nil {
				attrs = append(attrs, nb.Attributes...)
				blocks = append(blocks, nb.Blocks...)
			}
		default:

		}
	}

	fbs.Attributes = attrs
	fbs.Blocks = blocks
	return fbs, diags
}

func ValidateFileWithSchema(schemaPath, hclPath string) hcl.Diagnostics {
	var allDiags hcl.Diagnostics

	schemaRes, diags := ParseSchemaFile(schemaPath)
	allDiags = append(allDiags, diags...)
	if schemaRes == nil || schemaRes.BodySchema == nil {
		return allDiags
	}

	parser := hclparse.NewParser()
	file, d := parser.ParseHCLFile(hclPath)
	allDiags = append(allDiags, d...)
	if d.HasErrors() || file == nil {
		return allDiags
	}

	findBlockDef := func(fbs *FullBodySchema, blk *hcl.Block) *BlockHeaderAndBodySchema {
		if fbs == nil {
			return nil
		}

		for i := range fbs.Blocks {
			cand := &fbs.Blocks[i]
			if cand.Type != blk.Type {
				continue
			}
			if len(cand.LabelNames) == len(blk.Labels) {
				return cand
			}
		}
		return nil
	}

	var validate func(b hcl.Body, fbs *FullBodySchema, allowSchemaAttr bool) hcl.Diagnostics
	validate = func(b hcl.Body, fbs *FullBodySchema, allowSchemaAttr bool) hcl.Diagnostics {
		var res hcl.Diagnostics
		var bs *hcl.BodySchema
		if fbs == nil {
			bs = &hcl.BodySchema{}
		} else {
			bs = fbs.AsBodySchema()
		}
		if allowSchemaAttr {
			bs.Attributes = append(bs.Attributes, hcl.AttributeSchema{Name: "__schema"})
		}

		content, d := b.Content(bs)
		res = append(res, d...)

		for _, blk := range content.Blocks {
			def := findBlockDef(fbs, blk)
			if def != nil && def.BodySchema != nil {
				res = append(res, validate(blk.Body, def.BodySchema, false)...)
			}
		}
		return res
	}

	d = validate(file.Body, schemaRes.BodySchema, true)
	allDiags = append(allDiags, d...)
	return allDiags
}

// ValidateHCLWithLinkedSchema reads `hclPath`, looks for a linking attribute named
// `__schema` (string), resolves it relative to `hclPath` when necessary, then
// validates the HCL file against the referenced schema. Returns diagnostics
// from parsing or validation.
func ValidateHCLWithLinkedSchema(hclPath string) hcl.Diagnostics {
	var allDiags hcl.Diagnostics

	parser := hclparse.NewParser()
	file, diags := parser.ParseHCLFile(hclPath)
	allDiags = append(allDiags, diags...)
	if file == nil || diags.HasErrors() {
		return allDiags
	}

	data := file.Bytes
	if len(data) == 0 {
		b, rerr := os.ReadFile(hclPath)
		if rerr != nil {
			allDiags = append(allDiags, &hcl.Diagnostic{Severity: hcl.DiagError, Summary: "failed to read file for schema detection", Detail: rerr.Error()})
			return allDiags
		}
		data = b
	}

	schemaRef, found := extractSchemaRefFromText(string(data))
	if !found {
		return allDiags
	}

	if strings.HasPrefix(schemaRef, "http://") || strings.HasPrefix(schemaRef, "https://") {
		local, d := fetchRemoteSchema(schemaRef)
		allDiags = append(allDiags, d...)
		if d.HasErrors() || local == "" {
			return allDiags
		}
		schemaRef = local
	} else if !filepath.IsAbs(schemaRef) {
		schemaRef = filepath.Join(filepath.Dir(hclPath), schemaRef)
	}

	resDiags := ValidateFileWithSchema(schemaRef, hclPath)
	allDiags = append(allDiags, resDiags...)
	return allDiags
}

func fetchRemoteSchema(url string) (string, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	if !strings.HasPrefix(url, "https://") {
		diags = append(diags, &hcl.Diagnostic{Severity: hcl.DiagError, Summary: "insecure schema URL", Detail: "only https:// URLs are allowed for remote schemas"})
		return "", diags
	}

	cacheDir := filepath.Join(os.TempDir(), "hclschema-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		diags = append(diags, &hcl.Diagnostic{Severity: hcl.DiagError, Summary: "failed to create cache dir", Detail: err.Error()})
		return "", diags
	}

	h := sha256.Sum256([]byte(url))
	fname := hex.EncodeToString(h[:]) + ".schema.hcl"
	full := filepath.Join(cacheDir, fname)

	if fi, err := os.Stat(full); err == nil {
		if time.Since(fi.ModTime()) < 24*time.Hour {
			return full, diags
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		diags = append(diags, &hcl.Diagnostic{Severity: hcl.DiagError, Summary: "failed to create request", Detail: err.Error()})
		return "", diags
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		diags = append(diags, &hcl.Diagnostic{Severity: hcl.DiagError, Summary: "failed to download schema", Detail: err.Error()})
		return "", diags
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		diags = append(diags, &hcl.Diagnostic{Severity: hcl.DiagError, Summary: "failed to download schema", Detail: resp.Status})
		return "", diags
	}

	const maxSize = 1 << 20
	r := io.LimitReader(resp.Body, maxSize+1)
	f, err := os.CreateTemp(cacheDir, "tmp-*.schema.hcl")
	if err != nil {
		diags = append(diags, &hcl.Diagnostic{Severity: hcl.DiagError, Summary: "failed to create temp file", Detail: err.Error()})
		return "", diags
	}
	defer func() {
		f.Close()
	}()
	n, err := io.Copy(f, r)
	if err != nil {
		diags = append(diags, &hcl.Diagnostic{Severity: hcl.DiagError, Summary: "failed to read schema body", Detail: err.Error()})
		os.Remove(f.Name())
		return "", diags
	}
	if n > maxSize {
		diags = append(diags, &hcl.Diagnostic{Severity: hcl.DiagError, Summary: "schema too large", Detail: "remote schema exceeds maximum allowed size"})
		os.Remove(f.Name())
		return "", diags
	}

	if err := os.Rename(f.Name(), full); err != nil {
		if err2 := copyFile(f.Name(), full); err2 != nil {
			diags = append(diags, &hcl.Diagnostic{Severity: hcl.DiagError, Summary: "failed to cache schema", Detail: err2.Error()})
			os.Remove(f.Name())
			return "", diags
		}
		os.Remove(f.Name())
	}

	return full, diags
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func extractSchemaRefFromText(s string) (string, bool) {
	idx := strings.Index(s, "__schema")
	if idx == -1 {
		return "", false
	}

	eq := strings.Index(s[idx:], "=")
	if eq == -1 {
		return "", false
	}
	rest := s[idx+eq+1:]
	rest = strings.TrimLeft(rest, " \t\n\r")
	if rest == "" {
		return "", false
	}
	if rest[0] != '"' && rest[0] != '\'' {
		return "", false
	}
	quote := rest[0]

	for i := 1; i < len(rest); i++ {
		if rest[i] == quote {
			return rest[1:i], true
		}
	}
	return "", false
}
