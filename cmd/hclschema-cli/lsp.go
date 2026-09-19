package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/avestura/hcl-schema/internal/lsp"
)

func runLSP(args []string) int {
	fset := flag.NewFlagSet("lsp", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: hclschema lsp [flags]

Runs the language server over stdin and stdout, speaking LSP.

Provides diagnostics for the unsaved buffer, completion of declared attributes
and blocks, hover documentation, go-to-definition into the schema, and a
document outline.

Flags:
`)
		fset.PrintDefaults()
	}
	var (
		common commonFlags
	)
	common.register(fset)
	if err := fset.Parse(args); err != nil {
		return exitUsage
	}

	srv := lsp.NewServer(os.Stdin, os.Stdout, lsp.Options{
		Loader: common.loader(),
		Strict: common.strict,
		Log:    os.Stderr,
	})
	if err := srv.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "hclschema lsp:", err)
		return exitUsage
	}
	return exitOK
}
