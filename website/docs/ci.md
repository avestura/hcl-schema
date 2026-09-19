---
id: ci
title: Continuous integration
sidebar_position: 7
---

# Continuous integration

`hclschema validate` exits `1` when it reports an error and `2` when it could
not run, so it drops into a pipeline without a wrapper.

## GitHub Actions

### Inline annotations

`--format github` emits workflow commands, so findings appear on the changed
lines of a pull request with no extra action:

```yaml
name: hcl-schema

on: [push, pull_request]

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: stable
      - run: go install github.com/avestura/hcl-schema/cmd/hclschema-cli@latest
      - run: hclschema-cli validate --format github .
```

### Code scanning

`--format sarif` produces SARIF 2.1.0, which GitHub ingests as code scanning
alerts:

```yaml
      - run: hclschema-cli validate --format sarif -o results.sarif . || true
      - uses: github/codeql-action/upload-sarif@v3
        with:
          sarif_file: results.sarif
```

`|| true` is there because the upload step should still run when findings
exist. Keep a separate un-suppressed `validate` step if you also want the job
to fail.

## Working offline

CI that must not reach the network can vendor the schemas it needs:

```bash
hclschema bundle service.schema.hcl -o vendor/service.schema.hcl
```

Then validate against the vendored copy:

```bash
hclschema validate --offline --schema vendor/service.schema.hcl config/
```

`--offline` refuses network access outright: a remote schema is served from the
cache or reported as unavailable. It never silently succeeds by fetching.

## Pinning a remote schema

If you do fetch, pin the digest so that a change upstream cannot quietly change
what your build accepts:

```hcl
__schema        = "https://example.com/service.schema.hcl"
__schema_sha256 = "9f2c...64 hex characters..."
```

```bash
hclschema-cli validate .   # fails loudly if the digest no longer matches
```

Compute a digest with `sha256sum`, or with `hclschema.Sum` in Go.

## Keeping schemas formatted

```yaml
      - run: hclschema-cli fmt --check .
```

Rendering is idempotent, so this is stable rather than a source of churn.
