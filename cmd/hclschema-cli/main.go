// Command hclschema-cli validates HCL files against `*.schema.hcl` schemas.
package main

import (
	"fmt"
	"os"
)

// Version is stamped at build time with -ldflags "-X main.Version=...".
var Version = "dev"

const usage = `hclschema - schema validation for HCL

Usage:
  hclschema <command> [flags] [files...]

Commands:
  validate    Check HCL files against their schemas
  fmt         Rewrite schema documents in canonical form
  infer       Derive a starting schema from existing HCL files
  docs        Render Markdown reference documentation for a schema
  bundle      Inline a schema's imports into one self-contained document
  lsp         Run the language server over stdin/stdout
  version     Print the version

Run "hclschema <command> -h" for the flags of a command.

Legacy invocation:
  hclschema --detect <file.hcl>
  hclschema --detect=false <file.hcl> <file.schema.hcl>

  Prints JSON diagnostics and always exits 0. Kept for the editor extension
  that shipped against it; new callers should use "validate".
`

// Exit codes. Separating "found problems" from "could not run" is what lets
// a CI job tell a failing check apart from a broken invocation.
const (
	exitOK       = 0
	exitFindings = 1
	exitUsage    = 2
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(exitUsage)
	}

	switch args[0] {
	case "validate":
		os.Exit(runValidate(args[1:]))
	case "fmt":
		os.Exit(runFmt(args[1:]))
	case "infer":
		os.Exit(runInfer(args[1:]))
	case "docs":
		os.Exit(runDocs(args[1:]))
	case "bundle":
		os.Exit(runBundle(args[1:]))
	case "lsp":
		os.Exit(runLSP(args[1:]))
	case "version", "--version", "-version":
		fmt.Println("hclschema", Version)
		os.Exit(exitOK)
	case "help", "-h", "--help":
		fmt.Print(usage)
		os.Exit(exitOK)
	}

	// Anything else is the pre-subcommand invocation the editor extension uses.
	os.Exit(runLegacy(args))
}

func fail(format string, a ...any) int {
	fmt.Fprintf(os.Stderr, "hclschema: "+format+"\n", a...)
	return exitUsage
}
