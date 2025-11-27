package main

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

type CLIOutDiagnostic struct {
	File      string `json:"file"`
	StartLine int    `json:"startLine"`
	StartCol  int    `json:"startCol"`
	EndLine   int    `json:"endLine"`
	EndCol    int    `json:"endCol"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
}

func TestCLIReportsErrorForInvalidLinkedFile(t *testing.T) {
	hclPath := filepath.Join("..", "..", "pkg", "hclschema", "testdata", "invalid_linked.hcl")
	cmd := exec.Command("go", "run", "./main.go", "--detect", hclPath)
	cmd.Dir = "./"
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Even if exit code != 0, the CLI should print JSON; attempt to parse whatever
	}

	var diags []CLIOutDiagnostic
	if err := json.Unmarshal(out, &diags); err != nil {
		t.Fatalf("failed to parse CLI output as JSON: %v; output: %s", err, string(out))
	}

	if len(diags) == 0 {
		t.Fatalf("expected diagnostics for invalid file, got none; output: %s", string(out))
	}

	hasError := false
	for _, d := range diags {
		if d.Severity == "error" {
			hasError = true
			break
		}
	}
	if !hasError {
		t.Fatalf("expected at least one error severity diagnostic, got: %#v", diags)
	}
}
