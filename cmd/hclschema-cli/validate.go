package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/avestura/hcl-schema/pkg/hclschema"
	"github.com/hashicorp/hcl/v2"
)

type commonFlags struct {
	strict   bool
	offline  bool
	cacheDir string
	timeout  time.Duration
	noNet    bool
}

func (c *commonFlags) register(fs *flag.FlagSet) {
	fs.BoolVar(&c.strict, "strict", false, "Apply draft 2026-09 rules regardless of the draft a schema pins")
	fs.BoolVar(&c.offline, "offline", false, "Never reach the network; serve remote schemas from the cache only")
	fs.StringVar(&c.cacheDir, "cache-dir", "", "Directory for cached remote schemas (default: the per-user cache dir)")
	fs.DurationVar(&c.timeout, "timeout", hclschema.DefaultFetchTimeout, "Timeout for a single remote schema fetch")
}

func (c *commonFlags) loader() *hclschema.Loader {
	l := &hclschema.Loader{
		Offline:                c.offline,
		Timeout:                c.timeout,
		AllowCrossHostRedirect: true,
	}
	if c.cacheDir != "" {
		l.Cache = &hclschema.DiskCache{Dir: c.cacheDir}
	}
	return l
}

func (c *commonFlags) loadOptions() hclschema.LoadOptions {
	return hclschema.LoadOptions{
		ParseOptions: hclschema.ParseOptions{
			Loader: c.loader(),
			Strict: c.strict,
		},
		ValidateOptions: hclschema.ValidateOptions{Strict: c.strict},
	}
}

func runValidate(args []string) int {
	fset := flag.NewFlagSet("validate", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: hclschema validate [flags] [files or directories...]

Checks each HCL file against the schema named by its __schema attribute, or
against the schema given by --schema. A *.schema.hcl file is checked against
the meta-schema instead.

With no files, the current directory is walked.

Exits 1 when any error diagnostic is reported, 2 on a usage or I/O problem.

Flags:
`)
		fset.PrintDefaults()
	}

	var (
		common        commonFlags
		schemaPath    string
		format        string
		stdinFilename string
		exitZero      bool
		noColor       bool
		quiet         bool
		warnAsError   bool
		recursive     bool
	)
	common.register(fset)
	fset.StringVar(&schemaPath, "schema", "", "Validate against this schema instead of each file's __schema")
	fset.StringVar(&format, "format", "text", "Output format: text, json, github or sarif")
	fset.StringVar(&stdinFilename, "stdin-filename", "", "Read the document from stdin and report it under this name")
	fset.BoolVar(&exitZero, "exit-zero", false, "Always exit 0, even when errors are reported")
	fset.BoolVar(&noColor, "no-color", false, "Disable colored text output")
	fset.BoolVar(&quiet, "quiet", false, "Suppress warnings; report only errors")
	fset.BoolVar(&warnAsError, "warn-as-error", false, "Treat warnings as errors for the exit code")
	fset.BoolVar(&recursive, "recursive", true, "Walk directories for .hcl files")

	if err := fset.Parse(args); err != nil {
		return exitUsage
	}

	color := !noColor && isTerminal(os.Stdout)
	rep, err := newReporter(format, os.Stdout, color)
	if err != nil {
		return fail("%s", err)
	}

	opts := common.loadOptions()
	anyError := false
	anyWarning := false

	report := func(res hclschema.Result) {
		diags := res.Diagnostics
		if quiet {
			diags = errorsOnly(diags)
		}
		for _, d := range diags {
			switch d.Severity {
			case hcl.DiagError:
				anyError = true
			case hcl.DiagWarning:
				anyWarning = true
			}
		}
		if err := rep.Report(res.Filename, diags, res.Files); err != nil {
			fmt.Fprintln(os.Stderr, "hclschema:", err)
		}
	}

	if stdinFilename != "" {
		src, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fail("reading stdin: %s", err)
		}
		report(checkOne(src, stdinFilename, schemaPath, opts))
	} else {
		targets, err := expandTargets(fset.Args(), recursive)
		if err != nil {
			return fail("%s", err)
		}
		if len(targets) == 0 {
			fmt.Fprintln(os.Stderr, "hclschema: no HCL files found")
			return exitUsage
		}
		for _, path := range targets {
			src, err := os.ReadFile(path)
			if err != nil {
				return fail("reading %s: %s", path, err)
			}
			report(checkOne(src, path, schemaPath, opts))
		}
	}

	if err := rep.Close(); err != nil {
		return fail("writing output: %s", err)
	}

	switch {
	case exitZero:
		return exitOK
	case anyError, warnAsError && anyWarning:
		return exitFindings
	}
	return exitOK
}

// checkOne validates one document, either against an explicit schema or
// against the one it links to.
func checkOne(src []byte, filename, schemaPath string, opts hclschema.LoadOptions) hclschema.Result {
	if schemaPath == "" {
		return hclschema.Check(src, filename, opts)
	}

	loader := opts.ParseOptions.Loader
	schemaSrc, schemaName, diags := loader.Load(schemaPath, "", "")
	res := hclschema.Result{Filename: filename, Files: map[string]*hcl.File{}}
	if diags.HasErrors() {
		res.Diagnostics = diags
		return res
	}
	schema, sd := hclschema.ParseSchemaOpts(schemaSrc, schemaName, opts.ParseOptions)
	diags = append(diags, sd...)
	if schema == nil {
		res.Diagnostics = diags
		return res
	}

	inner := hclschema.Check(src, filename, hclschema.LoadOptions{
		ParseOptions: opts.ParseOptions,
	})
	res.File = inner.File
	res.Files = inner.Files
	res.Schema = schema
	res.Diagnostics = append(diags, inner.Diagnostics...)
	if inner.File != nil {
		res.Diagnostics = append(res.Diagnostics,
			schema.ValidateOpts(inner.File.Body, opts.ValidateOptions)...)
	}
	return res
}

func errorsOnly(diags hcl.Diagnostics) hcl.Diagnostics {
	out := make(hcl.Diagnostics, 0, len(diags))
	for _, d := range diags {
		if d.Severity == hcl.DiagError {
			out = append(out, d)
		}
	}
	return out
}

// expandTargets turns the positional arguments into a file list. A directory
// is walked for .hcl files; a bare pattern is expanded as a glob so that the
// tool behaves the same on shells that do not expand one.
func expandTargets(args []string, recursive bool) ([]string, error) {
	if len(args) == 0 {
		args = []string{"."}
	}
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if seen[abs] {
			return
		}
		seen[abs] = true
		out = append(out, p)
	}

	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			matches, gerr := filepath.Glob(arg)
			if gerr != nil || len(matches) == 0 {
				return nil, fmt.Errorf("no such file or directory: %s", arg)
			}
			for _, m := range matches {
				if isHCL(m) {
					add(m)
				}
			}
			continue
		}
		if !info.IsDir() {
			add(arg)
			continue
		}
		if !recursive {
			entries, err := os.ReadDir(arg)
			if err != nil {
				return nil, err
			}
			for _, e := range entries {
				if !e.IsDir() && isHCL(e.Name()) {
					add(filepath.Join(arg, e.Name()))
				}
			}
			continue
		}
		err = filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// Directories that never contain configuration worth checking,
				// and which would otherwise dominate the run.
				switch d.Name() {
				case ".git", "node_modules", "vendor", ".terraform":
					return filepath.SkipDir
				}
				return nil
			}
			if isHCL(path) {
				add(path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

func isHCL(name string) bool {
	return strings.HasSuffix(name, ".hcl") || strings.HasSuffix(name, ".hcl.json")
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
