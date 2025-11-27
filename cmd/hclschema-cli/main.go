package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/avestura/hcl-schema/pkg/hclschema"
	"github.com/hashicorp/hcl/v2"
)

type OutDiagnostic struct {
	File      string `json:"file"`
	StartLine int    `json:"startLine"`
	StartCol  int    `json:"startCol"`
	EndLine   int    `json:"endLine"`
	EndCol    int    `json:"endCol"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
}

func diagSeverity(d *hcl.Diagnostic) string {
	switch d.Severity {
	case hcl.DiagError:
		return "error"
	case hcl.DiagWarning:
		return "warning"
	default:
		return "info"
	}
}

func main() {
	var detect bool
	flag.BoolVar(&detect, "detect", true, "Detect schema via __schema attribute and validate")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hclschema-cli <hcl-file>")
		os.Exit(2)
	}
	hclPath := args[0]

	var diags hcl.Diagnostics
	if detect {
		diags = hclschema.ValidateHCLWithLinkedSchema(hclPath)
	} else {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: hclschema-cli <hcl-file> <schema-file>")
			os.Exit(2)
		}
		schema := args[1]
		diags = hclschema.ValidateFileWithSchema(schema, hclPath)
	}

	out := make([]OutDiagnostic, 0, len(diags))
	for _, d := range diags {
		if d == nil {
			continue
		}
		startLine, startCol, endLine, endCol := 0, 0, 0, 0
		if d.Subject != nil {
			startLine = d.Subject.Start.Line - 1
			startCol = d.Subject.Start.Column - 1
			endLine = d.Subject.End.Line - 1
			endCol = d.Subject.End.Column - 1
		}
		file := hclPath
		if d.Subject != nil && d.Subject.Filename != "" {
			file = d.Subject.Filename
		} else {
			if !filepath.IsAbs(file) {
				if ab, err := filepath.Abs(file); err == nil {
					file = ab
				}
			}
		}

		msg := d.Summary
		if d.Detail != "" {
			if msg != "" {
				msg = msg + ": " + d.Detail
			} else {
				msg = d.Detail
			}
		}

		out = append(out, OutDiagnostic{
			File:      file,
			StartLine: startLine,
			StartCol:  startCol,
			EndLine:   endLine,
			EndCol:    endCol,
			Severity:  diagSeverity(d),
			Message:   msg,
		})
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "failed to emit json:", err)
		os.Exit(2)
	}
}
