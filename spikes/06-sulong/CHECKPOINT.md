# Spike 06: Sulong evaluation — checkpoint log

## 2026-10-03 ~07:35 CEST — setup & ground-truth research

Environment:
- No Java on box (`command -v java` → not found); ~/tools/graalvm-jdk-25 absent (coordinator install not present).
- Per task: do NOT install a JDK; note as setup gap, proceed analytically.
- Go 1.27.1 available at ~/tools/go/bin/go. gcc 13.3, cmake, make present. No clang.

Ground truth established (web, 2026-10-03):
1. **gollvm** (go.googlesource.com/gollvm): HEAD 605d1b6, committed 2025-04-07 ("update to LLVM mainline",
   opaque pointers). Dormant ~18 months; own README still says "early stage of development".
   No release, no maintenance. → NOT a viable per-release frontend.
2. **llgo (old, go-llvm/axw)**: dropped from LLVM monorepo (Phoronix; stuck at Go 1.5 era). Dead.
3. **xgo-dev/llgo ("LLGo")**: NEW, active (release v1.0.6, 2026). Go 1.27, LLVM 22, bdwgc (CONSERVATIVE GC),
   aimed at C/Python/JS interop, not gc-compat. Prebuilt linux-amd64 tarball ~310MB.
4. **TinyGo 0.42** (Sept 2026): active, Go 1.27 support, LLVM 22. Language SUBSET (own runtime,
   divergent semantics: no full reflect, cooperative scheduler, etc.). Release tarball downloading.
5. **Sulong**: still in oracle/graal monorepo; JDK 21 release notes show LLVM fixes → maintained.
   BUT: official docs (graalvm.org, JDK 24) state **"GraalVM supports LLVM bitcode versions 4.0 to 12.0.1"**.
   LLVM runtime unbundled from GraalVM distribution → standalone `llvm-community` distribution;
   bundled LLVM toolchain at 16.0.1 "deprecated for removal in Oracle GraalVM for JDK 23".
   → Every maintained Go→LLVM frontend emits LLVM 22 (opaque-pointer) IR; Sulong reads ≤12.0.1. Hard version gap.

Empirical plan:
- `tinygo build -o hello.ll` (confirmed supported in builder/build.go:672, v0.42.0) → inspect IR for
  Sulong-incompatible features (opaque `ptr` types, etc.).
- Cannot run under Sulong: no JVM/GraalVM on box (setup gap, documented; task forbids installing JDK).

Download: dl/tinygo0.42.0.linux-amd64.tar.gz in progress (detached curl, download.log).

## 2026-10-03 ~07:45 CEST — empirical results

- tinygo0.42.0.linux-amd64.tar.gz: downloaded OK (184,946,240 bytes, sha256 b87688fa2e19cee7…).
- `tinygo version` → 0.42.0 linux/amd64 (using go version <unknown> and LLVM version 22.1.4).
- `tinygo build -o hello.ll hello.go` → 4,102,055 bytes IR (target triple x86_64-unknown-linux-musleabihf).
- `tinygo build -o goroutine.ll goroutine.go` → 4,186,088 bytes IR.
- IR: 9020 opaque `ptr` uses, 0 typed pointers; `go` → `@tinygo_task_start` (external C runtime fn);
  0 landingpad/personality; #dbg_value debug records (LLVM 19+ style).
- `-o *.ll` emission confirmed in primary source builder/build.go:672 (v0.42.0).
- Sulong NOT run: no JVM on box (setup gap, task forbids installing JDK). Verdict overdetermined
  by IR version gap (LLVM 22 opaque-pointer IR vs Sulong ≤12.0.1).
- Verdict: COMMIT-DIRECT (6 vs 19). REPORT.md written.
