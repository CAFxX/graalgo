# Track 5 — Native compilers (rustc, Clang/LLVM, GCC) + language RFCs + wishlist mining

**Mission:** mine ahead-of-time/native compilers and language design proposals for optimization ideas transferable to Go-on-Truffle.
Phase-5 rule: every claimed optimization must beat stock gc **with PGO** on a named benchmark, or the claim is struck.
Edges: (1) descriptor-based value model; (2) user-space goroutine scheduler w/ precise signals; (3) speculative JIT + guards/deopt; (4) always-on profiler; (5) three execution modes.

Do NOT re-propose (note complementary angles only): speculative interface devirtualization cascade; profile-guided struct field reorder/pack; speculative AoS→SoA; type narrowing + bit-packing; speculative data representation (map specialization, unboxing); small string optimization; dynamic per-site string interning; inferred store-once values; idle-time repack/defrag/shrink; adaptive concurrency primitives; vectorization; always-on profiling; partial escape analysis; don't list gc basics (inlining, gc's escape analysis, bounds-check elimination).

Sibling tracks (mark overlap, keep distinct angle): T1 academic, T2 industry JVMs (+.NET), T3 dynamic runtimes (+Swift), T4 Go-adjacent (issue tracker, blogs, TinyGo).

> **Status:** wave-1 drafting — incremental writes; see `## Draft log` and `## Coverage notes` at bottom.

**Method note (depth directive 2026-10-04):** long-term systematic survey, wave 1 of an indefinite effort. Primary sources preferred (RFC text, pass docs, release notes). "Shipped/claimed" ≠ "we verified". Negative results are first-class citizens.

---

## rustc

### Rich LLVM attribute emission from Go language semantics [ABLE]

**Idea.** If graalgo's AOT backend lowers Go to LLVM IR, annotate it the way
rustc does: `nonnull`, `dereferenceable(N)`, `noundef`, `noalias`/`nocapture`,
`readonly` on pointers and calls, derived from Go type facts + escape analysis
— pointers from `new`/`make` are nonnull+dereferenceable; map/slice headers are
nonnull; EA-proven non-escaping params get `noalias`. These attributes are what
unlock LLVM's GVN/LICM/DSE/SROA on pointer-heavy code.
**Precedent (citable).** LLVM LangRef attribute semantics (primary — the
LangRef diffs document `dereferenceable` ⇒ speculative loads legal,
`nonnull`, `noalias`; see reviews.llvm.org/D61652, reviews.llvm.org/D95238);
rustc as existence proof that a frontend can feed these richly from language
semantics — Rust references carry `dereferenceable` (secondary: compiler-dev
discussion, https://news.ycombinator.com/item?id=35059511 — labeled, not
primary).
**Why gc doesn't do it.** gc has no LLVM layer at all; its backend consumes its
own internal alias info and never reifies facts as optimizer-visible
attributes. There is simply no channel for "this pointer is dereferenceable"
to reach a general-purpose optimizer in gc's pipeline.
**Architectural fit.** Edge (5): AOT-on-LLVM. Edge (1): the descriptor model's
per-site representation knowledge is exactly the feedstock for precise
attributes (a descriptor stating "this value is a non-nil map" justifies
`nonnull`+`dereferenceable` on the hmap pointer — knowledge gc's type-global
lowering cannot express).
**Benchmark sketch.** Pointer-chasing data-structure workload (ordered map /
tree insert+lookup mix): graalgo-AOT-on-LLVM with attributes vs the same
pipeline with attributes stripped vs gc+PGO. Claim dies if the attribute delta
is noise — this isolates the transfer from the backend change.
**Open risks.** Attribute soundness is UB-on-violation: every attribute needs a
proof (type fact or EA), never a hope. `unsafe.Pointer` and `reflect` can
invalidate static facts — restrict to compiler-derived facts. LLVM attribute
semantics evolve across versions (D95238 changed dereferenceable/noundef
interaction) — pin the LLVM version and its LangRef.

### Speculative niche-filling for interface values via the descriptor model [THINKING]

**Idea.** Rust packs enum discriminants into *invalid bit patterns* ("niches")
of payloads: `Option<&T>` is the size of `&T`. Go's analogs: interface values
`(tab, data)` and `(T, bool)` / `(T, error)` return pairs. Under the descriptor
model (edge 1), *speculatively* represent hot interface shapes using pointer
niches (alignment spare bits, null-data-word encodings) with a guard + deopt —
instead of the fixed 2-word iface — where profiling shows the niche holds.
**Precedent (citable).** rustc layout computation tracks `largest_niche` per
type (rustc-dev-guide layout dump shows `valid_range` + `largest_niche`;
https://rustc-dev-guide.rust-lang.org/mir/optimizations.html is the MIR page —
layout dump example via the dev-guide mirror,
https://github.com/llvm-z80/rust-z80/blob/HEAD/src/doc/rustc-dev-guide/src/compiler-debugging.md);
Rust Reference "validrange" (`rustc_layout_scalar_valid_range_*`); RFC 3204
extends niches to references — accepted, implemented behind
`-Z reference-niches` (rust-lang/rust#113166), tracked via
https://github.com/rust-lang/rfcs/pull/3204.
**Why gc doesn't do it.** Go's nil-able reference types (pointer, slice, map,
chan, func) already get the null-pointer optimization by language design; gc
has no invalid-bit-pattern analysis for anything else, and the interface
representation is fixed at two words, type-globally.
**Architectural fit.** Edge (1): representation ≠ semantics, per allocation
site — the exact precondition niche-filling needs. Edge (3): Go's typed-nil
semantics make *static* niche-filling unsound (a nil data word with non-nil tab
is a non-nil interface), so speculation with guard+deopt is mandatory, not
optional. Complementary angle to backlog "type narrowing + bit-packing": that
item covers observed *value ranges*; niches cover *invalid patterns* — same
family, distinct mechanism.
**Benchmark sketch.** Error-return-heavy call chains (nil-error fast path):
branch-miss reduction on the nil check; plus footprint on interface-heavy
slices. Prototype-gated: mark speculative until a prototype shows >noise.
**Open risks.** Guard cost may eat the win; complexity vs payoff unclear; the
niche must be re-validated on every deopt — needs the always-on profiler's
representation-stability signal (edge 4).

### JIT-time generic specialization (monomorphization per observed instantiation) [THINKING]

**Idea.** rustc monomorphizes every generic instantiation into fully optimized,
type-specific code. gc compiles generics via gcshape stenciling + runtime
dictionaries (shared code, type params passed as dictionaries). In JIT mode,
specialize *hot* generic instantiations per concrete type observed at the call
site — guard + deopt — eliminating dictionary indirection and unlocking
per-type inlining and constant propagation.
**Precedent (citable).** rustc monomorphization (standard rustc behavior;
rustc-dev-guide codegen docs); language-design precedent: Rust RFC 2000
(const generics, rust-lang/rfcs#2000,
https://github.com/rust-lang/rfcs/blob/master/text/2000-const-generics.md)
shows specialization as an accepted language commitment.
See also track 2 (.NET generics re-JIT per instantiation) and track 3 (Swift
generic specialization) — sibling angles, this entry keeps the rustc/AOT
contrast.
**Why gc doesn't do it.** gcshape stenciling is a deliberate compile-time and
code-size tradeoff; gc never specializes beyond shapes, and every type-param
operation pays dictionary indirection.
**Architectural fit.** Edge (3): speculative JIT is the natural home —
specialize on observed types, deopt when a new type appears. Edge (4): the
profiler picks which instantiations are hot enough to justify specialized code.
**Benchmark sketch.** Generic numeric kernels (generic `Min`/`SortFunc` over
`int64` in a tight loop; hot methods of a generic container): specialized JIT
vs gc+PGO. Claim dies if ≤ noise.
**Open risks.** Code bloat from overspecialization — cap by hotness (inliner
budget discipline); dictionary-passing ABI at generic call boundaries needs a
versioning story; megamorphic generic call sites must NOT specialize (guard
with a type-count cap).

### rustc MIR-level opts → surveyed, nothing applicable (beyond gc)

rustc runs optimizations at MIR because MIR sees Rust semantics (drops,
borrows, panics) that LLVM IR cannot. The Go analog of "the semantics-aware
mid-level" is gc's own SSA pipeline — writebarrier expansion, nilcheckelim,
prove.go, checkbce are all Go-aware passes. There is no foreign-IR gap to
bridge. The transferable lesson is organizational (keep a semantics-aware
mid-level IR), not an optimization. One line for the negative list.

## Clang/LLVM

### Pass-by-pass classification vs gc (LLVM 16 transform passes)

Primary sources: LLVM 16 pass list (https://releases.llvm.org/16.0.0/docs/Passes.html);
gc 1.27.1 SSA pass list (`~/tools/go/src/cmd/compile/internal/ssa/compile.go`, `passes` var — [VERIFIED] local source).
Legend: ✓ gc has an equivalent · ✗ gc has none · ~ partial/rough analog · N/A not applicable to Go.

| LLVM pass | gc status | note |
|---|---|---|
| -adce/-dce/-die | ✓ deadcode | |
| -always-inline | ✓ inliner (mid-stack, better) | excluded from survey per brief |
| -argpromotion | ✗ | Go ABIInternal already passes small aggregates in registers; no by-ref ABI penalty to fix → negative (see below) |
| -bb-vectorize | ✗ | removed from LLVM itself — see ATTEMPTED |
| -block-placement (PGO) | ✗ | → candidate |
| -break-crit-edges | ✓ critical | |
| -codegenprepare | ~ lower/lateLower | |
| -constmerge | ✗ | dup read-only constants not merged by cmd/link; minor |
| -deadargelim | ✗ | no interprocedural dead-arg elim |
| -dse | ✓ dse | |
| -function-attrs | ✗ | → candidate (purity deduction) |
| -globaldce | ~ linker strips unreached symbols only | |
| -globalopt | ✗ | |
| -gvn | ✓ cse | |
| -indvars (canonicalize IVs) | ✗ | → candidate (loop suite) |
| -inline | ✓ | excluded per brief |
| -instcombine/-aggressive-instcombine | ~ opt (rewrite rules; narrower pattern set) | |
| -internalize | ✗ | per-package compilation; closed-world AOT could do it |
| -ipsccp | ✗ | → candidate (with GCC ipa-cp) |
| -jump-threading | ✗ | gc has branchelim only |
| -licm | ✓ licm | gc HAS licm.go — do not claim otherwise |
| -loop-deletion | ~ deadcode covers some | |
| -loop-extract(-single) | N/A | not a perf opt for us |
| -loop-reduce (strength reduction) | ✗ | → candidate (loop suite) |
| -loop-rotate | ✓ loopRotate | |
| -loop-unroll | ✗ | gc deliberately does not unroll → candidate/negative (see below) |
| -loop-unroll-and-jam | ✗ | → candidate (loop suite) |
| -loop-unswitch | ✗ | → candidate (loop suite) |
| -mem2reg | N/A | gc is SSA-native |
| -memcpyopt | ~ | gc handles memmove/memclr intrinsics; no general memcpy forwarding |
| -mergefunc | ✗ | → candidate (ICF, with GCC ipa-icf) |
| -mergereturn | ~ layout/schedule | |
| -partial-inliner | ✗ | gc inlines whole functions only |
| -reassociate | ✗ | gc opt has limited reassociation |
| -sroa | ✗ | gc has decompose (ABI) only, no SSA SROA |
| -sccp | ✓ sccp | |
| -simplifycfg | ~ shortcircuit/fuse/trim | |
| -sink (code sinking) | ✗ | complementary angle for backlog PEA (Graal sinks; gc can't) |
| -tailcallelim | ✗ deliberate | Go traceback/defer semantics forbid silent TCO → negative |
| -strip* | N/A | |
| -reg2mem | N/A | anti-optimization |

PGO facts [VERIFIED] from gc source: gc's PGO feeds exactly two consumers —
inlining budgets (`inlineHotMaxBudget`, `cmd/compile/internal/inline/inl.go`) and
interface devirtualization (`cmd/compile/internal/devirtualize/pgo.go`). No PGO
block layout, no PGO unrolling, no value profiling. Everything below marked "gc
lacks PGO-X" rests on that grep, not on memory.

### PGO-driven code layout: hot/cold splitting, block placement, function reordering [ABLE]

**Idea.** In AOT mode, use the always-on profiler's block/function hotness to (1)
split cold blocks out of hot functions (hot/cold partitioning), (2) lay out basic
blocks by profile (block-placement), (3) order functions hot→cold
(-freorder-functions style). gc emits one text section per function in
compile order; PGO never touches layout.
**Precedent (citable).** LLVM `-block-placement`: "Profile Guided Basic Block
Placement" (https://releases.llvm.org/16.0.0/docs/Passes.html); GCC `-O2`
enables `-freorder-blocks-and-partition -freorder-functions`
(https://gcc.gnu.org/onlinedocs/gcc-16.2.0/gcc/Optimize-Options.html);
`-fbasic-block-sections` (basic-block-level function splitting) ships in Clang.
**Why gc doesn't do it.** gc's backend has `likelyadjust` (static heuristics) and
`layout` passes but no profile input to either; PGO stops at the inliner.
**Architectural fit.** Edge (4): the always-on profiler gives block hotness for
free — LLVM/GCC need a separate instrumented training run; we get continuous
profiles. Edge (5): applies to pure-AOT and hybrid image builds.
**Benchmark sketch.** i-cache-sensitive large binary: build `cmd/compile` with
graalgo-AOT vs `go build`, measure wall time of a fixed compile workload and
`perf stat` i-cache miss rate; plus a branchy server benchmark (e.g.
`net/http` hello-world throughput) for block-placement effects.
**Open risks.** Profile staleness across runs (mitigate: re-profile continuously,
re-layout at rebuild); cold-start code bloat from splitting; must beat gc+PGO,
not gc-without-PGO — layout is the one PGO dimension gc leaves entirely on the
table, so the comparison is clean.

### Post-link code re-layout, Propeller/BOLT style [ABLE]

**Idea.** After linking the AOT binary, re-optimize function/basic-block order
using perf-counter profiles (LBR) — a post-link pass independent of the
compiler's own layout. Complements the compile-time candidate above; catches
cases the compiler's function-at-a-time view misses.
**Precedent (citable).** LLVM Propeller (post-link optimizer using profiles;
ships in LLVM, used in production at Google/Meta — docs pending fetch, see
coverage notes); Facebook BOLT (post-link binary optimizer, open source).
**Why gc doesn't do it.** No post-link step exists in the Go toolchain;
cmd/link emits final layout.
**Architectural fit.** Edge (5) pure-AOT; edge (4) supplies the profiles without
a training run. Closed-world AOT binaries are ideal BOLT inputs (static,
self-contained).
**Benchmark sketch.** Same as above: `cmd/compile` build workload + large
server binary; metric: instructions-per-cycle and i-TLB misses via `perf stat`.
**Open risks.** Toolchain complexity (needs perf/LBR access at build or deploy
time); benefit concentrates in >10 MB text binaries — small Go CLIs may see
nothing. Wave-2: verify Propeller's current LLVM docs URL.

### Speculative loop multi-versioning with runtime alias/bounds checks [ABLE]

**Idea.** When the vectorizer (backlog) can't prove no-aliasing or exact trip
counts, emit TWO loop versions — vector + scalar — guarded by a cheap runtime
check (pointer-disjointness, alignment, trip count), instead of giving up.
On guard failure, run scalar; no deopt needed (both versions are valid code).
**Precedent (citable).** LLVM LoopVectorize's runtime-check multi-versioning
("runtime memory checks" when aliasing can't be disproven at compile time —
standard vectorizer behavior, docs pending fetch in wave 2). This is *shipped*
mainstream compiler behavior, not exotic.
**Why gc doesn't do it.** gc has no loop vectorizer at all (backlog), and its
versioning story is absent; `prove.go`/`checkbce.go` eliminate checks rather
than versioning on them.
**Architectural fit.** Edge (3): in JIT mode the guard can deopt-and-recompile
instead of keeping both versions; edge (4): the profiler tells us which loops
are hot enough to justify the code-size cost of two versions. This is the
*mechanism* complement to the backlog's vectorization item — vectorization says
*what*, this says *how to fire it on Go's aliasing-hostile loops*.
**Benchmark sketch.** `bench_simd` shapes from T6 §3 (saxpy, dot product over
`[]float64` with unknown aliasing — the exact case static analysis surrenders
on): graalgo-JIT vs gc; claim dies if ≤1.2× gc.
**Open risks.** Code-size blowup if applied indiscriminately (gate on profile
hotness); the runtime check itself must be cheaper than the vector win on the
trip counts Go actually sees (short slices are common — measure, don't assume).

### Loop-nest suite gc lacks: unswitching, unroll-and-jam, strength reduction, peeling [ABLE]

**Idea.** Four LLVM/GCC loop transforms with no gc equivalent: `-loop-unswitch`
(hoist loop-invariant branches out of the loop), `-loop-unroll-and-jam`,
`-indvars`/`-loop-reduce` (strength reduction of indexed addressing), `-fpeel-loops`.
gc's loop repertoire is rotation + limited BCE + licm.
**Precedent (citable).** LLVM pass list, ibid.; GCC `-O3` enables
`-fpeel-loops -funswitch-loops -floop-unroll-and-jam -floop-interchange`
(https://gcc.gnu.org/onlinedocs/gcc-16.2.0/gcc/Optimize-Options.html).
**Why gc doesn't do it.** Deliberate compile-time/simplicity tradeoff; gc's
`loopbce.go`/`prove.go` attack bounds checks, not loop shape. gc has no
unroller at all (verified: no unroll pass in `passes`).
**Architectural fit.** Edge (3): unswitching/unrolling decisions need trip-count
knowledge — the JIT observes actual trip counts (edge 4) and can version or
deopt; static compilers must guess. This is where speculative JIT beats
`-O3`-style static heuristics structurally.
**Benchmark sketch.** Loop-invariant-branch microbenchmark (e.g. JSON-number
parse loop with a per-iteration mode check — `encoding/json` decode of a
homogeneous array); indexed-addressing reduction over `[]struct`. Claim dies if
no win vs gc+PGO on at least one named shape.
**Open risks.** Unrolling without trip-count evidence bloats i-cache (the
classic `-O3` regret); gate strictly on observed trip counts. Unswitching
duplicates loop bodies — code-size guard needed.

### Machine outlining for code size (-moutline style) [ABLE]

**Idea.** Dedupe repeated machine-instruction sequences *across functions*
into shared outlined functions (called, not inlined): shrinks text, improves
i-cache density. Go binaries are a prime target — gc emits verbose repeated
sequences (prologues, bounds-check idioms, write-barrier sequences) per
function with no cross-function machine-level dedup.
**Precedent (citable).** LLVM MachineOutliner, shipped in LLVM; enabled by
default under `-Oz` for AArch64 (reviews.llvm.org/D45916); original
implementation review D26872 (reviews.llvm.org/D26872).
**Why gc doesn't do it.** gc's backend has no cross-function machine-level
dedup pass; identical sequences are emitted per function. (Distinct from ICF
above: outlining works at the *instruction-sequence* level within function
bodies, ICF at the *whole-function* level.)
**Architectural fit.** Edge (5) AOT; edge (4): profile-guided outlining —
outline cold/repeated sequences, keep hot sequences inline to avoid call/ret
on hot paths.
**Benchmark sketch.** Text size + `perf stat` i-cache miss rate on a large Go
binary (e.g. `cmd/compile` itself) and startup page-fault count:
graalgo-AOT with outlining vs `go build`. Claim is primarily size/i-cache, not
peak throughput — say so up front.
**Open risks.** Outlining on a hot path is a pessimization — the hotness gate
is load-bearing; interacts with PGO block layout (outline after layout, or
re-verify).

### Code sinking (-sink) → complementary angle for backlog PEA

LLVM's `-sink` moves computations into the branches that use them (the IR-level
mechanism behind "sink allocations into cold branches"). gc has no sinking pass
(verified in pass list). **Not a new candidate**: the backlog's partial escape
analysis already owns sinking; this is a note that LLVM ships the *scalar*
version of the transform and Graal does the allocation version — our PEA should
implement both, and the `-sink` cost model (sink only if it doesn't duplicate
into multiple hot successors) is directly reusable. See backlog PEA item.

## GCC

Primary: GCC Internals, "Regular IPA passes"
(https://gcc.gnu.org/onlinedocs/gccint/Regular-IPA-passes.html); GCC 16.2
Optimize Options (https://gcc.gnu.org/onlinedocs/gcc-16.2.0/gcc/Optimize-Options.html).

### Interprocedural const-arg specialization / function cloning (ipa-cp) [ABLE]

**Idea.** When whole-program (or profile) evidence shows a function is always
called with the same constant argument values, clone it and specialize the
clone: fold the constants, delete dead branches, shrink the body. GCC does this
at `-O3` (`-fipa-cp-clone`); LLVM's `-ipsccp` propagates but doesn't clone.
**Precedent (citable).** GCC Internals: ipa-cp "can also do partial
specialization" for functions "always invoked with some arguments with the same
known constant values" (ibid.); `-fipa-cp-clone` enabled at `-O3` (Optimize
Options, ibid.).
**Why gc doesn't do it.** gc has no interprocedural constant propagation at all
(verified: no ipsccp in SSA passes) and no function cloning/specialization —
inlining is its only cross-call optimization. PGO never feeds value profiles.
**Architectural fit.** Edge (4): the always-on profiler records argument-value
distributions per call site (GCC needs `-fprofile-values` training runs; we get
value profiles free). Edge (3): in JIT mode specialize speculatively with a
guard on the argument value + deopt; in AOT mode specialize only
provably-constant call sites (closed world). This is disjoint from the backlog:
"speculative data representation" specializes *data*; this specializes *code*
on observed argument values.
**Benchmark sketch.** Templating/formatting hot paths with effectively-constant
arguments (e.g. `text/template` execution with a fixed template, or
`fmt.Fprintf` with a constant format string in a loop): specialized clone vs
gc+PGO. Claim dies if no measurable win on a named constant-arg hot call.
**Open risks.** Clone explosion (cap clones per function; GCC uses the same
discipline); value-profile staleness — guard, don't assume, in JIT mode.

### Whole-program identical code folding (ipa-icf / mergefunc) [ABLE]

**Idea.** After whole-program analysis, fold functions (and read-only data)
with exactly identical semantics into one copy. Saves text size and i-cache.
**Precedent (citable).** GCC Internals: ipa-icf discovers "functions and
read-only variables that have exactly the same semantics" (ibid.);
`-fipa-icf` at `-O2`; LLVM `-mergefunc` ("Merge Functions",
https://releases.llvm.org/16.0.0/docs/Passes.html).
**Why gc doesn't do it.** Verified: no ICF/mergefunc anywhere in
`cmd/compile` or `cmd/link` (grep for `icf|identical.*fold` in cmd/link: empty).
Go generics *increase* the duplication surface: gc's gcshape stenciling emits
one instantiation per shape, and identical shapes across packages are never
folded.
**Architectural fit.** Edge (5): pure-AOT closed-world build is the natural
place — the whole program is visible at image-build time, exactly the
condition ICF needs. (Also applicable to hybrid images.)
**Benchmark sketch.** Binary size + i-cache misses on a large generics-heavy
binary (e.g. a program instantiating the same generic containers/algorithms
across many packages): graalgo-AOT with ICF vs `go build`. Metric: text size
and `perf stat` i-cache miss rate on a fixed workload.
**Open risks.** Folding interacts with function-identity-sensitive code
(`reflect.ValueOf(f).Pointer()`, function equality) — Go spec says func values
are only comparable to nil, but `reflect` exposes PCs; must exclude
identity-observed functions (GCC's ipa-icf has the same caveat class — the
kernel-livepatch deck cited "problem for stack unwinder"). Modest throughput
expectation: this is primarily a size/i-cache play.

### Closed-world whole-program devirtualization for AOT (ipa-devirt style) [ABLE]

**Idea.** In closed-world AOT mode, run class-hierarchy-style analysis over the
whole program at image-build time: interface call sites with a single possible
receiver become direct calls — no guards, no profiling, no deopt, because the
world is closed. Where >1 receiver exists, keep the dispatch (or fall back to
the backlog's speculative cascade in hybrid/JIT mode).
**Precedent (citable).** GCC Internals: ipa-devirt "performs speculative
devirtualization based on the type inheritance graph. When a polymorphic call
has only one likely target in the unit, it is turned into a speculative call"
(ibid.); GCC also ships `-fdevirtualize -fdevirtualize-speculatively` at `-O2`.
**Why gc doesn't do it.** gc compiles package-at-a-time and never does
whole-program analysis; its devirtualization is per-call-site static proof or
frozen PGO profile (`devirtualize/pgo.go`). It cannot know "this interface has
exactly one implementation in the final binary."
**Architectural fit.** Edge (5): this is the AOT-mode sibling of the backlog's
*speculative* devirtualization cascade (JIT/hybrid). Complementary angle, not a
duplicate: static whole-program proof vs speculative guards. The two compose —
AOT proves the monomorphic sites, the JIT speculates on the rest.
**Benchmark sketch.** Interface-dispatch microbenchmark with a closed set of
implementations (e.g. `io.Writer` with a single concrete writer linked in):
graalgo-AOT vs gc+PGO. Claim dies if AOT devirt ≤ gc+PGO devirt.
**Open risks.** Closed-world soundness for `reflect`-constructed types and
plugins (same cliff as T6 §9.4.1 — the analysis must see reflect-reachable
implementations or conservatively bail); build-time cost of whole-program
analysis on huge modules.

### Interprocedural purity / readonly deduction (ipa-pure-const, function-attrs) [ABLE]

**Idea.** Whole-program deduction of which functions are pure/const (no memory
writes, no reads of mutable globals) and which pointer params are readonly;
feeds DCE of calls, LICM hoisting of calls, and store elimination.
**Precedent (citable).** GCC Internals ipa-pure-const (regular IPA pass list,
ibid.); LLVM `-function-attrs`: "Deduce function attributes"
(https://releases.llvm.org/16.0.0/docs/Passes.html).
**Why gc doesn't do it.** gc has no interprocedural attribute deduction
(verified in pass list); purity knowledge is limited to compiler intrinsics and
builtins. A pure helper called in a loop cannot be hoisted by gc's licm.
**Architectural fit.** Edge (5) AOT closed world; in JIT mode, purity can be
*speculative* (guard on "no store observed" is hard — more honestly: derive
from closed-world analysis of the compiled method set, which the JIT also has).
Small, composable, low-risk.
**Benchmark sketch.** Loop calling a pure helper (e.g. a small pure hash or
`math` function) where hoisting/DCE across the call matters: graalgo vs
gc+PGO. Expect single-digit percent — honesty: this is an enabler for the
loop suite, not a headline.
**Open risks.** Go's memory model + write barriers: a "pure" function may still
trigger write barriers on stack growth... precisely: purity must be defined
modulo runtime-internal effects. Keep the bar high to stay sound.

### Graphite (polyhedral loop optimization) [ATTEMPTED]

**Idea (what it was).** GCC's Graphite framework used polyhedral analysis to do
loop interchange, blocking/tiling, and other nest restructurings beyond what
the classic loop passes attempt.
**Precedent (citable).** GCC docs: `-floop-interchange` (still ships at `-O3`);
the Graphite framework itself (`-fgraphite-identity` etc.) — wave-2 to pin the
exact maintenance-status citation; widely reported as effectively unmaintained
for general code.
**Why it stalled (the valuable part).** Polyhedral optimization needs precise
dependence analysis on affine loop bounds; real C/C++ loops are rarely cleanly
affine, profitability heuristics were brittle, and compile-time cost was high
for single-digit wins on code that wasn't already hand-tuned. The obstacle was
*static legibility of loop bounds*, not the math.
**Does our architecture route around it?** Partially — edge (3)+(4): a
speculative JIT observes *actual* trip counts, bounds, and dependence behavior
at run time, which is exactly the information Graphite couldn't get
statically. But the honest caveat: if the loop isn't hot enough to justify
polyhedral compile time in a JIT, the economics still fail. Verdict: do NOT
resurrect Graphite-as-a-framework; DO treat "runtime-known bounds enable
dependence-proof loop restructuring" as a JIT-mode-only technique with a hard
hotness gate. The failure reason transfers as a design constraint, not a veto.
**Benchmark sketch.** Only if ever attempted: dense-matrix nest with
runtime-known square bounds vs gc+PGO and vs hand-tiled code.
**Open risks.** High complexity, historically low payoff — this stays a
cautionary entry unless a specific hot nest shape demands it.

## Language RFCs / evolution proposals

### Move/copy elision for provably-dead sources (SE-0390-inspired) [THINKING]

**Idea.** Swift made move-only types a language feature (SE-0390: `~Copyable`);
C++ has move semantics. Go copies on assignment, always. Transfer as a
*runtime-level* optimization, no language change: when liveness analysis
proves — or profiling + a guard observes — that a copied value's source is
dead after the copy, convert copy→move. For heap-allocated values that means
pointer swizzling instead of memcpy; for `copy(dst, src)` and append-growth,
eliding the memmove when the source is uniquely referenced and dead.
**Precedent (citable).** SE-0390 "Noncopyable structs and enums", accepted,
implemented Swift 5.9
(https://github.com/swiftlang/swift-evolution/blob/main/proposals/0390-noncopyable-structs-and-enums.md);
SE-0377 ownership modifiers as the companion contract
(https://github.com/swiftlang/swift-evolution/blob/HEAD/proposals/0377-parameter-ownership-modifiers.md).
**Why gc doesn't do it.** Go has no move semantics in the language; gc emits
memmove for struct copies and `copy()` always copies. Liveness-driven
copy→move conversion exists nowhere in the pipeline.
**Architectural fit.** Edge (1): in the descriptor model a "move" is just a
representation change — the model already knows value lifetimes. Edge (3):
speculative variant — guard on uniqueness (no other live reference to the
source, checked via the descriptor's reference tracking), deopt to a copy on
violation. This is the *constructive* twin of the backlog's "idle-time repack":
instead of repacking later, never copy in the first place.
**Benchmark sketch.** Append-growth loops over large element types; `copy(dst,
src)` where src dies; large struct by-value returns in a hot loop: graalgo-JIT
vs gc+PGO. Claim dies if the win is within noise — Go style discourages huge
by-value structs, so the honest target is the `append`/`copy` builtins.
**Open risks.** Write barriers: moving pointer-containing values still needs
barriers unless the source slot is dead *and* cleared — the proof bar is high.
`unsafe`/`reflect` aliasing defeats static proof → the speculative path is the
realistic one. Payoff concentrated in builtins; general copy elision may be
thin.

### Placement-new saga: RFC 469 box_syntax / RFC 809 / RFC 1228 [ATTEMPTED]

**Idea (what it was).** Surface syntax for constructing a value directly in
its final memory location (placement new), avoiding temporary + move:
`box_syntax` (RFC 469), full design RFC 809, syntax revision RFC 1228
(`PLACE_EXPR <- VALUE_EXPR`).
**Status.** `box_syntax` accepted and implemented nightly-only; the full
placement-new design never materialized; `box_patterns` is being *removed*
(rust-lang/rust#156749, May 2026 — current).
**Failure reason (the valuable part).** Syntax shipped without the semantics it
was meant to serve; it blocked better designs and became dead weight. The
obstacle was design-incompleteness, not implementation difficulty.
**Does our architecture route around it?** Yes — we get placement-new's
*benefit* (construct directly in the final representation/location) with no
language syntax at all: the descriptor model + PEA can materialize a value
directly in its final form. No Go syntax change is needed or wanted.
**Process lesson.** Never add surface syntax ahead of the optimization it
serves — recorded here so a future "we need syntax for X" proposal meets this
precedent first.

### Specialization: RFC 1210 accepted, never stabilized [ATTEMPTED]

**Status.** Accepted; the general form is known-unsound (lifetime-dependent
impl selection contradicts lifetime erasure at codegen); the
`min_specialization` restricted subset remains nightly-only with blocking
issues (rust-lang/rust#149257, #147507, #103708, #150387, #157731); tracking
issue rust-lang/rust#31844; survey at
https://github.com/jackh726/specialization-survey-and-design (secondary).
**Failure reason.** Soundness of impl selection under lifetime erasure —
choosing an impl by lifetime bounds requires lifetime knowledge at codegen
time, which erasure forbids; pinning a fully sound rule is the decade-old open
problem.
**Does our architecture route around it?** Completely. Our JIT per-call-site
specialization (rustc-section candidate) is *code versioning*, not
trait/impl resolution: no coherence, no impl selection, no lifetime-dependent
dispatch. The obstacle does not exist in our mechanism. This is also why the
candidate is framed as specialization-of-code, never as a language feature.

### JEP notes (language-design angle; T2 owns JVM-roadmap items)

- **JEP 401, Value Classes and Objects (preview, JDK 28)**
  (https://openjdk.org/jeps/401): the language gives up *identity* so the JVM
  may pick optimized encodings. See T6 §5 for the full verdict — non-actionable
  for Go (Go structs are already identity-free values; the win vs Java doesn't
  exist vs gc). The language-design angle adds nothing over T6.
- **JEP draft 8316779, Null-Restricted Value Class Types (preview)**
  (https://openjdk.org/jeps/8316779): excluding `null` from a value's type
  enables compact storage — the language-design twin of niche-filling.
  Cross-ref the rustc niche candidate. Go's nil-able types already encode null
  for free; nothing further transfers.
- **JEP draft 8261529, Universal Generics + Valhalla "Parametric JVM"**
  (https://openjdk.org/jeps/8261529; http://openjdk.org/projects/valhalla/):
  "specializing and optimizing generic class and method parameterizations at
  run time" is an explicit Valhalla feature set. This is independent
  validation of the JIT-time generic specialization candidate — a major
  platform is building the same thing. Cross-ref, not a new candidate.
- **JEP 417, Vector API** (incubator): explicit vector programming as the
  alternative to auto-vectorization. Complementary angle for the backlog
  vectorization item (explicit API vs auto) — not a new candidate.

### RFC 3204: reference niches [THINKING]

Accepted RFC extending niche-filling to reference-typed payloads, implemented
behind `-Z reference-niches` (rust-lang/rust#113166); tracked via
https://github.com/rust-lang/rfcs/pull/3204. For us: (1) the precedent that
niche support keeps expanding — if we build speculative niche-filling (rustc
candidate), reference payloads are wave 2; (2) the still-open "prefer
all-zeros niche" pre-RFC (internals.rust-lang.org, 2020) is a WOULD-LIKE
pointer: an accepted-direction discussion nobody picked up.

## WOULD-LIKE mining (cross-language wishlists)

### Profile-driven vector-plan selection (answers LLVM's vectorizer wishlist) [THINKING]

LLVM's own vectorizer roadmap (per the VPlan seminar paper summarizing Florian
Hahn's directions — secondary:
https://github.com/atalantus/seminar-llvm-vplan/raw/3ba1909d8c6780db09200f424bdd6bd84897c3d8/paper/paper.pdf):
tail-loop folding by default ("would lead to a significant performance
increase in some instances"), reviving outer-loop vectorization, merging SLP
with VPlan, and the vector-predication (VP intrinsics) roadmap
(LLVM proposal doc, via mirror:
https://github.com/amirulandalib/llvm-project/blob/HEAD/llvm/docs/Proposals/VectorPredication.md).
The structural complaint underlying all of these: LLVM picks vectorization
*plans* with a static cost model, blind to actual trip counts.
**Transfer.** Our JIT observes real trip-count distributions (edge 4) and can
select vector plans per observed distribution — or version loops per plan
(ties to the speculative multi-versioning candidate). Static compilers *wish*
for this information; we have it. Complementary angle for the backlog
vectorization item (which covers *that* we vectorize; this covers *how we
choose plans better than LLVM's static cost model*).
**Benchmark sketch.** Odd/short-trip-count loops over slices (the tail-folding
case): plan selected on observed counts vs LLVM-style static choice.
Prototype-gated.

### Value profiling without a training run (answers GCC's perennial wish) [WOULD-LIKE]

GCC's `-fprofile-values` + ipa-cp value-driven cloning needs an instrumented
training run; stale or missing profiles are the perennial complaint. Our
always-on profiler (edge 4) makes value profiles free and continuous — this
wishlist item is already folded into the ipa-cp candidate above as its
enabling argument. Listed here so wave-2 mining credits the source: GCC
bugzilla "missed-optimization" meta-bugs around value profiling.

### Open RFCs nobody picked up [WOULD-LIKE]

- Rust "prefer all-zeros niche for niche variant optimization" pre-RFC
  (internals.rust-lang.org, 2020): an accepted-direction discussion with no
  implementation. Watch item for the niche candidate.
- Rust specialization stabilization (rust-lang/rust#31844): the community's
  decade-long wish — we route around it (see ATTEMPTED), but track it: if Rust
  ever stabilizes specialization, the soundness rules they settle on may inform
  our own specialization discipline.
- Swift forums "Compiler" pitch threads and LLVM Discourse "RFC" category:
  not yet mined in wave 1 — see coverage notes.

## Surveyed, nothing applicable

Negative results are first-class: each names what was surveyed and why it
doesn't transfer. (Candidates that are complementary angles to backlog items
are noted in their sections, not here.)

1. **LLVM `-tailcallelim`**: Go's traceback/defer/panic semantics forbid silent
   tail-call optimization; gc deliberately lacks it. Nothing to transfer.
2. **LLVM `-argpromotion`**: Go's ABIInternal already passes small aggregates
   in registers; there is no by-reference ABI penalty to promote away.
3. **LLVM `-reg2mem`, `-loop-extract`**: not performance optimizations for our
   purposes (debugging/offload utilities).
4. **LLVM CFI schemes (`-fsanitize=cfi`, shadow call stacks)**: Go is
   memory-safe; CFI addresses C/C++ vtable-hijack attack classes that don't
   exist in Go. Any interface-dispatch guard idea here is subsumed by the
   backlog's devirtualization cascade.
5. **PGO *data* placement**: LLVM/GCC PGO is overwhelmingly code layout
   (functions, blocks); no transferable data-placement pass was found in
   either pipeline. The data-layout space is owned by the backlog (field
   reorder, AoS→SoA).
6. **rustc MIR-level optimization passes as a category**: gc's SSA pipeline is
   already the semantics-aware mid-level (writebarrier, nilcheckelim,
   prove.go); there is no foreign-IR gap to bridge. Organizational lesson only.
7. **SE-0377 borrowing/consuming parameters**: the performance content is
   ARC-call reduction; Go has no ARC, and the borrowed-parameter case is
   exactly what gc's escape analysis proves. Subsumed by EA (excluded per
   brief).
8. **Swift copy-on-write value semantics**: direction inverted for Go —
   `s2 := s1` *requires* aliasing visibility, so CoW would add copies, not
   remove them. Salvage: uniqueness-tracked move elision (separate candidate).
9. **JEP 401 value classes**: see T6 §5 — Go structs are already
   identity-free values; the vs-Java win doesn't exist vs gc. Non-actionable.
10. **GCC `-fipa-sra`**: Go's ABIInternal already scalarizes small aggregates
    into registers; the remaining large-struct-by-pointer cases are marginal.
    No candidate; revisit only if profiles show otherwise.
11. **LLVM `-constmerge`**: cmd/link doesn't merge duplicate read-only
    constants, but no benchmark case justifies a candidate — minor gap, noted.
12. **Biased locking / TSX lock elision**: T2's bucket; Go has no object
    headers to bias and goroutines never pin an OS thread across a wait.
    Out of track scope, noted for the record.

## Coverage notes (drives wave 2)

**Surveyed in wave 1:**
- LLVM 16 transform passes, pass-by-pass vs gc 1.27.1 SSA pass list ([VERIFIED]
  from `~/tools/go/src/cmd/compile/internal/ssa/compile.go`); gc PGO consumers
  verified by grep (inlining budgets + devirtualize only).
- GCC regular IPA passes (Internals doc) + -O2/-O3 flag lists (GCC 16.2 docs).
- rustc: niche-filling (layout dumps, Rust Reference, RFC 3204), attribute
  emission (LangRef + rustc behavior), monomorphization, MIR-opt role.
- Rust RFCs: 2000 (const generics), 1210 (specialization — accepted, unsound,
  unstabilized), 469/809/1228 (placement-new saga, box_patterns removal
  #156749), 3204 (reference niches).
- Swift evolution: SE-0377 (borrowing/consuming — negative), SE-0390
  (~Copyable — candidate), CoW stdlib design (negative with salvage).
- JDK JEPs: 401 (see T6 §5), draft 8316779 (null-restricted), draft 8261529
  (universal generics) + Valhalla Parametric JVM, 417 (Vector API, complementary).
- Wishlist: LLVM VPlan/vectorizer roadmap (tail-folding, outer-loop, SLP
  merge, VP predication), GCC value-profiling-without-training-runs, open Rust
  RFCs (all-zeros niche pre-RFC, specialization #31844).

**NOT yet surveyed (wave-2+ queue):**
- LLVM: full `-O2`/`-O3` *pipeline order* walk (not just the pass list);
  NewPM pass names vs legacy; Propeller docs URL; BOLT details; LLVM Discourse
  "RFC" category wishlist threads; `llvm/docs/Vectorizers.rst` for the
  multi-versioning citation.
- GCC: full IPA pass list beyond "regular" (early/late/small IPA passes in the
  Internals manual); graphite maintenance-status citation; gcov value-profiling
  consumers; gccgo-as-data-point (coordinate with T4 — see also track 4).
- rustc: full MIR pass list (`run_optimization_passes` order); `-Z mir-opt-level`
  semantics; codegen-backend flag deltas; rustc perf-guide claims; layout
  randomization (`-Z layout-randomization`?) status.
- Rust RFCs: full-text search of rust-lang/rfcs for accepted-but-unimplemented
  perf RFCs; rejected RFCs pre-2018; T-lang "I-" perf labels.
- Swift evolution: full proposals index filtered by performance impact; Swift
  forums "Compiler" pitch threads; SE-0427 (noncopyable generics) implications.
- JDK JEPs: JEP index sweep for optimizer-content JEPs beyond the five above
  (T2 owns JVM-roadmap items — stay on the language-design angle).
- Wishlists: rust-internals "we wish" threads (search returned nothing useful
  in wave 1); LLVM bugzilla "missed optimization" label sampling; GCC bugzilla
  missed-optimization meta-bugs.

## Draft log
- 2026-10-04 ~08:40 CEST: skeleton created.
- 2026-10-04 ~09:10 CEST: wave-1 batch A — LLVM pass-by-pass classification
  (LLVM 16 doc vs gc 1.27.1 SSA passes, verified local) + 6 LLVM candidates
  (PGO code layout, post-link layout, speculative loop multi-versioning, loop
  suite, machine outlining, sink-as-complementary-note).
- 2026-10-04 ~09:25 CEST: wave-1 batch B — GCC section: 4 candidates (ipa-cp
  cloning, ICF, closed-world devirt, purity deduction) + Graphite ATTEMPTED.
- 2026-10-04 ~09:40 CEST: wave-1 batch C — rustc section: attribute emission,
  speculative niche-filling, JIT generic specialization + MIR negative.
- 2026-10-04 ~10:00 CEST: wave-1 batch D — RFCs section (SE-0390 move elision,
  placement-new ATTEMPTED, specialization ATTEMPTED, JEP notes, RFC 3204) +
  WOULD-LIKE + 12 negatives + coverage notes.
