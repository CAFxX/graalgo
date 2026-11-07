# ADR-0004 — M4 freeze exceptions (addendum: R-1107-06)

Status: accepted (addendum 2026-11-07, lane rev1107-22-r1107-06).

## Context

ADR-0004 froze the M4 milestone scope: `//go:linkname` support covered
bodyless non-generic functions (WS C). `export.go` carried the guard
`//go:linkname on generic func %s is not in M4 scope` for generic
functions, implying a later milestone would implement generic-func
linknames.

## Decision

The generic-func linkname case is NOT an M4-scope gap and is NOT
deferred to a future milestone. Verified against gc 1.27.1: gc rejects
bodyless generic functions outright (`generic function is missing
function body`; bodyless methods on generic types: `missing function
body`), with or without `//go:linkname`, and no generic linkname
exists in the go1.27.1 stdlib. There is no valid program to support.

graalgo therefore rejects bodyless generic functions/methods with gc's
error at the template skip points in the exporter, and the stale
"not in M4 scope" message is removed. The `exportLinknameDecl` guard
is retained as defense-in-depth returning the same gc error, since
both emission loops skip templates before reaching it.

## Consequences

- No milestone work is owed for generic-func linknames.
- Any future bodyless-template path reaching the exporter fails loudly
  with gc's wording instead of silently skipping or emitting a linkname
  for a declaration gc rejects.
