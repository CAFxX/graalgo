# T1 — Prior art: Go on JVM / GraalVM / Truffle and alternative Go implementations

Track T1, research for project "graalgo". Survey date: 2026-10-03. Method: web search
(`browser_search`) + page fetches (`browser_open`) of primary sources (project READMEs/repos,
paper PDFs, go.dev docs, Go source-adjacent docs). Every important claim is cross-checked
against a primary source where one was found; claims resting only on secondary sources are
labeled [SECONDARY-ONLY]; claims that could not be verified are labeled [UNVERIFIED] with the
experiment that would verify them. Negative verdicts are scoped to the observed configuration.

## 1. Go on Truffle / GraalVM: the headline finding

1. **No public "Go on Truffle" project exists.** Repeated targeted searches ("Go language
   implementation on GraalVM Truffle prototype", "TruffleGo", "Go on Truffle", "Go interpreter
   Truffle github", "Go compiler targeting JVM bytecode") returned no Truffle-based Go
   implementation — only Truffle tutorials for other languages (SimpleLanguage, EasyScript),
   a santa-lang *proposal* issue to add a Truffle backend (feature list entirely unchecked),
   and unrelated hits. Absence-of-evidence, not evidence-of-absence, but the search surface
   was broad enough to treat "nobody has published one" as a solid working assumption.
2. **The closest toy attempts at Go-on-JVM (not Truffle):**
   - `backwards-rat-race/go-on-jvm` (primary: repo page): "Experimental Compiler for Go
     targeting the JVM", README goal "to be able to self host the compiler (written in go)
     on the JVM". State: created 2020-12-31, 23 commits total, 1 star, 0 forks — a one-person
     experiment that never approached self-hosting or stdlib support.
   - `davidzwei/ugo-compiler` (primary: README): university coursework compiling a "Micro-Go"
     subset to JVM instructions via lex/yacc + the Jasmin assembler. Not a Go implementation
     in any meaningful sense (subset language, pedagogical).
   - `pdelewski/goany` (primary: repo + `docs/go-to-java-transpilation.md`): an active
     Go-source-to-multiple-backends *transpiler* (Java, C++, C#, Rust, JS). It is not a Go
     toolchain and does not run Go programs on the JVM with Go semantics — it rewrites
     source. Its Java-emitter doc is nevertheless a useful primary catalog of Go→JVM
     semantic mismatches (value vs reference semantics requiring copy constructors;
     unsigned types widened to signed; boxed generics; effectively-final closure captures
     wrapped in `Object[]`; multi-return lowered to result classes; `uint64`→`long` precision
     loss). Lesson: even a shallow transpiler needs deep per-construct lowering rules; a
     faithful Truffle interpreter would face every one of these plus goroutines, GC, and
     the runtime.
3. **Why the gap exists is itself data.** Three structural reasons recur across the surveyed
   projects: (a) Go's memory model (goroutines with growable stacks, precise GC, value
   semantics) fights the JVM object model — this is why goany's author calls Java "the most
   challenging backend"; (b) the stdlib is enormous and runtime-coupled (see §5); (c) the
   payoff story (polyglot interop, better optimizer) never justified the cost for anyone
   without a corporate sponsor — and Oracle Labs, which built every production Truffle
   language, never picked Go.

## 2. gccgo — the instructive incumbent

4. **State (primary: https://go.dev/doc/install/gccgo).** gccgo is a GCC frontend for Go,
   maintained principally by Ian Lance Taylor, sharing the C++ `gofrontend` parser/typechecker
   with gollvm and the `libgo` runtime+stdlib. As of GCC 5 its install also ships a version
   of the `go` command; the standard `go` command supports it via `go build -compiler gccgo`.
   It is still shipped inside GCC (not removed), but its Go-language currency lags gc badly:
   community reports place its supported language at ~Go 1.18 as of 2024 [SECONDARY-ONLY —
   a Hacker News comment; I could not verify the exact current gofrontend language version
   from a primary source in this track; verification = build GCC trunk's gccgo and run the
   gc test suite / check `gofrontend` commit log for post-1.18 language features].
5. **The Go team's relationship with gccgo — the lesson that matters for graalgo.**
   - The Go project *designed for* a second compiler from day one: `cmd/go` has a
     `-compiler gccgo` switch, import/export data formats (`.gox`/archive metadata) were
     kept compiler-neutral, and cgo/SWIG support gccgo (primary: go.dev blog
     "Gccgo in GCC 4.7.1" and go.dev/doc/install/gccgo).
   - But the Go team never let gccgo gate gc's evolution: runtime overhauls (Go 1.5
     runtime rewrite, new GC, register ABI in 1.17/1.18) landed in gc first and libgo had to
     catch up or diverge. The equilibrium that resulted: gccgo survives as a niche
     (exotic architectures, GCC-centric shops, Solaris — primary: same go.dev sources),
     never as a co-equal implementation, and nobody outside Ian's orbit contributes much.
   - **Reusable lesson:** upstream goodwill + a driver hook (`-compiler`) buys you a second
     implementation's *existence*, but currency with the language is a treadmill owned by
     whoever does the work. graalgo's requirement (2) — "always tracks the most recent Go
     release" — needs a *mechanical* per-release bump pipeline (frontend reuse that tracks
     gc's own parser/typechecker, plus the gc test suite as the acceptance gate), because
     goodwill alone demonstrably does not keep an alternative implementation current.

## 3. gollvm — Google's own LLVM Go, stalled

6. **State (primary: gollvm README at go.googlesource.com, incl. the FAQ).** Gollvm =
   `gofrontend` + a C++ bridge (`Llvm_backend`) lowering gofrontend IR to LLVM IR + the
   `llvm-goc` driver + libgo. The README FAQ states plainly: "Gollvm is not intended as a
   replacement for the main Go compiler"; supported only on x86_64/aarch64 Linux; GC scans
   stacks *conservatively* (no stack-map generation); no race detector; and — tellingly —
   "`go build` currently treats gollvm as an instance of gccgo (hence the need to pass
   compile flags via `-gccgoflags`). This is expected to be temporary."
7. **Why it stalled (scoped).** The last community build notes reference gollvm/gofrontend
   commits circa September 2020 (primary: `advancedwebdeveloper/gollvm_1.15.2_ubuntu20_release`
   README, "List of last known changes"); golang/go issues #53483 (2022) and #74866 (2025,
   "gollvm: build fails with llvm latest and release/16.x") show bit-rot against newer LLVM,
   with a third-party fork (`tinted-software/gollvm`, active ~231 days before crawl) carrying
   the maintenance instead of upstream. No upstream functional progress observed since ~2020.
8. **Reusable lessons.**
   - gollvm proves the *frontend-reuse* architecture works: gofrontend did the hard language
     work once, two backends (GCC, LLVM) shared it, and libgo gave both a complete stdlib
     for free. graalgo should copy this shape: reuse gc's own parser/typechecker (or
     gofrontend) rather than writing a Go frontend — the frontend is not where the
     differentiation is.
   - The *driver* is a second-class citizen in every alternative implementation: gollvm
     piggybacked on cmd/go's `-compiler gccgo` path and still carried the wart ("expected
     to be temporary", never fixed). graalgo's requirement (1) — full `cmd/go` driver
     parity (modules, build cache, vet, test, toolchain switching) — has *no* successful
     prior art at all (see §7); it is likely the largest unproven work item.
   - Conservative stack scanning was the accepted technical compromise for a non-gc
     backend; on Truffle the analog is mapping Go values onto the guest-language object
     model without precise stack maps — a known-hard design decision that must be made
     explicitly, not discovered mid-project.

## 4. llgo (go-llvm/llgo, 2012–2014) — the one that almost made it

9. **State (primary: go-llvm/llgo README).** Axel Wagner's llgo was a Go frontend for LLVM
   *written in Go itself*, self-hosting on x86-64 Linux, that "compiles and passes most of
   the standard library test suite and a substantial portion of the gc test suite". In
   2014–2015 it moved to llvm.org (README: "This project has moved to llvm.org"), after
   which it vanished — issue #225 "where did llgo go?" (2015) documents the disappearance,
   and the repo is now a struck-through entry in LLVM frontend lists.
10. **The drop-in driver attempt (primary: go-llvm/llgo PR #142, "Initial version of gllgo
    driver", 2014).** The commit message is directly on point for graalgo requirement (1):
    "This is intended to be a drop-in replacement for the gccgo executable that can be used
    by the libgo build system, 'go build' etc. In the long term, we can consider working
    with the upstream Go authors to adopt a subset of the gccgo command syntax as a common
    interface between 'go build' and a compiler." The follow-up discussion (primary: same
    PR) records the blocker: import-path lookup in `go/build` was hard-coded for gc/gccgo,
    so a third compiler could not slot in cleanly without upstream changes. The driver
    interface problem was identified in 2014 and never solved; llgo died with it unsolved.
11. **Reusable lessons.**
    - A from-scratch Go frontend *in Go itself* can reach "most of the stdlib test suite"
      with a small team — frontend work is tractable; the graveyard is full of projects
      that died on runtime/driver/ecosystem, not parsing.
    - Moving into a big institution's tree (llvm.org) *reduced* the project's visibility
      and bus factor instead of increasing it (primary: issue #225 discussion). Institutional
      home ≠ survival.
    - The name has since been reused by an unrelated active project (`goplus/llgo`, now
      `xgo-dev/llgo`, a Go-syntax LLVM compiler targeting C/Python/JS interop) — do not
      confuse the two in later research.

## 5. TinyGo — the one that survived, and what it teaches about stdlib reuse

12. **State (primary: tinygo-org/tinygo README + tinygo-site docs).** TinyGo is an
    LLVM-based Go compiler for microcontrollers, WASM/WASI, and CLI tools. Active (17.8k
    stars; LLVM 14–21 supported via build tags per tinygo-org/go-llvm README; Google
    sponsorship announced Feb 2020 per dev.to [SECONDARY-ONLY for the sponsorship claim]).
    It is the only alternative Go implementation with a living community and releases.
13. **Stdlib reuse vs reimplementation (primary: tinygo-site
    `content/docs/concepts/compiler-internals/pipeline.md`).** This is the key datum for
    graalgo requirement (6):
    - *Reused:* lexing, parsing, and typechecking are done by the standard library's
      `go/*` packages and `golang.org/x/tools/go`; SSA construction by
      `golang.org/x/tools/go/ssa`. TinyGo's own compiler only lowers Go SSA → LLVM IR.
    - *Reimplemented:* the runtime and the standard library. TinyGo vendors its own
      patched subset of the stdlib (with `tinygo` build tags and target-specific files);
      packages like `net/http`, full `reflect`, and much of `os` are absent or partial —
      the standard "missing standard library support / limited reflection" caveats
      (secondary: dev.to articles, consistent with the docs' supported-package lists).
    - I.e., the industry's best answer to "reuse the stdlib" is: **reuse the entire
      frontend for free, reimplement the runtime, and port the stdlib package-by-package
      against your runtime.** The runtime is the irreducible kernel; everything above it
      is porting labor proportional to stdlib surface.
14. **Driver (primary: TinyGo README/docs).** TinyGo ships its own `tinygo` CLI
    (`build/run/test/flash`) that *mimics* a subset of the `go` CLI for its targets — it is
    not a drop-in `go` replacement (different flags, targets, no module/toolchain-switching
    parity). Even the most successful alternative implementation did not attempt full
    driver parity.
15. **Why TinyGo survived where llgo/gollvm stalled:** it picked a niche gc serves badly
    (microcontrollers, WASM — small binaries via LLVM tree-shaking) rather than competing
    head-on, and it had a single sustained maintainer (Ayke van Laethem) plus corporate
    sponsorship. Lesson for graalgo: "better optimizer + JVM interop" is the niche; it
    needs an equally crisp answer to "what can graalgo do that gc fundamentally cannot."

## 6. Go on other managed runtimes

16. **.NET/CLR:** no project runs the Go language on the CLR. Found instead: `G#`
    (InfoWorld, 2026 — a new *Go-like* language for .NET with `func`, channels, `go`,
    `select` syntax, pre-1.0; explicitly a new language, not Go) and `ropnop/go-clr`
    (hosts the CLR *from* Go — the inverse direction). [SECONDARY-ONLY for G# details;
    primary would be its GitHub repo, not fetched in this track.]
17. **BEAM/other VMs:** no public Go-on-BEAM or Go-on-other-managed-runtime implementation
    found in these searches. GopherJS (Go→JavaScript transpiler) exists but is a
    source-to-source compiler to an *unmanaged* target, out of scope.
18. **Sulong (LLVM bitcode on Truffle) — the architectural alternative.** Sulong is
    GraalVM's Truffle interpreter for LLVM IR [SECONDARY-ONLY for current maintenance
    status — known from the GraalVM/Truffle ecosystem skill catalog]. It matters because it
    offers graalgo a second route that needs no Go AST interpreter at all: Go →(TinyGo/
    llgo-style frontend)→ LLVM IR → Sulong → Graal JIT. That trades "write a Go Truffle
    language" for "port the Go runtime to Sulong's C-ABI world" — a different, possibly
    smaller, porting surface, worth a dedicated feasibility comparison by another track.

## 7. Drop-in `go` toolchain replacements (full cmd/go driver)

19. **None exists.** Exhaustive search found no reimplementation of the `go` command's full
    surface (build/test/vet/fmt/mod/toolchain-switching/build-cache). The nearest points:
    - gccgo's bundled `go` (primary: go.dev/doc/install/gccgo) — not a reimplementation;
      it *is* cmd/go built from Go sources with gccgo compiler support compiled in.
    - llgo's `gllgo` driver (2014, §4) — a gccgo-executable-compatible shim so `go build`
      could shell out to it; died with llgo.
    - TinyGo's `tinygo` CLI — subset mimicry for its targets, not drop-in.
    - `goany`'s CLI — transpiler driver, not a Go toolchain.
20. **Reusable lesson:** requirement (1) ("alias go=graalgo indistinguishable from real go")
    is the least precedented requirement in the whole program. Every prior project either
    reused cmd/go itself (gccgo, gollvm) or reimplemented a *subset* CLI (TinyGo). A
    from-scratch module-aware, build-caching, toolchain-switching driver is greenfield
    territory — it should be estimated and risk-rated as its own workstream, not assumed
    as a corollary of having a compiler.

## 8. What the Truffle language catalog teaches about effort scale

21. **TruffleRuby — the calibrated yardstick (primary: Benoit Daloze's 2024 slides,
    eregon.me; oracle/truffleruby → shopify/truffleruby README).** Started 2013 at Oracle
    Labs; ~15 main authors listed in the README; lead (Daloze) on it since 2014; by 2024 it
    "targets full compatibility with CRuby 3.2, including C extensions" (Mastodon/Discourse
    cited as running). In 2024–2025 stewardship moved to the community/Shopify fork
    (shopify/truffleruby is the live repo: releases like 25.0.0→34.0.1 pinned in Shopify CI
    per primary GitHub PRs). **Roughly a decade of a dedicated multi-person team to take a
    Truffle language from zero to full compatibility with a production ecosystem** — and
    Ruby is dynamically typed (Truffle's home turf). A statically-typed, runtime-heavy
    language like Go (goroutines, precise GC, value semantics) is not obviously cheaper.
22. **The catalog shape (primary: GraalVM-adjacent sources; nixpkgs release notes).**
    Production Truffle languages (GraalJS, TruffleRuby, GraalPy) are all multi-year
    Oracle-Labs-scale efforts; the documented starting point is always the toy
    SimpleLanguage. The polyglot-interop payoff is real (the catalog's raison d'être) but
    it accrues only *after* the language is complete enough to run real programs — it does
    not de-risk the middle years.
23. **Effort-scale implication for graalgo:** the compiler/AST-interpreter is the small
    fraction. The cost centers, in order, are almost certainly: (a) Go runtime on the JVM
    (scheduler, GC interop, goroutine stacks — no prior art); (b) stdlib porting
    (TinyGo's lesson: package-by-package against your runtime); (c) the `cmd/go`-parity
    driver (no prior art at all); (d) per-release currency treadmill (the gccgo lesson).
    Any plan that prices "the Truffle interpreter" as the project is mispriced.

## T1 key takeaways

1. No Go-on-Truffle/GraalVM project has ever been published; the nearest JVM attempts are a 23-commit 2020 toy and coursework — graalgo is genuine greenfield, with no corpse to autopsy and no code to salvage.
2. Copy the winners' architecture: reuse gc's (or gofrontend's) parser/typechecker/SSA wholesale like TinyGo does, reimplement only the runtime, and port the stdlib package-by-package against it — the runtime is the irreducible kernel.
3. The two hardest requirements have zero successful prior art: full `cmd/go` driver parity (llgo's gllgo shim died on `go/build` hard-coding in 2014) and a mechanical per-release currency treadmill (gccgo's decade-long lag is the cautionary tale).
4. Calibrate effort against TruffleRuby: ~a decade of a dedicated multi-person Oracle Labs team for full production compatibility — price the runtime, stdlib port, and driver, not the AST interpreter.
5. Evaluate the Sulong shortcut explicitly (Go→LLVM IR→Truffle) before committing to a Go AST interpreter; and treat goany's Java-emitter doc as the seed catalog of Go→JVM semantic mismatches to design against.
