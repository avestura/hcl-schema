---
id: drafts
title: Drafts and compatibility
sidebar_position: 3
---

# Drafts and compatibility

A schema names its meta-schema in `__schema`, and the `draft/<id>/` segment of
that reference selects the draft:

```hcl
__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
```

Recognising the draft is a matter of finding that path segment, so mirrors and
vendored copies keep working as long as they preserve the path shape. An
unrecognised reference is treated as **2025-10**, the permissive reading, so an
unfamiliar meta-schema never fails closed.

## 2025-10

The original draft.

- `attribute` takes `required`.
- `block_header` takes `label_names`, `id` and `ref`.
- A `block_header` with no `body` accepts arbitrary content.
- A name declared twice in one body silently resolves to the first.

## 2026-09

Adds the type system, cardinality, per-label constraints, cross-field
constraints, imports and variants, and tightens two structural rules.

### What is additive

Every key added after 2025-10 is optional in the meta-schema, so an existing
`*.schema.hcl` parses unchanged. New keys also work in a document that pins
2025-10 — features are not gated on the draft, only strictness is.

### What is breaking

Two rules change meaning, which is the whole reason for a new draft:

| Rule | 2025-10 | 2026-09 |
| --- | --- | --- |
| `block_header` with no `body` | Accepts anything | Must be empty |
| A name declared twice in a body | Warning; first wins | Error |

Both apply only to documents that pin 2026-09. Nothing you have written changes
meaning until you change its `__schema`.

## Migrating

Two ways, and you can mix them:

**Per file.** Change `__schema` to the 2026-09 URL and fix what it reports.

**Across the repository, without editing anything.** `--strict` applies the new
rules regardless of what a document pins:

```bash
hclschema validate --strict .
```

That is the way to find out what a migration would cost before committing to
it. The same switch exists as `ParseOptions.Strict` and
`ValidateOptions.Strict` in the library, and as `hclSchema.strict` in the VS
Code extension.

## Tool compatibility

Two contracts are deliberately preserved.

**The legacy CLI invocation.** `hclschema --detect <file>` still prints JSON to
stdout and still exits `0` even when the file has errors. The VS Code extension
that shipped against it treats a non-zero exit as a crash, so changing the code
would break installed copies. Use `hclschema validate` for anything new — it
has proper exit codes.

**`ValidateFileWithSchema` and `ValidateHCLWithLinkedSchema`.** Unchanged
signatures and behaviour.

`ParseSchemaFile` is the one Go API that did change: it now returns `*Schema`
rather than `*BlockHeaderAndBodySchema`. Call `Schema.Legacy()` for the old
shape.
