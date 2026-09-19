# Changelog

## 0.1.0

The extension is now a client for the `hclschema lsp` language server rather
than a wrapper that shells out to the CLI on every keystroke.

### Added

- **Completion** of the attributes and blocks the schema declares, and of an
  attribute's permitted values when it has an `enum`.
- **Hover** documentation built from the schema's `description`, type, bounds
  and default.
- **Go to definition** from a use in an instance to its declaration in the
  schema document.
- **Document outline** (`documentSymbol`).
- Settings `hclSchema.strict`, `hclSchema.offline` and `hclSchema.cacheDir`.
- Commands **Restart language server** and **Show output**.

### Fixed

- **Unsaved edits are now validated.** Validation previously ran against the
  file on disk, so diagnostics lagged behind the buffer until you saved.

### Changed

- The VSIX is published per platform, so a download carries one binary instead
  of all six. It is roughly a fifth of the previous size.
- Activation waits for an HCL file or a workspace containing a `*.schema.hcl`
  rather than firing on startup.

## 0.0.2

- Bundled the CLI for several platforms.

## 0.0.1

- Initial release: diagnostics for HCL files with a `__schema` attribute.
