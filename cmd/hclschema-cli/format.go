package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
)

// OutDiagnostic is the JSON shape the editor extension consumes. Its fields
// are zero-based to match the editor's own coordinates.
type OutDiagnostic struct {
	File      string `json:"file"`
	StartLine int    `json:"startLine"`
	StartCol  int    `json:"startCol"`
	EndLine   int    `json:"endLine"`
	EndCol    int    `json:"endCol"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Summary   string `json:"summary,omitempty"`
	Detail    string `json:"detail,omitempty"`
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

func toOut(d *hcl.Diagnostic, fallbackFile string) OutDiagnostic {
	out := OutDiagnostic{
		Severity: diagSeverity(d),
		Summary:  d.Summary,
		Detail:   d.Detail,
		File:     fallbackFile,
	}
	if d.Subject != nil {
		out.StartLine = d.Subject.Start.Line - 1
		out.StartCol = d.Subject.Start.Column - 1
		out.EndLine = d.Subject.End.Line - 1
		out.EndCol = d.Subject.End.Column - 1
		if d.Subject.Filename != "" {
			out.File = d.Subject.Filename
		}
	}
	if out.File != "" && !isRemote(out.File) && !filepath.IsAbs(out.File) {
		if abs, err := filepath.Abs(out.File); err == nil {
			out.File = abs
		}
	}
	out.Message = d.Summary
	if d.Detail != "" {
		if out.Message != "" {
			out.Message += ": " + d.Detail
		} else {
			out.Message = d.Detail
		}
	}
	return out
}

func isRemote(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// reporter renders diagnostics in one of the supported output formats.
type reporter interface {
	Report(filename string, diags hcl.Diagnostics, files map[string]*hcl.File) error
	Close() error
}

func newReporter(format string, w io.Writer, color bool) (reporter, error) {
	switch format {
	case "", "text":
		return &textReporter{w: w, color: color}, nil
	case "json":
		return &jsonReporter{w: w}, nil
	case "github":
		return &githubReporter{w: w}, nil
	case "sarif":
		return &sarifReporter{w: w}, nil
	}
	return nil, fmt.Errorf("unknown format %q (want text, json, github or sarif)", format)
}

// textReporter uses HCL's own diagnostic writer, which prints the offending
// source line with a caret under the range.
type textReporter struct {
	w     io.Writer
	color bool
	count int
}

func (r *textReporter) Report(filename string, diags hcl.Diagnostics, files map[string]*hcl.File) error {
	if len(diags) == 0 {
		return nil
	}
	width := 80
	wr := hcl.NewDiagnosticTextWriter(r.w, files, uint(width), r.color)
	for _, d := range diags {
		r.count++
		if err := wr.WriteDiagnostic(d); err != nil {
			return err
		}
	}
	return nil
}

func (r *textReporter) Close() error { return nil }

type jsonReporter struct {
	w   io.Writer
	all []OutDiagnostic
}

func (r *jsonReporter) Report(filename string, diags hcl.Diagnostics, _ map[string]*hcl.File) error {
	for _, d := range diags {
		if d == nil {
			continue
		}
		r.all = append(r.all, toOut(d, filename))
	}
	return nil
}

func (r *jsonReporter) Close() error {
	if r.all == nil {
		r.all = []OutDiagnostic{}
	}
	enc := json.NewEncoder(r.w)
	enc.SetIndent("", "  ")
	return enc.Encode(r.all)
}

// githubReporter emits workflow commands so that findings appear inline on a
// pull request without any extra action.
type githubReporter struct {
	w io.Writer
}

func (r *githubReporter) Report(filename string, diags hcl.Diagnostics, _ map[string]*hcl.File) error {
	for _, d := range diags {
		if d == nil {
			continue
		}
		out := toOut(d, filename)
		level := "notice"
		switch out.Severity {
		case "error":
			level = "error"
		case "warning":
			level = "warning"
		}
		rel := out.File
		if cwd, err := os.Getwd(); err == nil && !isRemote(rel) {
			if r, err := filepath.Rel(cwd, rel); err == nil && !strings.HasPrefix(r, "..") {
				rel = r
			}
		}
		fmt.Fprintf(r.w, "::%s file=%s,line=%d,col=%d,endLine=%d,endColumn=%d::%s\n",
			level, filepath.ToSlash(rel),
			out.StartLine+1, out.StartCol+1, out.EndLine+1, out.EndCol+1,
			escapeWorkflow(out.Message))
	}
	return nil
}

func (r *githubReporter) Close() error { return nil }

// escapeWorkflow encodes the characters that would otherwise terminate a
// workflow command.
func escapeWorkflow(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}

// sarifReporter emits SARIF 2.1.0, which GitHub code scanning and several
// other tools ingest directly.
type sarifReporter struct {
	w       io.Writer
	results []sarifResult
	rules   map[string]bool
}

type sarifResult struct {
	RuleID    string           `json:"ruleId"`
	Level     string           `json:"level"`
	Message   sarifMessage     `json:"message"`
	Locations []sarifLocation  `json:"locations"`
	Fixes     []map[string]any `json:"-"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           sarifRegion   `json:"region"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
	EndLine     int `json:"endLine"`
	EndColumn   int `json:"endColumn"`
}

func (r *sarifReporter) Report(filename string, diags hcl.Diagnostics, _ map[string]*hcl.File) error {
	if r.rules == nil {
		r.rules = map[string]bool{}
	}
	for _, d := range diags {
		if d == nil {
			continue
		}
		out := toOut(d, filename)
		level := "note"
		switch out.Severity {
		case "error":
			level = "error"
		case "warning":
			level = "warning"
		}
		rule := ruleID(out.Summary)
		r.rules[rule] = true

		uri := out.File
		if cwd, err := os.Getwd(); err == nil && !isRemote(uri) {
			if rel, err := filepath.Rel(cwd, uri); err == nil && !strings.HasPrefix(rel, "..") {
				uri = rel
			}
		}
		r.results = append(r.results, sarifResult{
			RuleID:  rule,
			Level:   level,
			Message: sarifMessage{Text: out.Message},
			Locations: []sarifLocation{{
				PhysicalLocation: sarifPhysical{
					ArtifactLocation: sarifArtifact{URI: filepath.ToSlash(uri)},
					// SARIF regions are one-based and the end column is
					// exclusive, which is what HCL ranges already are.
					Region: sarifRegion{
						StartLine:   max1(out.StartLine + 1),
						StartColumn: max1(out.StartCol + 1),
						EndLine:     max1(out.EndLine + 1),
						EndColumn:   max1(out.EndCol + 1),
					},
				},
			}},
		})
	}
	return nil
}

func (r *sarifReporter) Close() error {
	rules := make([]map[string]any, 0, len(r.rules))
	for id := range r.rules {
		rules = append(rules, map[string]any{
			"id":               id,
			"shortDescription": map[string]any{"text": id},
		})
	}
	if r.results == nil {
		r.results = []sarifResult{}
	}
	doc := map[string]any{
		"$schema": "https://json.schemastore.org/sarif-2.1.0.json",
		"version": "2.1.0",
		"runs": []map[string]any{{
			"tool": map[string]any{
				"driver": map[string]any{
					"name":           "hclschema",
					"version":        Version,
					"informationUri": "https://github.com/avestura/hcl-schema",
					"rules":          rules,
				},
			},
			"results": r.results,
		}},
	}
	enc := json.NewEncoder(r.w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

func ruleID(summary string) string {
	if summary == "" {
		return "hclschema"
	}
	s := strings.ToLower(summary)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '_':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
