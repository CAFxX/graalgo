# M9 Independent Review — Generality Findings

**Reviewer:** generality lane (did not write any M9 code)
**Scope:** `git diff 671ac3b..m9` (297 commits) — every lane since M8b
**Date:** 2026-10-13
**Focus:** Carlo's binding directive — every fix must be GENERAL, never module-specific.

## Method

Audited each lane's diff for hardcoded package paths, type names, function names,
and magic constants. For each fix asked: "would this work for a DIFFERENT module
with the same bug class?" Grepped the Java and Go diffs for string literals,
`.equals()`/`==` on names, and package-path conditionals. Verified the vendored
`go/` tree has zero changes in the m9 diff (byte-identical gate holds).

## Findings

### F-1 (Medium): `isMultiResultSynthetic` hardcodes fips140/ecdsa `Point` methods

**Location:** `phases/phase1/frontend/synth_machinery.go`, in `isMultiResultSynthetic`
(added P3-M9).

**Code:**
```go
if p == "crypto/internal/fips140/ecdsa" {
    if sig.Recv() != nil {
        rt := sig.Recv().Type()
        if nt, ok := rt.(*types.Named); ok && nt.Obj().Name() == "Point" {
            switch fn.Name() {
            case "SetBytes", "BytesX", "ScalarMult", "ScalarBaseMult":
                return false
            }
        }
    }
}
```

**Why it's module-specific:** Hardcodes one package path, one type name, and four
method names. The general principle — "interface receivers are not concrete
synthetic methods" — is implemented immediately below via
`rt.Underlying().(*types.Interface)`, but this carve-out bypasses it for one
specific generic type. A different generic interface with multi-result methods
in any other package would not get this treatment.

**General fix:** Extend the interface-receiver check to cover generic named types
whose underlying is an interface (and type parameters constrained by such),
instead of naming `Point`. If go/types reports the receiver in a way the general
check cannot see, document exactly why and check for "generic interface receiver"
structurally (e.g. `*types.Named` with type params whose underlying is
`*types.Interface`), not by name.

### F-2 (Medium): `fips140cache.Cache.Get` hardcoded desugar plan

**Location:** `phases/phase1/frontend/` multi-result desugar (added P3-M9).

**Code:**
```go
// P3-M9 (WS D): fips140cache.Cache.Get — the synthetic cache
// is a no-memoization stub, so inline the new() call directly.
// This avoids generic helper instantiation complexity.
if f.Pkg().Path() == "crypto/internal/fips140cache" && f.Name() == "Get" {
    // ... checks receiver is *types.Named "Cache" ...
    return &mrPlan{kind: mrCacheGet, pairFields: []string{"v", "err"}}
}
```

**Why it's module-specific:** Hardcodes package + function + type names. The
comment openly states the motive: "avoids generic helper instantiation
complexity" — i.e. working around a desugar limitation for one call site
instead of fixing the desugar to handle generic helpers. Any other generic
memoization helper would need its own carve-out.

**General fix:** Implement generic helper instantiation in the multi-result
desugar (or a general "inline constructor call" plan keyed by the call's
structural shape — zero-arg constructor returning (T, error) — not by
package/type/method names).

### F-3 (Low): `TlsRuntime.parseCipherSuite`/`parseVersion` silent wrong defaults

**Location:** `phases/phase3/src/main/java/graalgo/p3tls/TlsRuntime.java`.

**Code:** `parseCipherSuite` maps only 3 TLS 1.3 suite names via
`.contains()` and silently returns `0x1301` for anything else (including all
TLS 1.2 suites). `parseVersion` silently returns `0x0304` (TLS 1.3) for
unknown protocol strings.

**Why it matters:** Not module-specific (works for any TLS user), but not
general-correct either: a TLS 1.2 handshake negotiating
`TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256` would report suite `0x1301` to Go code
reading `ConnectionState` — a wrong value, silently. The `.contains()` matching
is also fragile (a suite name containing another as substring would mis-map).

**General fix:** Complete IANA cipher-suite mapping (or at minimum the TLS 1.2
suites the JDK negotiates), and return a distinct "unknown" sentinel (or -1)
instead of a plausible-but-wrong value. Same for `parseVersion`.

## Verified general (non-findings)

Each of these was checked against the "different module, same bug class" test:

1. **hexdata compact encoding** (`export.go`, `NodeFactory.java`): triggers on
   literal shape (unkeyed composite literal, >100 byte-sized int elements), not
   on package/type names. Java decodes to `UINT8` consts then runs the normal
   `maybeConvert` path. General.
2. **TLS PENDING buffer** (`TlsRuntime.unwrap`): buffers unconsumed SSLEngine
   input per engine handle, runs delegated tasks internally. Textbook-correct
   SSLEngine driving; keyed by handle, not by test or message type. General.
3. **bug-003** (`Fmt.java`): checks `instanceof GoErrorValue` — the general
   error-value representation, not `PanicNilError` specifically. Matches gc's
   `handleMethods` error dispatch. General.
4. **bug-028** (`NodeFactory.convertMapKey`): converts via `maybeConvert`
   against the map's key type from `underlyingEntry` (handles named map types).
   No-op when kinds agree. General.
5. **bug-030** (assignment re-read): implements Go's assignment evaluation-order
   semantics for all index/selector/deref assignments. General.
6. **bug-019** (`BinaryOpNode.interfaceEqual` + `GoTypeDescriptor.isComparable`):
   implements the spec rule — panic iff identical uncomparable dynamic types.
   Comparability computed structurally (func/slice/map → false; struct/array →
   recurse). General.
7. **bug-005** (`ConvertNode`): routes `[]rune(string)` through
   `RangeStringNode.decodeRune` (the same decoder range-over-string uses),
   matching `utf8.DecodeRuneInString` semantics. General.
8. **bug-010** (`BinaryOpNode.executeDouble`): float32 ops route through the
   correctly-rounded float path. General.
9. **bug-016/018/022** (fmt): `GoErrorValue` type-based dispatch, dynamic-kind
   `%p` handling, structural anonymous-struct rendering from wire fields.
   General.
10. **sync.OnceFunc intrinsic** (`synth_machinery.go`): follows the established
    synthetic-package intrinsic pattern (same as `time`, `math`). The `sync`
    package-path check is the synthetic-package dispatch mechanism, not
    user-module special-casing. General.
11. **`crypto/internal/fips140/alias` intrinsic**: same synthetic-package
    pattern for `uintptr(unsafe.Pointer(...))` lowering. General.
12. **Vendored `go/` tree**: zero changes in `671ac3b..m9`. Upstream Go 1.27.1
    sources untouched (byte-identical gate in `build.sh check`).
13. **Synthetic crypto preserves generics**: 1420 generic function definitions
    (e.g. `[P ecdsa.Point[P]]`) in the diff — no de-genericization.
14. **Linkname registrations**: `internal/poll.runtime_*` are the real Go
    runtime linknames; `graalgo.net.*`/`graalgo.x509.*`/`graalgo.tls.*` are
    graalgo's own intrinsic namespace. General runtime surface.
15. **reflect fix (2ea556b)**: canonical descriptor lookup by qualified name.
    General.
16. **`byteLiteralHex` non-Int guard**: kind check before `Int64Val`. General
    robustness.

## Notes

- The synthetic-package registries (`isMultiResultSynthetic`'s long package
  list, `multiResultSyntheticError`'s milestone mapping) are verbose but are
  registries of the approved synthetic-stdlib approach, not per-module hacks.
  Every entry follows the same pattern. Not findings — but they are a
  maintenance surface: each new synthetic package needs a list entry.
- F-1 and F-2 are both in the fips140/ecdsa bring-up path and both work around
  generic-type handling gaps. They suggest the underlying gap — "the frontend
  doesn't handle generic interface/type-parameter receivers uniformly" — is the
  real general fix that would subsume both.

## Verdict

**3 findings (2 medium, 1 low), 16 verified-general.** No findings require
blocking M9 by themselves — F-1 and F-2 are confined to the synthetic
`crypto/internal/*` bring-up path and don't affect non-crypto modules — but
F-1 and F-2 should enter the fix queue: they are exactly the class of
special-casing the directive forbids, and the general fixes are identified
above.
