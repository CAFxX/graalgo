# P3-M8 analysis: synthetic bring-up of std `io`

Vendored source of truth: `~/workspace/graalgo-stdlib/go1.27.1/io/` (pinned go1.27.1).
Author: M8 analysis worker. Scope: package `io` ONLY — `io/fs` and `io/ioutil`
are separate packages with separate dependency profiles (see §6); they need
their own analyses.

## 1. Source structure

Package `io` is three files, all pure Go, no build tags, no asm, no linknames:

| File | Size | Contents |
|---|---|---|
| `io.go` | 23 KB | 22 interfaces, constants, error vars, Copy/CopyN/CopyBuffer/WriteString/ReadAtLeast/ReadFull/LimitReader/LimitedReader/SectionReader/OffsetWriter/TeeReader/Discard/NopCloser/ReadAll |
| `multi.go` | 3.4 KB | eofReader, multiReader (+WriteTo), MultiReader, multiWriter, MultiWriter |
| `pipe.go` | 5.2 KB | onceError, pipe, PipeReader, PipeWriter, Pipe, ErrClosedPipe |

Imports: `io.go` → `errors` + `sync`; `multi.go` → none; `pipe.go` → `errors` + `sync`.
`sync` use is exactly: `sync.Mutex` (embedded in `onceError`; field `pipe.wrMu`),
`sync.Once` (`pipe.once`), `sync.Pool` (`blackHolePool` in `discard.ReadFrom`).

Inventory checks (P3-M6):
- **asm inventory** (`asm-inventory-go1.27.1.json`): zero matches for `io` — no `.s` files in the package (confirmed by directory listing).
- **linkname trial** (`linkname-trial-go1.27.1.json`): no linkname targets in `io`.
- **coupled-packages** (`coupled-packages-go1.27.1.json` line 2221): `io` classified **"pure"**, no reasons — no coupling blockers.
- **exclusions.yaml** v2: `io` not excluded.

Import resolution: `errors` resolves to the P3-M7 synthetic package
(`synth_errors.go`, in `syntheticSources`). `sync` resolves to the P3-M2
synthetic surface (`SyncNewNode`/`SyncOpNode`, P3SyncMain) — **but only for
Mutex, RWMutex, WaitGroup, Once** (`SYNC_DESIGN.md` lines 9–58). **`sync.Pool`
is NOT in the surface.** This is the single dependency gap (see §3, item 1).
Channels and `select` (pipe.go) are expressible: chanops_m5.go (P3-M5 WS E,
additive `makechan`/`send`/`recv`/`select` wire nodes over the Phase-2
`GoChan`/`ChanOps`/`SelectOp` primitives). `defer`, closures, `recover`
are proven in synthetic scope (synth_errors.go `errorsIsComparable`).

## 2. Full API inventory vs v1 frontend expressibility

Generics: none in `io` (moot for the P3-M3 generics note). All constructs
below are v1-expressible except the flagged items.

**Constants:** `SeekStart/SeekCurrent/SeekEnd`. **Error vars:** `ErrShortWrite`,
`ErrShortBuffer`, `EOF`, `ErrUnexpectedEOF`, `ErrNoProgress`, `ErrClosedPipe`
(public). **`Discard`** (var, `Writer`).

**Interfaces (22):** `Reader`, `Writer`, `Closer`, `Seeker`, `ReadWriter`,
`ReadCloser`, `WriteCloser`, `ReadWriteCloser`, `ReadSeeker`,
`ReadSeekCloser`, `WriteSeeker`, `ReadWriteSeeker`, `ReaderFrom`, `WriterTo`,
`ReaderAt`, `WriterAt`, `ByteReader`, `ByteScanner`, `ByteWriter`,
`RuneReader`, `RuneScanner`, `StringWriter`. Interface embedding (e.g.
`ReadCloser = Reader + Closer`) type-checks via the overlay importer;
named-method-set assertions (`r.(WriterTo)`) are proven (synth_errors.go
`unwrapper` pattern). Struct-embedded interfaces (`nopCloser{ Reader }`)
and assertion through them (`c.Reader.(WriterTo)`) need exporter
verification — flagged in §4.

**Structs:** `LimitedReader` (exported fields R, N), `SectionReader`
(unexported fields + `Size()`/`Outer()`), `OffsetWriter` (unexported
fields), `PipeReader` (embeds unexported `pipe`), `PipeWriter` (named
field `r PipeReader`).

**Multi-result members — ALL need Appendix M desugar (mrdesugar_m5.go,
mrM7-style table entries), following the encoding/* precedent (fail-fast
public bodies with exact gc signatures + single-result `__` helpers +
pair structs):**
- Package funcs (2 results): `WriteString`, `ReadAtLeast`, `ReadFull`,
  `CopyN`, `Copy`, `CopyBuffer`, `ReadAll`; `Pipe` (2 results,
  `*PipeReader, *PipeWriter`).
- 3-result members: `RuneReader.ReadRune` (`rune, int, error`) —
  the desugar is N-ary (`UnquoteChar` 4-result precedent), so this is
  supported but needs a triple carrier struct; `SectionReader.Outer`
  (`ReaderAt, int64, int64`).
- Single-result funcs needing NO desugar: `LimitReader`, `NewSectionReader`,
  `NewOffsetWriter`, `TeeReader`, `NopCloser`, `MultiReader`, `MultiWriter`.
- Methods (2 results): `Read`, `Write`, `Seek`, `ReadAt`, `WriteAt`,
  `ReadByte`, `ReadFrom`, `WriteTo`, `WriteString` (multiWriter),
  `CloseWithError`. Single-result: `UnreadByte`, `UnreadRune`, `Close`,
  `Size`.
- Special: interface methods can only be desugared at concrete call
  sites — a call through an interface value `r.Read(p)` with 2 targets
  must desugar against the *dynamic* callee. The M7 table keys on
  (pkgPath, name, recvType); interface-value call sites are a new
  desugar dimension — flagged in §4 as a design question for the M8
  implementer.

## 3. Proposed scope and fail-fast list

**Verdict: FULL package `io` (all of io.go + multi.go + pipe.go), with two
loud deviations below. No silent wrong-data stubs anywhere.**

Every member that cannot be brought up real must fail fast with a named
error and a re-test trigger, never a silent stub:

1. **`discard.ReadFrom` — `sync.Pool` gap.** `blackHolePool` (io.go:655)
   needs `sync.Pool`, which the P3-M2 synthetic surface does not provide
   (Mutex/RWMutex/WaitGroup/Once only). Two options, both loud: (a)
   extend the M2 sync surface with Pool in M8 (scope expansion, needs
   M2-owner agreement); (b) rewrite `ReadFrom` in the synthetic source
   to allocate a fresh 8KB buffer per call — **io-observable identical**
   (same Read calls, same sizes; only allocation/GC behavior differs,
   which the differential runner does not observe). Preferred: (b) with
   a comment naming the deviation, **re-test trigger: when `sync.Pool`
   lands in the synthetic surface, restore the pool form and re-run
   the io corpus.**
2. **`io/fs`, `io/ioutil` — OUT OF SCOPE for this analysis.** Separate
   packages: `io/fs` imports `internal/bytealg`, `internal/oserror`,
   `os`, `log`, `path/filepath`, `time` (none wave-2); `io/ioutil`
   imports `os`, `io/fs`, `slices`. ioutil is deprecated upstream.
   They need their own M8+ analyses; do not fold them into the io wave.
3. **M7 re-test triggers discharged by this wave** (must be executed as
   part of M8, not left parked): `bytes.Buffer.ReadFrom/WriteTo`,
   `bytes.Reader.WriteTo` (synth_bytes.go:10070,10109,10367);
   `strings.Reader.Read/ReadAt/WriteTo`, strings replacer `WriteString`
   (synth_strings.go:2215,2220,2263,2281,2312);
   `encoding/base32` NewEncoder/NewDecoder omission
   (synth_base32.go header comment + drift test); `encoding/xml`
   io.Reader/io.Writer stand-in migration (synth_xml.go:28,48–53 —
   `NewDecoder` takes the stand-in `Reader` today).

## 4. The 3 hardest behavioral risks

1. **Pipe blocking semantics under the P3 scheduler.** `pipe.read`/`write`
   are unbuffered rendezvous (`wrCh chan []byte`, `rdCh chan int`) plus a
   `done chan struct{}` closed exactly once via `sync.Once`. The hard
   cases: a `Write` blocked in `select { case p.wrCh <- b: ... case
   <-p.done: }` racing `CloseWithError` from another goroutine (partial
   `n` + close error must be returned, never lost or doubled); the
   write loop `for once := true; once || len(b) > 0; once = false`
   delivering partial writes across multiple rendezvous; `wrMu`
   serialization interacting with channel parking. The Java primitives
   (GoChan/SelectOp) are proven, but this *composition* — select-send of
   subslices, close()-wakes-blocked-select, Once-guarded close — is the
   most scheduler-sensitive code in the package. Corpus must include
   `.timing`-marked concurrent Pipe programs (gmp=1 only).
2. **`copyBuffer` dispatch + error normalization.** Three dispatch layers:
   `src.(WriterTo)` → `WriteTo(dst)`; `dst.(ReaderFrom)` → `ReadFrom(src)`;
   else the manual loop. Subtle branches: `nw < 0 || nr < nw` →
   normalize to `errInvalidWrite` (only if `ew == nil`); `nr != nw` →
   `ErrShortWrite`; EOF swallowed on success; `src.(*LimitedReader)`
   exact-concrete-type assertion shrinking the buffer. A desugar or
   assertion misfire here silently changes which code path runs —
   exactly the wrong-data class the program forbids. Every branch needs
   an instrumented differential program (counting readers/writers that
   prove which path executed).
3. **`ReadAll` growth-schedule fidelity + `ReadAtLeast` edge matrix.**
   `ReadAll`'s observable behavior is NOT just the final bytes: a
   counting Reader observes the exact Read call sizes (initial
   `b[0:512]` full-capacity read, then 256→384→576… growth with the
   1/16-capacity rollover into the chunk list). The synthetic `ReadAll`
   must replicate this schedule exactly or differential tests with
   instrumented readers fail. `ReadAtLeast` has a 4-way edge matrix
   (`min > len(buf)` → `ErrShortBuffer` before any read;
   `n ≥ min ⟺ err == nil`; EOF-after-partial → `ErrUnexpectedEOF`;
   error-after-≥min dropped) that is easy to get subtly wrong.

Design question for the M8 implementer (not a blocker for this analysis):
interface-value multi-result call sites (`n, err := r.Read(p)` where `r`
is `io.Reader`) — the mrM7 table keys on concrete (pkg, name, recv);
interface dispatch needs either a desugar rule for interface method sets
or monomorphization at the call site. Resolve before writing synth_io.go.

## 5. Dependency check: was `io` already wave-1?

**No — `io` was named in wave 1 but deferred; it is the wave-2 target.**

Evidence:
- `syntheticSources` in `phases/phase1/frontend/synth_machinery.go`
  (lines 29–71) has **no `"io"` entry** — the overlay importer cannot
  resolve `import "io"` today.
- `phases/phase3/corpus/wave1/` contains **no `io/` directory** and no
  wave-1 program imports `"io"` (grep over `corpus/wave1/**/*.go`:
  zero hits).
- The wave-1 README lists `io` in its package roster
  ("strings, bytes, strconv, unicode/utf8, path, path/filepath,
  errors, io, sort — see m7-CHECKLIST.md") — aspirational naming only.
- `phases/phase3/corpus/wave2/README.md` (this tree) names `io` as a
  wave-2 synthetic package with an `io/` corpus dir and an
  `run_wave.sh io` wrapper template — i.e. this analysis IS the
  wave-2 io bring-up input.
- M7 synthetic sources carry explicit forward triggers on io
  (see §3 item 3): nothing in wave 1 implements `io.Reader`/`io.Writer`
  semantics (xml uses structural stand-ins; strings/bytes keep
  fail-fast panics).

## 6. Differential corpus program ideas (12)

1. `00_copy_empty` — Copy from empty reader to Discard: expect (0, nil), EOF swallowed.
2. `01_copy_shortwrite` — writer returns (2, nil) for 5-byte write → ErrShortWrite; negative-n writer → errInvalidWrite path.
3. `02_copy_writerto_path` — instrumented WriterTo source: prove Copy dispatches through WriteTo (call counter), not the loop.
4. `03_copy_readerfrom_path` — Copy into Discard: prove ReadFrom path taken; error mid-stream propagates, EOF swallowed.
5. `04_readatleast_edges` — min>len(buf)→ErrShortBuffer (no read issued); partial-then-EOF→ErrUnexpectedEOF; error-after-min dropped; n≥min⟺err==nil.
6. `05_readall_growth` — counting reader asserts exact Read call sizes (512, 256, 384…) and chunk-list rollover; empty reader → empty non-nil slice.
7. `06_multireader_flatten` — nested MultiReader flattening (issue 13558/18232), mid-list non-EOF error stops with readers intact for resume.
8. `07_multiwriter_stops` — second writer short-writes → ErrShortWrite and third writer never called.
9. `08_teereader_err` — tee write error surfaces as the Read error; write completes before read returns (ordering probe).
10. `09_pipe_rendezvous` (.timing) — writer/reader goroutines, exact bytes, Close→read EOF, write-after-close→ErrClosedPipe.
11. `10_pipe_closewitherror` (.timing) — blocked reader/writer woken by CloseWithError(customErr); partial write returns (n, customErr).
12. `11_sectionreader_seek` — SeekStart/Current/End math, invalid whence/offset errors, ReadAt at/past boundary EOF, Outer() triple.

Also worth one `12_limitreader_nopcloser` combining LimitReader N-countdown,
NopCloser WriterTo-forwarding, and Discard.WriteString — cheap, keep if
the 12 above stay green.
