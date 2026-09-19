package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildCLI compiles the command once and returns its path. Exercising the real
// binary is the only way to assert exit codes, which are the contract CI
// depends on.
func buildCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "hclschema")
	if os.PathSeparator == '\\' {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the CLI failed: %v\n%s", err, out)
	}
	return bin
}

func run(t *testing.T, bin string, args ...string) (stdout string, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return out.String(), errb.String(), ee.ExitCode()
		}
		t.Fatalf("running %v: %v", args, err)
	}
	return out.String(), errb.String(), 0
}

func testdata(name string) string {
	return filepath.Join("..", "..", "pkg", "hclschema", "testdata", name)
}

// A clean file exits 0.
func TestValidateExitsZeroWhenClean(t *testing.T) {
	bin := buildCLI(t)
	_, stderr, code := run(t, bin, "validate", "--offline", testdata("typed_ok.hcl"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", code, stderr)
	}
}

// The original CLI always exited 0, which made it useless in CI. The validate
// subcommand reports findings through the exit code.
func TestValidateExitsOneOnErrors(t *testing.T) {
	bin := buildCLI(t)
	stdout, _, code := run(t, bin, "validate", "--offline", "--format", "json", testdata("typed_bad.hcl"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1\nstdout: %s", code, stdout)
	}
	var diags []OutDiagnostic
	if err := json.Unmarshal([]byte(stdout), &diags); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, stdout)
	}
	if len(diags) == 0 {
		t.Fatal("expected diagnostics")
	}
}

func TestValidateExitZeroFlag(t *testing.T) {
	bin := buildCLI(t)
	_, _, code := run(t, bin, "validate", "--offline", "--exit-zero", testdata("typed_bad.hcl"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0 with --exit-zero", code)
	}
}

// The editor extension that shipped before subcommands existed treats a
// non-zero exit as a crash, so the legacy invocation must keep exiting 0.
func TestLegacyInvocationStillExitsZero(t *testing.T) {
	bin := buildCLI(t)
	stdout, _, code := run(t, bin, "--detect", testdata("invalid_linked.hcl"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0 for the legacy invocation", code)
	}
	var diags []OutDiagnostic
	if err := json.Unmarshal([]byte(stdout), &diags); err != nil {
		t.Fatalf("legacy output is not JSON: %v\n%s", err, stdout)
	}
	if len(diags) == 0 {
		t.Fatal("expected diagnostics from the legacy path")
	}
	hasError := false
	for _, d := range diags {
		if d.Severity == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Fatalf("expected an error severity, got %+v", diags)
	}
}

// Validating the buffer rather than the file on disk is what the editor needs.
func TestValidateReadsStdin(t *testing.T) {
	bin := buildCLI(t)
	cmd := exec.Command(bin, "validate", "--offline", "--format", "json",
		"--stdin-filename", testdata("typed_ok.hcl"))
	cmd.Stdin = strings.NewReader("__schema = \"typed.schema.hcl\"\nname = \"NOT VALID\"\n")
	out, err := cmd.Output()
	if err == nil {
		t.Fatal("expected a non-zero exit for the invalid buffer")
	}
	if !strings.Contains(string(out), "pattern") {
		t.Fatalf("expected a pattern error from the buffer, got:\n%s", out)
	}
}

func TestFormats(t *testing.T) {
	bin := buildCLI(t)
	for _, tc := range []struct {
		format string
		want   string
	}{
		{"text", "Value above maximum"},
		{"github", "::error file="},
		{"sarif", `"version": "2.1.0"`},
		{"json", `"severity": "error"`},
	} {
		t.Run(tc.format, func(t *testing.T) {
			stdout, _, _ := run(t, bin, "validate", "--offline", "--no-color",
				"--format", tc.format, testdata("typed_bad.hcl"))
			if !strings.Contains(stdout, tc.want) {
				t.Fatalf("%s output missing %q:\n%s", tc.format, tc.want, stdout)
			}
		})
	}
}

func TestUnknownFormatIsAUsageError(t *testing.T) {
	bin := buildCLI(t)
	_, _, code := run(t, bin, "validate", "--format", "yaml", testdata("typed_ok.hcl"))
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for a usage error", code)
	}
}

func TestExplicitSchemaFlag(t *testing.T) {
	bin := buildCLI(t)
	_, stderr, code := run(t, bin, "validate", "--offline",
		"--schema", testdata("simple.schema.hcl"), testdata("simple.hcl"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, stderr)
	}
}

func TestInferRoundTrip(t *testing.T) {
	bin := buildCLI(t)
	out, stderr, code := run(t, bin, "infer", "--id", "local://x", testdata("typed_ok.hcl"))
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(out, "__schema") || !strings.Contains(out, "body {") {
		t.Fatalf("inferred output looks wrong:\n%s", out)
	}

	// The generated schema must accept the document it came from.
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "gen.schema.hcl")
	if err := os.WriteFile(schemaPath, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code = run(t, bin, "validate", "--offline", "--schema", schemaPath, testdata("typed_ok.hcl"))
	if code != 0 {
		t.Fatalf("inferred schema rejects its own sample: exit %d\n%s", code, stderr)
	}
}

func TestDocsCommand(t *testing.T) {
	bin := buildCLI(t)
	out, stderr, code := run(t, bin, "docs", "--offline", testdata("typed.schema.hcl"))
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	for _, want := range []string{"---", "title:", "| Attribute |", "`name`"} {
		if !strings.Contains(out, want) {
			t.Fatalf("docs output missing %q:\n%s", want, out)
		}
	}
}

func TestBundleCommand(t *testing.T) {
	bin := buildCLI(t)
	out, stderr, code := run(t, bin, "bundle", "--offline", testdata("importer.schema.hcl"))
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if strings.Contains(out, "import ") {
		t.Fatalf("bundle kept its imports:\n%s", out)
	}
	if !strings.Contains(out, "attempts") {
		t.Fatalf("bundle lost the imported declarations:\n%s", out)
	}
}

func TestFmtCheckDetectsUnformatted(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "x.schema.hcl")
	src := "__schema=\"" + draftURL + "\"\n__id=\"local://x\"\nbody {\nattribute \"a\" {\nrequired=true\n}\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, code := run(t, bin, "fmt", "--check", path)
	if code != 1 {
		t.Fatalf("--check exit = %d, want 1 for an unformatted file", code)
	}

	if _, stderr, code := run(t, bin, "fmt", "-w", path); code != 0 {
		t.Fatalf("fmt -w exit = %d\n%s", code, stderr)
	}
	if _, _, code := run(t, bin, "fmt", "--check", path); code != 0 {
		t.Fatalf("--check still reports the file after rewriting it")
	}
}

// Formatting re-renders from a parse tree that has no comments, so a commented
// file must be left alone rather than silently stripped.
func TestFmtSkipsCommentedFiles(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "c.schema.hcl")
	src := "// keep me\n__schema=\"" + draftURL + "\"\n__id=\"local://c\"\nbody {\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := run(t, bin, "fmt", "-w", path)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "discard its comments") {
		t.Fatalf("expected a skip notice, got: %s", stderr)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != src {
		t.Fatalf("the file was rewritten despite its comments: %s", after)
	}

	if _, _, code := run(t, bin, "fmt", "-w", "--force", path); code != 0 {
		t.Fatalf("--force exit = %d", code)
	}
	forced, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(forced), "keep me") {
		t.Fatal("--force was expected to drop the comment")
	}
}

func TestVersionCommand(t *testing.T) {
	bin := buildCLI(t)
	out, _, code := run(t, bin, "version")
	if code != 0 || !strings.HasPrefix(out, "hclschema ") {
		t.Fatalf("version output %q exit %d", out, code)
	}
}

func TestDirectoryWalk(t *testing.T) {
	bin := buildCLI(t)
	// The testdata directory deliberately contains invalid documents, so the
	// walk should find them and exit 1 rather than silently checking nothing.
	stdout, _, code := run(t, bin, "validate", "--offline", "--format", "json",
		filepath.Join("..", "..", "pkg", "hclschema", "testdata"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	var diags []OutDiagnostic
	if err := json.Unmarshal([]byte(stdout), &diags); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(diags) < 5 {
		t.Fatalf("expected the walk to find several problems, got %d", len(diags))
	}
}

const draftURL = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
