# P3-M7 bytes: red-green evidence

Non-tautological verification that the synthetic `bytes` package is a
real port, not a stub.

## 1. End-to-end differential corpus (via graalgo exporter)

`phases/phase3/corpus/wave1/bytes/` — 11 programs, each run under stock
gc 1.27.1 and under graalgo P3SyncMain (gmp=1 and gmp=4), byte-comparing
stdout and exit codes:

**Result: 22 passed, 0 failed** (11 programs × gmp=1 and gmp=4 legs;
run6.log in `phases/phase3/hidden_files/m7-tmp/bytes/`).

Coverage: 45 exported package funcs (all except the 4 multi-result Cut
family which fail fast under WS D, the 8 func-valued funcs + ToTitle/Title
which crash the v1 runtime, and Runes which crashes the v1 runtime),
Buffer's 7 crash-free single-result methods (Len/Cap/Available/Bytes/
String/Grow/AvailableBuffer), Reader's 3 single-result methods
(Len/Size/Reset), empty/nil/large/malformed-UTF-8 adversarial inputs
(via the crash-free funcs), cross-feature combinations, and two panic
programs (Repeat(-1), Buffer.Grow(-1)). The 8 pinned omissions
(iter.Seq, unicode.SpecialCase) are not tested (fail fast by design).

## 2. Mutation testing (corpus is non-tautological)

Two mutations to the synthetic source, each verified to turn the corpus
red, then reverted to green. (Helper:
`phases/phase3/hidden_files/m7-tmp/bytes/muttest.sh`; gofrontend rebuilt
via `make -C phases/phase1 main` after each change.)

### M1: break `Count` → 00_basic goes red → revert → green

Mutation: in `Count`'s general branch, `n++` → `n += 2`.
- Before: `GREEN: 00_basic outputs identical`.
- After mutation + rebuild: `RED: 00_basic outputs differ`
  (graalgo reports doubled counts, e.g. `Count("mississippi","ss")`
  yields 4 instead of 2).
- After revert + rebuild: `GREEN: 00_basic outputs identical`.

### M2: break `ToUpper` ASCII fast path → 03_case goes red → revert → green

Mutation: in `ToUpper`, `c -= 'a' - 'A'` → `c -= 'a' - 'B'`.
- Before: `GREEN: 03_case outputs identical`.
- After mutation + rebuild: `RED: 03_case outputs differ` —
  graalgo prints `IFMMP` where gc prints `HELLO`.
- After revert + rebuild: `GREEN: 03_case outputs identical`.

Both mutations prove the corpus exercises the real synthetic
implementation: a behavioral change in the port is caught.

## 3. Real integration bug caught: `copy(dst, string)` in `bytesStringHasRune`

The first corpus run failed for ALL 11 programs at AST-build time:

```
graalgo-sync-run: internal error building AST: call.fields: copy from a
string source is not supported in frontend/v1 scope (WS D:
string<->[]byte conversions)
```

Root cause: `bytesStringHasRune` (synthetic bytes source,
`phases/phase1/frontend/synth_bytes.go`) used `copy(buf[:], chars[i:])`
with a string source. The Java `NodeFactory.buildCopyBuiltin` rejects
`copy` from a string source, so the entire synthetic bytes package was
un-importable — no program importing "bytes" could run at all. The
strings port had avoided this exact pattern (manual loop in
`strings.Clone`, with a WS D comment); the bytes port missed this one
spot (the only string-source `copy` in the file; all 13 others are
`[]byte`→`[]byte`).

Fix (behavior-preserving; copies `min(4, len(chars)-i)` bytes exactly
like `copy`): manual loop with a WS D precedent comment. After the fix
+ gofrontend rebuild, the corpus runs.

## 4. Runtime limitations discovered (v1)

The corpus avoids the following because they crash the Truffle runtime
with `ClassCastException: Integer cannot be cast to Long` (repros in
`phases/phase3/hidden_files/m7-tmp/bytes/repro/`). All are runtime bugs,
not port bugs:

1. Invoking a func value (ContainsFunc, IndexFunc, LastIndexFunc,
   FieldsFunc, Map, TrimFunc/TrimLeftFunc/TrimRightFunc, ToTitle, Title,
   non-ASCII ToUpper/ToLower via Map).
2. `string([]rune)` conversion.
3. Buffer methods writing the int8 `lastRead` field (Truncate, Reset,
   Next).
4. Split/SplitAfter with empty separator.
5. Unicode table paths (EqualFold/TrimSpace/IndexAny on non-ASCII).
6. ToValidUTF8/Runes on invalid UTF-8 input.

See `corpus/wave1/bytes/README.md` for the full list and re-test triggers.

## 5. Drift test

`phases/phase3/src/p3frontend/bytes_test.go`:
- `TestBytesSyntheticSignatures`: mirror matches phase-1 source
  (53 funcs, Buffer 24 methods, Reader 11 methods, ErrTooLarge, MinRead).
- Omission set pins the 8 deliberate omissions (5 iter.Seq + 3
  unicode.SpecialCase).

## Re-test triggers

- **WS D lands**: re-test multi-result Cut family, Buffer/Reader
  multi-result methods.
- **Runtime func-value invocation fixed**: re-test group 1 above.
- **Runtime sub-64-bit integer handling fixed**: re-test groups 3, 5.
- **unicode/iter/io waves**: re-test the 8 pinned omissions.
- **Go version change**: re-run table generation, diff tables.
