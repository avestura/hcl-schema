---
id: cli
title: CLI
sidebar_position: 4
---

# CLI

```
hclschema <command> [flags] [files...]
```

| Command | Purpose |
| --- | --- |
| [`validate`](#validate) | Check HCL files against their schemas |
| [`fmt`](#fmt) | Rewrite schema documents in canonical form |
| [`infer`](#infer) | Derive a starting schema from existing files |
| [`docs`](#docs) | Render Markdown reference documentation |
| [`bundle`](#bundle) | Inline imports into one self-contained document |
| [`lsp`](#lsp) | Run the language server |
| `version` | Print the version |

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | No error diagnostics |
| `1` | At least one error was reported |
| `2` | Usage problem, or a file could not be read |

Separating "found problems" from "could not run" is what lets a CI job tell a
failing check apart from a broken invocation.

## Shared flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--strict` | `false` | Apply draft 2026-09 rules regardless of what a schema pins |
| `--offline` | `false` | Never reach the network; serve remote schemas from the cache only |
| `--cache-dir` | per-user cache dir | Where cached remote schemas live |
| `--timeout` | `15s` | Timeout for a single remote fetch |

## `validate`

```bash
hclschema validate [flags] [files or directories...]
```

With no arguments the current directory is walked. Directories are walked
recursively for `.hcl` and `.hcl.json` files, skipping `.git`, `node_modules`,
`vendor` and `.terraform`. A pattern that your shell did not expand is treated
as a glob.

A `*.schema.hcl` file is checked against the meta-schema rather than against a
schema of its own.

| Flag | Meaning |
| --- | --- |
| `--schema <path>` | Validate against this schema instead of each file's `__schema` |
| `--format <fmt>` | `text` (default), `json`, `github` or `sarif` |
| `--stdin-filename <name>` | Read the document from stdin, report it under this name |
| `--exit-zero` | Always exit `0` |
| `--warn-as-error` | Count warnings toward the exit code |
| `--quiet` | Report only errors |
| `--no-color` | Disable colour in `text` output |
| `--recursive` | Walk directories (default `true`) |

### Output formats

**`text`** uses HCL's own diagnostic writer, so you get the offending line with
a caret under the range:

```
Error: Value above maximum

  on config/service.hcl line 6, in listener "https":
   6:   port = 99999

"port" has 99999, above the maximum of 65535.
```

**`json`** emits an array with zero-based positions, the shape the editor
integration consumes:

```json
[
  {
    "file": "/abs/path/config/service.hcl",
    "startLine": 5,
    "startCol": 9,
    "endLine": 5,
    "endCol": 14,
    "severity": "error",
    "message": "Value above maximum: \"port\" has 99999, above the maximum of 65535.",
    "summary": "Value above maximum",
    "detail": "\"port\" has 99999, above the maximum of 65535."
  }
]
```

**`github`** emits workflow commands, which appear as inline annotations on a
pull request with no extra action:

```
::error file=config/service.hcl,line=6,col=10,endLine=6,endColumn=15::Value above maximum: ...
```

**`sarif`** emits SARIF 2.1.0 for GitHub code scanning. See [CI](./ci.md).

### Checking a buffer

```bash
hclschema validate --stdin-filename config/service.hcl < buffer.hcl
```

The name is used to resolve a relative `__schema` and to report positions; the
content comes from stdin. This is how an editor checks unsaved work — though
the [language server](#lsp) is the better answer for that.

## `fmt`

```bash
hclschema fmt [-w] [--check] [files...]
```

Parses each `*.schema.hcl` and re-renders it through `hclwrite`, which
normalises key order, spacing and indentation. Rendering is idempotent, so
`--check` is meaningful:

| Flag | Meaning |
| --- | --- |
| `-w` | Rewrite the file in place |
| `--check` | List unformatted files and exit `1`; write nothing |
| `--force` | Format a file even though its comments would be lost |

Without `-w` or `--check`, the result goes to stdout.

:::warning Comments
Formatting works by parsing and re-rendering, and HCL's parse tree holds no
comments. A file containing comments is therefore **skipped**, with a note on
stderr, rather than silently stripped. `--force` formats it anyway and loses
them.
:::

## `infer`

```bash
hclschema infer [flags] <files or directories...>
```

Derives a schema from existing documents. It reads the native-syntax parse tree
directly, so JSON samples are skipped with a warning.

| Flag | Meaning |
| --- | --- |
| `--id <id>` | Value for the generated `__id` |
| `-o <path>` | Write to a file instead of stdout |
| `--required-when-ubiquitous` | Mark an attribute required when every sample has it |

Inference deliberately does not guess: where samples disagree on a type, no
type is emitted, because a wrong constraint would reject a file that is
actually fine. Treat the output as a first draft.

## `docs`

```bash
hclschema docs [flags] <schema.hcl>
```

| Flag | Meaning |
| --- | --- |
| `--title <text>` | Document title (default: the schema's `__id`) |
| `-o <path>` | Write to a file instead of stdout |
| `--heading-offset <n>` | Shift every heading down, for embedding |
| `--no-frontmatter` | Omit the Docusaurus front matter |

The output carries Docusaurus front matter by default, so it drops straight
into a docs site:

```bash
hclschema docs service.schema.hcl -o website/docs/reference/service.md
```

## `bundle`

```bash
hclschema bundle [flags] <schema.hcl>
```

Writes one self-contained document with no `import` blocks. Shared and
recursive bodies get a generated `id` and are emitted once. Use it to vendor a
schema so that validation needs no network access.

## `lsp`

```bash
hclschema lsp [flags]
```

Speaks LSP over stdin and stdout. See [Editors](./editors.md).
