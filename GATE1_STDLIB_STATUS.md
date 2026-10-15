# Gate-1: Full Stdlib Compile Survey

**Date:** 2026-10-15
**Base:** master @ `17cb0498`
**Method:** For each of the 383 Go 1.27.1 stdlib packages (`go list std`),
synthesize `package main; import _ "<pkg>"; func main() {}` and run it through
`gofrontend` (the graalgo frontend binary). Success = exit 0 (frontend accepts
the package via the disk importer). Failure = exit 2 with the frontend error.
This tests **frontend compile acceptance**, not backend/JVM execution.

**Tooling:** `gofrontend` built from `internal/frontend/` (module
`graalgo-frontend`); driver script `/tmp/stdlib_survey.py` (2 workers,
300s timeout per package). Raw results: `/tmp/stdlib_results.jsonl`
(383 lines, JSONL: package, ok, error).

## Summary

| Metric | Count | Pct |
|--------|-------|-----|
| Total packages surveyed | 383 | 100% |
| Compile OK | 160 | 41.8% |
| Compile FAIL | 223 | 58.2% |

## Failure categories

| Category | Count | Description |
|----------|-------|-------------|
| Dependency cascade | ~180 | Package fails because a dependency fails (e.g. `go/ast` → `go/scanner` → `path/filepath` → `os.PathSeparator`). The leaf causes below are the real fixes. |
| `call to unknown function` | 14 | Frontend can't resolve a function from a synthetic/disk package (`IndexByte`, `DecodeRune`, `BinarySearchFunc`, `Equal`, `New`, `polyByteDecode`). |
| `asm-stub without body` | 17 | Assembly-only functions (`cpuid`, `Xadd`, `Syscall6`, etc.). Needs asm support (deferred). |
| Multi-valued call results | 11 | `multi-valued call result is not supported in frontend/v1`. Frontend limitation. |
| Synthetic package type errors | 5 | The synthetic implementations themselves don't type-check (`archive/tar`, `archive/zip`, `mime`, `mime/multipart`, `text/scanner`). |
| Scope limitations (M5/M7/M8/M9) | 6 | `X is not in MN scope`. Known workstream gaps. |
| Declaration ordering | 5 | `variable "form" used before declaration`. Frontend ordering bug. |
| `unsafe.Sizeof` non-constant | 3 | `unsafe.Sizeof: operand is not a constant expression`. |
| Other frontend gaps | ~12 | Interface multi-result methods, `&` on selector, non-function calls, etc. |

## Top 5 root causes (by cascade impact)

### 1. Synthetic `os` missing `PathSeparator` constant
**Blocks:** `path/filepath` → `go/scanner` → `go/ast` → `go/parser` →
`go/types` → `go/printer` → `go/format` → `go/doc` → `go/build` →
`go/importer` (+ `internal/buildcfg`, `internal/goroot`, etc.). **~17 packages.**

**Evidence:** `path/filepath/path.go:24:21: undefined: os.PathSeparator`.
The synthetic `os` (`internal/frontend/synth_os.go`) exports `IsPathSeparator`
but not the `PathSeparator`/`PathListSeparator` constants. The real
`path/filepath` (via disk importer) needs them.

**Fix:** Add `PathSeparator` and `PathListSeparator` constants to synthetic
`os`. Trivial, high-leverage. (Note: `path/filepath` also has a synthetic
implementation `synth_filepath.go`; the disk-imported real one should work
too once `os` is complete.)

### 2. Multi-valued call results not supported in frontend/v1
**Blocks:** `image`, `image/draw`, `image/gif`, `image/jpeg`, `image/png`,
`image/internal/imageutil`, `regexp`, `index/suffixarray`,
`net/http/internal/httpsfv`, `internal/lazyregexp`,
`crypto/internal/cryptotest/x509limbo`. **11 packages.**

**Evidence:** `gofrontend: multi-valued call result is not supported in
frontend/v1 scope`.

**Fix:** General frontend support for multi-valued call results (not just
in specific positions). This is a language-construct gap, not a
stdlib-specific issue.

### 3. `call to unknown function` — function resolution gaps
**Blocks (14 packages):**
- `IndexByte`: `compress/flate` → `compress/gzip`, `compress/zlib`,
  `compress/lzw` (4)
- `DecodeRune`: `encoding/json/internal/jsonwire`,
  `vendor/golang.org/x/text/secure/bidirule`,
  `vendor/golang.org/x/text/transform` (3)
- `BinarySearchFunc` (pkg slices): `go/constant`, `go/token` (2)
- `Equal` (pkg slices): `regexp/syntax` (1)
- `New`: `compress/bzip2` (1)
- `polyByteDecode`: `crypto/internal/fips140/mlkem`, `crypto/mlkem`,
  `crypto/mlkem/mlkemtest` (3)

**Evidence:** e.g. `gofrontend: call to unknown function IndexByte`.

**Fix:** The function registry doesn't resolve these across the
synthetic/disk boundary. General fix in the exporter's function lookup,
not per-function stubs.

### 4. Synthetic package type errors (synthetic bugs, not frontend gaps)
**Blocks (5 packages):**
- `archive/tar`: `undefined: fs.FormatFileInfo` (synthetic `io/fs` incomplete)
- `archive/zip`: `undefined: os.PathSeparator` (same as #1) + `could not import path/filepath`
- `mime`, `mime/multipart`: `undefined: base64.NewEncoder` (synthetic `encoding/base64` incomplete)
- `text/scanner`: `undefined: fmt.Fprintf` (synthetic `fmt` incomplete)

**Evidence:** `panic: gofrontend: synthetic <pkg> type error: ... [recovered, repanicked]`.

**Fix:** Complete the synthetic packages' exported API to match what real
stdlib code uses. These are synthetic-implementation bugs. (Long-term: the
real packages should compile via disk importer, retiring the synthetics.)

### 5. `variable "form" used before declaration` (frontend ordering bug)
**Blocks (5 packages):** `net/http/internal/httpcommon`,
`vendor/golang.org/x/net/http/httpguts`, `vendor/golang.org/x/net/idna`,
`vendor/golang.org/x/net/internal/httpcommon`,
`vendor/golang.org/x/text/unicode/norm`.

**Evidence:** `gofrontend: variable "form" used before declaration`.

**Fix:** Frontend declaration-ordering bug — a variable is referenced before
its declaration is processed. General fix in the exporter's ordering logic.

## Notes

- **Asm stubs (17 packages):** `internal/cpu`, `internal/runtime/atomic`,
  `crypto/internal/fips140/*`, etc. fail on assembly-only functions. This
  needs the deferred asm workstream, not a frontend fix.
- **`internal/*` test-only packages:** Many `internal/*` failures are for
  test helpers (`internal/testenv`, `internal/nettest`) or coverage tools.
  These are lower priority than user-facing packages.
- **The `go/*` toolchain** (`go/parser`, `go/types`, etc.) is blocked
  primarily by #1 (`path/filepath`). Fixing #1 unblocks the entire chain.
- **Real `fmt`/`reflect` lanes:** The `fmt` and `reflect` packages themselves
  compile OK. The `undefined: fmt.Fprintf` failures are for *consumers* of
  the synthetic fmt; the real-fmt2 lane (making real `fmt` work) will
  resolve these.

## Package status (full list)

See `/tmp/stdlib_results.jsonl` for machine-readable results.
160 OK, 223 FAIL. Key OK packages: `fmt`, `reflect`, `os`, `sync`,
`encoding/binary`, `bytes`, `strings`, `strconv`, `unicode/utf8`, `io`,
`bufio`, `context`, `errors`, `sort`, `math`, `crypto`, `crypto/aes`,
`crypto/cipher`, `crypto/ecdh`, `container/*`, `hash/*`.
