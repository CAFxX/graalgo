# graalgo development environment

Pinned 2026-10-03; host layout updated 2026-11-18 for the current box.

- Go toolchain: `~/tools/go` — `go version go1.27.1 linux/amd64`
  (`export PATH=~/tools/go/bin:$PATH`; Makefiles honor `GO=`).
- GraalVM: `~/tools/graalvm-jdk-25` — Oracle GraalVM 25.0.4+7.1 (JDK 25 LTS, JVMCI)
  (`export JAVA_HOME=~/tools/graalvm-jdk-25; export PATH=$JAVA_HOME/bin:$PATH`;
  Makefiles honor `GRAALVM_HOME=`).
  - Downloaded from `https://download.oracle.com/graalvm/25/latest/graalvm-jdk-25_linux-x64_bin.tar.gz`
  - sha256 `76007c309f821aaf435bce63162ea0395587fc77350801c81643fe7feea37276`
    (verified against the published `.sha256`; note it is a bare hash, not `sha256sum -c` format)
- OS: linux/amd64.
- Repository: integration checkout `~/dev/graalgo` (`master`, fast-forward
  only); one worktree per lane inside it at `~/dev/graalgo/.worktrees/<lane>`
  (gitignored, local-only; [AGENTS.md](../../AGENTS.md) §4).
- Vendored stdlib tree (external, not in the repo): `~/dev/graalgo-stdlib/<goVersion>/`
  ([tools/stdlib-vendor/](../../tools/stdlib-vendor/)).
- Requirements CLI: [tools/req/](../../tools/req/) (`cd tools/req && go run . lint`;
  it walks up to find [requirements/](../../requirements/); `make check` runs it).
- Resource limits on this shared host: see [AGENTS.md](../../AGENTS.md) §6.

## Host defect: lost bytes in large copies under load (BUG-0012)

The dev host (WSL2, AMD Ryzen 7 9700X) occasionally **loses one byte of a
large memory copy** when the machine is heavily loaded: after the copy the
byte at offset 28 of a 64-byte cache line holds 0 (or a stale earlier
value). It is not a graalgo or Go bug. Evidence (2026-11-24, 113 MB
copies, load raised to ~24 with `nice -n 19` busy loops):

| Program | Load | Corrupted / runs |
|---|---|---|
| `gofrontend hello.go` (deterministic 113 MB JSON) | ~16 | 0 / 40 |
| `gofrontend hello.go` | ~24–30 | 30 / 80 (all at offset ≡ 28 mod 64) |
| Go: `append`/`string()` growth of a 113 MB buffer | ~25–31 | 41 / 129 (and 27 / 255 with `GODEBUG=cpu.avx=off`, i.e. REP MOVS) |
| C: AVX2 `VMOVNTDQ` non-temporal copy, 1 thread | ~23 vs ~11 | 35 / 2267 vs **0 / 4401** |
| C: glibc `memcpy` (NT path), 2 threads, fresh/prefaulted/THP/pinned/reused buffers | ~23–26 | 57–285 per mode, every mode |
| C: plain byte-store loop (no NT / fast-string stores) | ~24 | 0 / 353 |

So any full-cache-line store path (non-temporal stores — Go's `memmove`
uses them for copies ≥ 1 MB on AMD — and fast-string `REP MOVS`/`STOS`,
which the kernel also uses for page clearing and `read`/`write` copies)
drops a single byte lane under all-core load, in any language, regardless
of page faults, THP or thread migration; nothing appears in the kernel log.
That signature points at the hardware/firmware (CPU or memory-controller
stability: PBO/Curve Optimizer undervolt, EXPO memory profile, or an AGESA
issue) rather than software. **Fix at the host**: run the CPU and memory at
stock settings (disable PBO/CO and EXPO), update the BIOS, then re-run a
loaded copy-and-verify loop (copy a ≥ 1 MB non-zero pattern into a fresh
buffer with Go's `copy`, compare, repeat for a minute under `nice -n 19`
busy loops on every core); expect 0 corruptions. Until then:

- keep host load moderate (`make -j2`, one JVM per lane, AGENTS.md §7):
  the rate is ~0 below load ~12 and climbs steeply above ~20 (but on
  2026-11-25 gofrontend's detection fired at load ~13, 25 minutes after
  the PC hard-crashed and rebooted overnight — treat any load as exposed);
- `gofrontend` scans its encoded document for NUL bytes (and treats a
  syntax error in encoding/json's re-scan of its own output alike) and
  exits **3** (infrastructure failure, `ErrHostCorruption`) instead of
  handing the backend a corrupted program. A stale byte that leaves the
  JSON valid is not detected there (a minority of the hits), nor is one
  hit in a later copy by the reader (seen live: two `internal/frontend`
  tests failed `make check` with `unmarshal doc: invalid character '\x00'`
  at load ~27). A flake that surfaces as a JSON parse error, an exit 3
  from `gofrontend` or a one-off divergence that does not reproduce is
  this defect until shown otherwise;
- any program on this host is exposed (the JVM, `go build`, `git`), not only
  graalgo: never trust a single run on a loaded host as evidence of a
  product bug — reproduce it at low load.
