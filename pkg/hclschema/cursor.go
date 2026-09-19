package hclschema

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/zclconf/go-cty/cty"
)

// Cursor is what the schema knows about one position in an instance document.
// It is the basis for completion, hover and go-to-definition.
type Cursor struct {
	// Schema is the document's schema.
	Schema *Schema

	// Body is the declaration of the innermost body containing the position.
	// It is nil when the position is inside a block the schema does not
	// declare.
	Body *FullBodySchema

	// Block is the declaration of the innermost enclosing block, or nil at the
	// document root.
	Block *BlockSchema

	// Attribute is the declaration of the attribute under the position, if the
	// position is on one.
	Attribute *AttributeSchema

	// AttributeName is the name written at the position, whether or not it is
	// declared.
	AttributeName string

	// Path is the chain of block types from the root to Block.
	Path []string
}

// At resolves what the schema says about a position in a parsed file.
func (s *Schema) At(f *hcl.File, pos hcl.Pos) *Cursor {
	if s == nil || f == nil {
		return nil
	}
	c := &Cursor{Schema: s, Body: s.Body}

	for _, blk := range f.BlocksAtPos(pos) {
		decl := c.Body.Block(blk.Type, len(blk.Labels))
		c.Path = append(c.Path, blk.Type)
		if decl == nil {
			c.Block, c.Body = nil, nil
			break
		}
		c.Block = decl
		c.Body = decl.Body
	}

	if a := f.AttributeAtPos(pos); a != nil {
		c.AttributeName = a.Name
		c.Attribute = c.Body.Attribute(a.Name)
	}
	return c
}

// CompletionKind distinguishes the sort of thing a completion inserts.
type CompletionKind string

const (
	// CompletionAttribute is an attribute assignment.
	CompletionAttribute CompletionKind = "attribute"
	// CompletionBlock is a block.
	CompletionBlock CompletionKind = "block"
	// CompletionValue is one permitted value of an attribute.
	CompletionValue CompletionKind = "value"
)

// Completion is one suggestion at a position.
type Completion struct {
	Label         string
	Kind          CompletionKind
	Detail        string
	Documentation string
	InsertText    string
	Deprecated    bool
}

// Completions lists what may be written at the cursor. When the cursor is on
// an attribute with an enum, the permitted values are offered; otherwise the
// declarations available in the enclosing body are.
func (c *Cursor) Completions() []Completion {
	if c == nil || c.Body == nil {
		return nil
	}

	if c.Attribute != nil && len(c.Attribute.Enum) > 0 {
		out := make([]Completion, 0, len(c.Attribute.Enum))
		for _, v := range c.Attribute.Enum {
			out = append(out, Completion{
				Label:      formatValue(v),
				Kind:       CompletionValue,
				Detail:     c.Attribute.Name,
				InsertText: formatValue(v),
			})
		}
		return out
	}

	var out []Completion
	for i := range c.Body.Attributes {
		a := &c.Body.Attributes[i]
		out = append(out, Completion{
			Label:         a.Name,
			Kind:          CompletionAttribute,
			Detail:        attributeSignature(a),
			Documentation: a.Description,
			InsertText:    a.Name + " = ",
			Deprecated:    a.Deprecated != "",
		})
	}
	for i := range c.Body.Blocks {
		b := &c.Body.Blocks[i]
		out = append(out, Completion{
			Label:         b.Type,
			Kind:          CompletionBlock,
			Detail:        blockSignature(b),
			Documentation: b.Description,
			InsertText:    blockSnippet(b),
			Deprecated:    b.Deprecated != "",
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == CompletionAttribute
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// Doc renders hover documentation for the cursor as Markdown.
func (c *Cursor) Doc() string {
	if c == nil {
		return ""
	}
	var b strings.Builder
	switch {
	case c.Attribute != nil:
		a := c.Attribute
		fmt.Fprintf(&b, "```hcl\n%s\n```\n", attributeSignature(a))
		if a.Description != "" {
			fmt.Fprintf(&b, "\n%s\n", a.Description)
		}
		if a.Deprecated != "" {
			fmt.Fprintf(&b, "\n**Deprecated:** %s\n", a.Deprecated)
		}
		if len(a.Enum) > 0 {
			fmt.Fprintf(&b, "\nOne of: %s\n", formatEnum(a.Enum))
		}
		if a.Pattern != "" {
			fmt.Fprintf(&b, "\nMust match `%s`\n", a.Pattern)
		}
		if a.Min != nil || a.Max != nil {
			fmt.Fprintf(&b, "\nBounds: %s\n", boundsText(a))
		}
		if a.Default != cty.NilVal {
			fmt.Fprintf(&b, "\nDefaults to `%s`\n", formatValue(a.Default))
		}
		if a.Sensitive {
			b.WriteString("\nMarked sensitive; values are redacted in diagnostics.\n")
		}
	case c.Block != nil:
		blk := c.Block
		fmt.Fprintf(&b, "```hcl\n%s\n```\n", blockSignature(blk))
		if blk.Description != "" {
			fmt.Fprintf(&b, "\n%s\n", blk.Description)
		}
		if blk.Deprecated != "" {
			fmt.Fprintf(&b, "\n**Deprecated:** %s\n", blk.Deprecated)
		}
		if blk.MinItems > 0 || blk.MaxItems > 0 {
			fmt.Fprintf(&b, "\nOccurrences: %s\n", cardinalityText(blk))
		}
	default:
		if c.Schema != nil && c.Schema.ID != "" {
			fmt.Fprintf(&b, "Schema `%s` (draft %s)\n", c.Schema.ID, c.Schema.Draft)
		}
	}
	return b.String()
}

// DefinitionRange is where the declaration under the cursor is written in the
// schema document.
func (c *Cursor) DefinitionRange() (hcl.Range, bool) {
	switch {
	case c == nil:
		return hcl.Range{}, false
	case c.Attribute != nil:
		return c.Attribute.DeclRange, true
	case c.Block != nil:
		return c.Block.DeclRange, true
	}
	return hcl.Range{}, false
}

func attributeSignature(a *AttributeSchema) string {
	var b strings.Builder
	b.WriteString(a.Name)
	if a.Type != cty.NilType {
		fmt.Fprintf(&b, " : %s", typeexpr.TypeString(a.Type))
	}
	if a.Required {
		b.WriteString(" (required)")
	} else {
		b.WriteString(" (optional)")
	}
	return b.String()
}

func blockSignature(b *BlockSchema) string {
	var sb strings.Builder
	sb.WriteString(b.Type)
	for _, l := range b.LabelNames {
		fmt.Fprintf(&sb, " %q", "<"+l+">")
	}
	sb.WriteString(" { ... }")
	return sb.String()
}

func blockSnippet(b *BlockSchema) string {
	var sb strings.Builder
	sb.WriteString(b.Type)
	for range b.LabelNames {
		sb.WriteString(` ""`)
	}
	sb.WriteString(" {\n}\n")
	return sb.String()
}

func boundsText(a *AttributeSchema) string {
	switch {
	case a.Min != nil && a.Max != nil:
		return fmt.Sprintf("%s to %s", formatNum(*a.Min), formatNum(*a.Max))
	case a.Min != nil:
		return "at least " + formatNum(*a.Min)
	default:
		return "at most " + formatNum(*a.Max)
	}
}

func cardinalityText(b *BlockSchema) string {
	switch {
	case b.MinItems > 0 && b.MaxItems > 0:
		return fmt.Sprintf("%d to %d", b.MinItems, b.MaxItems)
	case b.MinItems > 0:
		return fmt.Sprintf("at least %d", b.MinItems)
	default:
		return fmt.Sprintf("at most %d", b.MaxItems)
	}
}
