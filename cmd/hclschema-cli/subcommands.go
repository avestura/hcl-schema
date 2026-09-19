package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/avestura/hcl-schema/pkg/hclschema"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// hasComments reports whether the source carries any comment token. Lexing is
// the only way to tell: comments never reach the parse tree.
func hasComments(src []byte, filename string) bool {
	tokens, diags := hclsyntax.LexConfig(src, filename, hcl.InitialPos)
	if diags.HasErrors() {
		// An unparseable file is left alone rather than rewritten.
		return true
	}
	for _, tok := range tokens {
		if tok.Type == hclsyntax.TokenComment {
			return true
		}
	}
	return false
}

// runFmt rewrites schema documents in canonical form by parsing and
// re-rendering them, which also normalises key order and indentation.
func runFmt(args []string) int {
	fset := flag.NewFlagSet("fmt", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: hclschema fmt [flags] [files or directories...]

Rewrites *.schema.hcl documents in canonical form.

Flags:
`)
		fset.PrintDefaults()
	}
	var (
		common commonFlags
		write  bool
		check  bool
		force  bool
	)
	common.register(fset)
	fset.BoolVar(&write, "w", false, "Write the result back to the file instead of standard output")
	fset.BoolVar(&check, "check", false, "Exit 1 if any file is not already formatted; write nothing")
	fset.BoolVar(&force, "force", false, "Format a file even though its comments would be lost")
	if err := fset.Parse(args); err != nil {
		return exitUsage
	}

	targets, err := expandTargets(fset.Args(), true)
	if err != nil {
		return fail("%s", err)
	}
	schemas := make([]string, 0, len(targets))
	for _, t := range targets {
		if strings.HasSuffix(t, hclschema.SchemaExtension) {
			schemas = append(schemas, t)
		}
	}
	if len(schemas) == 0 {
		return fail("no %s files found", hclschema.SchemaExtension)
	}

	opts := hclschema.ParseOptions{Loader: common.loader(), Strict: common.strict}
	changed := false
	for _, path := range schemas {
		original, err := os.ReadFile(path)
		if err != nil {
			return fail("reading %s: %s", path, err)
		}

		// Formatting works by parsing and re-rendering, and the parse tree
		// holds no comments. Rewriting a commented file would delete the
		// comments, so it is skipped unless the author insists.
		if !force && hasComments(original, path) {
			fmt.Fprintf(os.Stderr,
				"hclschema: skipping %s because formatting would discard its comments (use --force to format anyway)\n",
				path)
			continue
		}

		schema, diags := hclschema.ParseSchemaOpts(original, path, opts)
		if diags.HasErrors() {
			printDiags(path, diags)
			return exitFindings
		}
		formatted := schema.Bytes()
		if string(formatted) == string(original) {
			continue
		}
		changed = true
		switch {
		case check:
			fmt.Printf("%s\n", path)
		case write:
			if err := os.WriteFile(path, formatted, 0o644); err != nil {
				return fail("writing %s: %s", path, err)
			}
		default:
			os.Stdout.Write(formatted)
		}
	}
	if check && changed {
		return exitFindings
	}
	return exitOK
}

func runInfer(args []string) int {
	fset := flag.NewFlagSet("infer", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: hclschema infer [flags] <files or directories...>

Derives a starting schema from existing HCL documents and prints it.

The result describes what the samples happen to contain, so read it before
committing it.

Flags:
`)
		fset.PrintDefaults()
	}
	var (
		id       string
		out      string
		required bool
	)
	fset.StringVar(&id, "id", "", "Value for the generated __id")
	fset.StringVar(&out, "o", "", "Write to this file instead of standard output")
	fset.BoolVar(&required, "required-when-ubiquitous", false,
		"Mark an attribute required when every sample carries it")
	if err := fset.Parse(args); err != nil {
		return exitUsage
	}
	if fset.NArg() == 0 {
		fset.Usage()
		return exitUsage
	}

	targets, err := expandTargets(fset.Args(), true)
	if err != nil {
		return fail("%s", err)
	}
	samples := make([]string, 0, len(targets))
	for _, t := range targets {
		if !strings.HasSuffix(t, hclschema.SchemaExtension) {
			samples = append(samples, t)
		}
	}
	if len(samples) == 0 {
		return fail("no instance documents found (a *.schema.hcl is not a sample)")
	}

	schema, diags := hclschema.InferFromPaths(samples, hclschema.InferOptions{
		ID:                id,
		RequireUbiquitous: required,
	})
	if diags.HasErrors() {
		printDiags("", diags)
		return exitFindings
	}
	printDiags("", diags)

	data := schema.Bytes()
	if out == "" {
		os.Stdout.Write(data)
		return exitOK
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return fail("writing %s: %s", out, err)
	}
	return exitOK
}

func runDocs(args []string) int {
	fset := flag.NewFlagSet("docs", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: hclschema docs [flags] <schema.hcl>

Renders Markdown reference documentation from a schema's descriptions.

Flags:
`)
		fset.PrintDefaults()
	}
	var (
		common  commonFlags
		title   string
		out     string
		offset  int
		noFront bool
	)
	common.register(fset)
	fset.StringVar(&title, "title", "", "Document title (default: the schema's __id)")
	fset.StringVar(&out, "o", "", "Write to this file instead of standard output")
	fset.IntVar(&offset, "heading-offset", 0, "Shift every heading down by this many levels")
	fset.BoolVar(&noFront, "no-frontmatter", false, "Omit the Docusaurus front matter")
	if err := fset.Parse(args); err != nil {
		return exitUsage
	}
	if fset.NArg() != 1 {
		fset.Usage()
		return exitUsage
	}

	path := fset.Arg(0)
	schema, diags := hclschema.ParseSchemaFileOpts(path, hclschema.ParseOptions{
		Loader: common.loader(),
		Strict: common.strict,
	})
	if diags.HasErrors() {
		printDiags(path, diags)
		return exitFindings
	}

	var buf []byte
	if !noFront {
		name := title
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(path), hclschema.SchemaExtension)
		}
		buf = append(buf, []byte(fmt.Sprintf("---\ntitle: %s\n---\n\n", name))...)
	}
	buf = append(buf, schema.Markdown(hclschema.MarkdownOptions{
		Title:         title,
		HeadingOffset: offset,
	})...)

	if out == "" {
		os.Stdout.Write(buf)
		return exitOK
	}
	if err := os.WriteFile(out, buf, 0o644); err != nil {
		return fail("writing %s: %s", out, err)
	}
	return exitOK
}

func runBundle(args []string) int {
	fset := flag.NewFlagSet("bundle", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: hclschema bundle [flags] <schema.hcl>

Inlines a schema's imports and prints one self-contained document, suitable
for vendoring so that validation needs no network access.

Flags:
`)
		fset.PrintDefaults()
	}
	var (
		common commonFlags
		out    string
	)
	common.register(fset)
	fset.StringVar(&out, "o", "", "Write to this file instead of standard output")
	if err := fset.Parse(args); err != nil {
		return exitUsage
	}
	if fset.NArg() != 1 {
		fset.Usage()
		return exitUsage
	}

	data, diags := hclschema.BundleFile(fset.Arg(0), hclschema.ParseOptions{
		Loader: common.loader(),
		Strict: common.strict,
	})
	if diags.HasErrors() {
		printDiags(fset.Arg(0), diags)
		return exitFindings
	}
	if out == "" {
		os.Stdout.Write(data)
		return exitOK
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return fail("writing %s: %s", out, err)
	}
	return exitOK
}

// runLegacy preserves the pre-subcommand contract: JSON on stdout and exit 0
// even when the document has errors. The editor extension that shipped against
// it treats a non-zero exit as a crash, so changing the code here would break
// installed copies.
func runLegacy(args []string) int {
	fset := flag.NewFlagSet("hclschema", flag.ContinueOnError)
	fset.SetOutput(os.Stderr)
	detect := fset.Bool("detect", true, "Detect the schema via the __schema attribute")
	if err := fset.Parse(args); err != nil {
		return exitUsage
	}
	rest := fset.Args()
	if len(rest) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return exitUsage
	}

	hclPath := rest[0]
	opts := hclschema.LoadOptions{}

	var diags hcl.Diagnostics
	if *detect {
		diags = hclschema.ValidateFileLinked(hclPath, opts)
	} else {
		if len(rest) < 2 {
			fmt.Fprintln(os.Stderr, "usage: hclschema --detect=false <hcl-file> <schema-file>")
			return exitUsage
		}
		diags = hclschema.ValidateWithSchema(rest[1], hclPath, opts)
	}

	rep := &jsonReporter{w: os.Stdout}
	_ = rep.Report(hclPath, diags, nil)
	if err := rep.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "failed to emit json:", err)
		return exitUsage
	}
	return exitOK
}

func printDiags(filename string, diags hcl.Diagnostics) {
	if len(diags) == 0 {
		return
	}
	wr := hcl.NewDiagnosticTextWriter(os.Stderr, nil, 80, isTerminal(os.Stderr))
	for _, d := range diags {
		_ = wr.WriteDiagnostic(d)
	}
	_ = filename
}
