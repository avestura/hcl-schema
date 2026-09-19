---
id: security
title: Security
sidebar_position: 8
---

# Security

A schema decides whether configuration is accepted, so fetching one from the
network is a trust decision. These are the defaults.

## Transport

- **https only.** An `http://` reference is refused, not downgraded silently.
- **Redirects are checked.** A redirect from https to anything else is refused
  even though the first hop was secure. Chains stop after five hops.
  `Loader.AllowCrossHostRedirect` controls whether a hop may change host.
- **Size is capped** at 1 MiB (`Loader.MaxSize`).
- **Fetches time out** after 15 seconds (`Loader.Timeout`).

## Integrity

`__schema_sha256` pins a schema to an exact digest:

```hcl
__schema        = "https://example.com/service.schema.hcl"
__schema_sha256 = "9f2c...64 hex characters..."
```

An `import` block takes the same thing as `sha256`. A mismatch is an error, and
a **cached** copy that fails its pin is deleted rather than served — a cache
entry that no longer matches is evidence of tampering or staleness either way.

## The cache

Cached schemas live in the per-user cache directory
(`os.UserCacheDir()/hclschema`) with owner-only permissions, and entries are
written through a temporary file in the same directory so a reader never sees a
half-written schema.

:::warning Changed in draft 2026-09
The cache used to live in the shared system temp directory
(`os.TempDir()/hclschema-cache`, mode `0755`) under a filename derived
predictably from the URL. On a multi-user machine another account could plant a
file there that this tool would then trust. Nothing is read from the old
location any more; delete it if it is still around.
:::

Entries are revalidated with `ETag` once past their TTL (24 hours by default),
so a `304` refreshes an entry without re-downloading it. When the network is
unreachable and a cached copy exists, it is served with a **warning** rather
than an error — stale validation beats no validation, but you are told.

## Offline mode

`--offline`, or `Loader.Offline`, forbids network access entirely. A remote
schema is served from the cache or reported as unavailable. This is the setting
for a build that must be reproducible.

## Evaluating instance values

Attribute values are evaluated with a nil `EvalContext` by default: no
variables, no functions. A schema document's own values are *always* evaluated
that way, so a schema cannot call anything.

`validation` conditions are ordinary HCL expressions evaluated with `self`
bound to the value under test. They run in the same sandbox — HCL expressions
have no I/O — but a pathological condition can still burn CPU, so treat a
schema from an untrusted source the way you would any other input and pin it.

## Sensitive values

An attribute marked `sensitive = true` has its value replaced with
`(sensitive value)` in diagnostics. Diagnostics routinely end up in CI logs,
and a failing pattern check on a credential should not print the credential.

## Resource limits

| Limit | Default | Setting |
| --- | --- | --- |
| Remote schema size | 1 MiB | `Loader.MaxSize` |
| Fetch timeout | 15s | `Loader.Timeout` |
| Redirect hops | 5 | fixed |
| Import depth | 16 | `ParseOptions.MaxDepth` |

Import cycles are detected and reported rather than followed.
