package hclschema

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/zclconf/go-cty/cty"
)

// MarkdownOptions controls generated reference documentation.
type MarkdownOptions struct {
	// Title heads the document. Defaults to the schema's `__id`.
	Title string

	// HeadingOffset shifts every heading down, for embedding the output in a
	// larger page.
	HeadingOffset int
}

// Markdown renders reference documentation for the schema.
//
// Descriptions are the point of the exercise: a `description` written once in
// the schema becomes hover text in the editor and a row in this table, so the
// two cannot drift apart.
func (s *Schema) Markdown(opts MarkdownOptions) []byte {
	var b strings.Builder
	title := opts.Title
	if title == "" {
		title = s.ID
	}
	if title == "" {
		title = "Schema reference"
	}

	h := func(level int, text string) {
		fmt.Fprintf(&b, "\n%s %s\n\n", strings.Repeat("#", level+opts.HeadingOffset), text)
	}

	fmt.Fprintf(&b, "%s %s\n", strings.Repeat("#", 1+opts.HeadingOffset), title)
	fmt.Fprintf(&b, "\nDraft `%s`.\n", s.Draft)

	w := &docWriter{builder: &b, heading: h, seen: map[*FullBodySchema]string{}}
	w.body(s.Body, "", 2)
	w.drain()
	return []byte(b.String())
}

type docWriter struct {
	builder *strings.Builder
	heading func(level int, text string)

	// seen records bodies already documented, so a schema that shares or
	// recurses through a body links back instead of looping.
	seen map[*FullBodySchema]string

	queue []docJob
}

type docJob struct {
	body  *FullBodySchema
	path  string
	level int
}

func (w *docWriter) drain() {
	for len(w.queue) > 0 {
		job := w.queue[0]
		w.queue = w.queue[1:]
		w.body(job.body, job.path, job.level)
	}
}

func (w *docWriter) body(fbs *FullBodySchema, path string, level int) {
	if fbs == nil {
		return
	}
	if prev, ok := w.seen[fbs]; ok {
		fmt.Fprintf(w.builder, "\nSame contents as [`%s`](#%s).\n", prev, anchor(prev))
		return
	}
	w.seen[fbs] = pathLabel(path)

	if fbs.Open {
		w.builder.WriteString("\nUndeclared attributes and blocks are permitted here.\n")
	}

	if len(fbs.Attributes) > 0 {
		w.builder.WriteString("\n| Attribute | Type | Required | Default | Description |\n")
		w.builder.WriteString("| --- | --- | --- | --- | --- |\n")
		attrs := make([]*AttributeSchema, 0, len(fbs.Attributes))
		for i := range fbs.Attributes {
			attrs = append(attrs, &fbs.Attributes[i])
		}
		sort.Slice(attrs, func(i, j int) bool {
			if attrs[i].Required != attrs[j].Required {
				return attrs[i].Required
			}
			return attrs[i].Name < attrs[j].Name
		})
		for _, a := range attrs {
			fmt.Fprintf(w.builder, "| `%s` | %s | %s | %s | %s |\n",
				a.Name, typeCell(a), yesNo(a.Required), defaultCell(a), descriptionCell(a))
		}
	}

	for i := range fbs.Blocks {
		blk := &fbs.Blocks[i]
		childPath := joinPath(path, blk.Type)
		w.heading(level, "`"+childPath+"`")

		if blk.Description != "" {
			fmt.Fprintf(w.builder, "%s\n", escapeProse(blk.Description))
		}
		if blk.Deprecated != "" {
			fmt.Fprintf(w.builder, "\n> **Deprecated.** %s\n", escapeProse(blk.Deprecated))
		}
		fmt.Fprintf(w.builder, "\n```hcl\n%s\n```\n", blockSignature(blk))
		if blk.MinItems > 0 || blk.MaxItems > 0 {
			fmt.Fprintf(w.builder, "\nOccurrences: %s.\n", cardinalityText(blk))
		}
		if blk.UniqueLabels {
			w.builder.WriteString("\nLabels must be unique among sibling blocks of this type.\n")
		}
		for _, l := range blk.Labels {
			if l.Description == "" && l.Pattern == "" && len(l.Enum) == 0 {
				continue
			}
			fmt.Fprintf(w.builder, "\n- Label `%s`%s%s%s\n", l.Name,
				optional(": ", escapeProse(l.Description)),
				optional(" Must match `", l.Pattern, "`."),
				enumNote(l.Enum))
		}
		w.body(blk.Body, childPath, level+1)
	}

	for i := range fbs.Variants {
		v := &fbs.Variants[i]
		childPath := joinPath(path, "one_of "+v.Name)
		w.heading(level, "`"+childPath+"`")
		w.builder.WriteString("\nExactly one variant of this group must apply.\n")
		w.body(v.Body, childPath, level+1)
	}
}

func typeCell(a *AttributeSchema) string {
	var parts []string
	if a.Type != cty.NilType {
		parts = append(parts, "`"+typeexpr.TypeString(a.Type)+"`")
	} else {
		parts = append(parts, "any")
	}
	if len(a.Enum) > 0 {
		parts = append(parts, "one of "+formatEnum(a.Enum))
	}
	if a.Pattern != "" {
		parts = append(parts, "matching `"+a.Pattern+"`")
	}
	if a.Min != nil || a.Max != nil {
		parts = append(parts, boundsText(a))
	}
	// Self-closing, because MDX parses this as JSX and would otherwise look
	// for a closing tag. Plain Markdown renders it the same way.
	return strings.Join(parts, "<br />")
}

func defaultCell(a *AttributeSchema) string {
	if a.Default == cty.NilVal {
		return ""
	}
	return "`" + formatValue(a.Default) + "`"
}

func descriptionCell(a *AttributeSchema) string {
	var parts []string
	if a.Description != "" {
		parts = append(parts, escapeProse(a.Description))
	}
	if a.Deprecated != "" {
		parts = append(parts, "**Deprecated.** "+escapeProse(a.Deprecated))
	}
	if a.Sensitive {
		parts = append(parts, "Sensitive.")
	}
	for _, g := range []struct {
		label string
		names []string
	}{
		{"Conflicts with", a.ConflictsWith},
		{"Requires", a.RequiredWith},
		{"Exactly one of", a.ExactlyOneOf},
		{"At least one of", a.AtLeastOneOf},
	} {
		if len(g.names) > 0 {
			parts = append(parts, g.label+" "+quoteList(g.names)+".")
		}
	}
	for _, v := range a.Validations {
		if v.ErrorMessage != "" {
			parts = append(parts, escapeProse(v.ErrorMessage))
		}
	}
	return strings.ReplaceAll(strings.Join(parts, " "), "|", "\\|")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func pathLabel(path string) string {
	if path == "" {
		return "root"
	}
	return path
}

func anchor(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == ' ', r == '.', r == '_':
			b.WriteRune('-')
		}
	}
	return b.String()
}

func optional(prefix, value string, suffix ...string) string {
	if value == "" {
		return ""
	}
	return prefix + value + strings.Join(suffix, "")
}

func enumNote(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return " One of " + quoteList(items) + "."
}

// escapeProse makes schema-authored text safe to drop into a Markdown document
// that may be processed as MDX, where a bare `<` starts a JSX tag and a bare
// `{` starts an expression. Both entities render as the original character in
// plain Markdown too, so the output stays portable.
var proseEscaper = strings.NewReplacer(
	"<", "&lt;",
	">", "&gt;",
	"{", "&#123;",
	"}", "&#125;",
)

func escapeProse(s string) string { return proseEscaper.Replace(s) }
