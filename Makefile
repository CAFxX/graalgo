# graalgo — root build orchestration
#
# Per project standing order (2026-10-12): `make all` regenerates all
# generated artifacts and runs the test suite. CI
# ([.github/workflows/ci.yml](.github/workflows/ci.yml)) runs `make all`
# and then `git diff --exit-code` — if the build regenerates any tracked
# file differently from what's committed, CI fails.
#
# Backend layout: [internal/backend/truffle](internal/backend/truffle) (the
# core module: Truffle AST nodes, value model, and the Go runtime on the JVM
# — scheduler, channels, maps, timers; one javac round, since nodes and
# runtime call each other), [internal/backend/interop](internal/backend/interop)
# (stdlib bindings, built downstream against truffle/out/classes). Each
# module has its own Makefile; this file orchestrates.
#
# CI SCOPE — honest, not aspirational. Kept in sync with the `all:` rule
# by .github/scripts/check_makefile.py (part of `make check`).
#   `make all` = generate build snapshot-check check test-pin-smoke test-v2
#                test-toolexec test-gir test-p3frontend test-ci test-java-fast
#                test-regression test-m1a pe-check-m1a test-m1w13
#                test-imgcheck test-m1r test-m1c test-m1d test-goroutine-modes:
#     generate  rebuilds the gofrontend binary + overlay.json, the 18
#               generated synth_*.go, and the GIR Go/Java codecs
#     build     compiles both backend modules (javac, -Werror)
#     snapshot-check
#               behavior snapshots (SPEC 5.9): runs the differential
#               corpus under graalgo and fails on any divergence from gc's
#               committed snapshot in [test/snapshots/](test/snapshots/)
#               that is not a reviewed, reason-coded expected divergence,
#               and on stale expected-divergence entries.
#     check     static gates: module grep gates (placeholder, debug-leftover,
#               v1 netpoller, off-heap/NIO, pin surface, pinned jars);
#               duplicate FQN; dead paths; MR-table
#               subset; check-repo (tracked-file size/binary, doc paths,
#               GG ids, this Makefile's .PHONY/header, generated-files
#               list, placeholder + runtime-unimplemented ratchets, the
#               new-path/v1 layering gate, the no-bridges gate (vs
#               GATE_BASE or the merge-base), the exclusion-register
#               gate (bug id + signature per deferred-parity entry), every
#               .github/scripts/test_*.py, markdown links, `tools/req`'s
#               tests + `lint`); and `internal/frontend/build.sh check` (gofmt/vet/
#               tests, vendoring diff, frontend/v1 wire-format validator);
#               vet-spikes (go vet of the test/spikes/abi probe modules)
#     test-pin-smoke
#               the pin-audit smoke (P2-M7, GG-0057; ~1.5 min): the whole
#               scheduler/runtime suite (SchedTestRunner, fast and slow
#               classes) in thread-per-goroutine mode under
#               jdk.VirtualThreadPinned; fails on a pin or a failing test.
#               Was part of `check` until 2026-11-25 (its own gate so CI's
#               shards balance)
#     test-v2 / test-toolexec / test-gir / test-p3frontend
#               the Go/Java module suites (frontend v2, -toolexec wrapper,
#               GIR Java codec, p3frontend loader)
#     test-ci   truffle smoke tests + test-jit (JIT regression guard)
#     test-java-fast
#               the fast backend Java unit tests (~30 s); the slow ones are
#               test-java-slow (weekly; quarantine in test/java/quarantine.txt)
#     test-regression
#               regression tests for fixed PKGs
#               ([test/regression/](test/regression/)): each
#               test/regression/<pkg>/ dir runs through the graalgo driver
#               + backend. Per-push: a re-break fails the gate.
#     test-m1a  the new path's differential gate (~2.5 min): import-free
#               corpus programs, test/m1a/programs, multi-package modules
#               and girinterp's programs built through graalgo-tool and run
#               on the new node package vs live gc; plus GG-0071's defer-path
#               checks (test/m1a/defer_path.sh)
#     pe-check-m1a
#               partial-evaluation gate of the new node package over the
#               benchmark set and the std paths of the realistic programs
#               (~35 s; test/m1a/pe_std.sh with its planted violation): any
#               failed compilation fails it
#     test-m1w13
#               the pointer-model gate (~1.5 min; GG-0072/GG-0062): programs
#               that use package unsafe through graalgo-tool on the new node
#               package vs live gc — generated loads and stores at every
#               byte offset per addressable kind, unsafe idioms of real
#               code, gc's -d=checkptr fatal errors, documented restrictions
#               ([test/m1w13/](test/m1w13/))
#     test-imgcheck
#               the descriptor-image gate (~15 s; toolexec W9a): every fact
#               of the type descriptors the node package builds for a
#               corpus of 1,800 types vs the ones gc's compiler emits, read
#               the way internal/reflectlite reads them; the walker is
#               checked against gc's reflect, and five seeded builder
#               errors must each be detected ([test/imgcheck/](test/imgcheck/))
#     test-m1r  the standard-library differential gate (~15 s): programs
#               importing std packages (upstream's source on graalgo's
#               fork) through graalgo-tool on the node package vs live gc
#               ([test/m1r/](test/m1r/))
#     test-m1c  the M1c gate (~15 s): sync's semaphores, internal/bytealg,
#               time and the syscall executor, the same way
#               ([test/m1c/](test/m1c/))
#     test-m1d  the M1d gate (~15 s; ~35 s on a cold build cache): hello world and a verb matrix with
#               the real fmt, os over internal/poll and the syscall role
#               table, the reflect probe, encoding/json, regexp,
#               text/template and friends, the same way
#               ([test/m1d/](test/m1d/))
#     test-goroutine-modes
#               the goroutine-mode matrix (~45 s; GG-0074: carrier mode is
#               the default): a few m1a/m1r/m1c goroutine programs in the
#               opt-in thread-per-goroutine mode (the scheduler suites run
#               in it in test-pin-smoke),
#               goroutine programs in carrier mode under -ea, and
#               Context.close(true) of a running program in both modes
#
# EXCLUDED from per-push CI (documented here and in ci.yml, not forgotten):
#   `make test` — the full suite: test-java-fast + test-java-slow (every
#   class of graalgo.test.Runner, SchedTestRunner, IfaceTestRunner, on
#   the right classpaths, judged against test/java/quarantine.txt). LONG;
#   the weekly CI full-suite job, not per-push.
#   `make check-links-external`, `make test-diffharness` — per-push CI
#   (markdown-links.yml, ci.yml's harness self-test) outside `make all`;
#   `make pre-land` runs `all`, both, and CI's drift check.
#   `make test-times` runs `all`'s list through test/lib/gates.py: each
#   gate timed (wall + CPU) and ratcheted against
#   test/duration-baseline.json (CI: test/duration-baseline-ci.json);
#   GATE_JOBS gates at a time. CI runs `all`
#   and test-diffharness this way, as four parallel jobs (GATE_SHARD=I/4:
#   disjoint shards covering the list, gates.py --shard; GATE_JOBS=2,
#   RATCHET=fail; off master, a Markdown-only change runs only the docs
#   tier). Cost profile:
#   docs/dev/test-profile.md.
#   `make quick` — the inner-loop tier (minutes; never landing evidence).
#   `make fuzz-newpath` — the differential fuzzer of the new path
#   ([test/fuzz/](test/fuzz/)): the hand-written adversarial corpus plus N
#   generated programs from SEED, each vs live gc (~1 s a program); nightly
#   (fuzz-newpath.yml), never per-push.
#   `make bench-spawn-flat` — goroutine spawn cost flat in the live count
#   (ADR-0018), a wall-clock check: weekly in ci.yml's full-suite job,
#   never per-push (host load moves it).
#   `make mutation` — the new path's mutation oracle (tools/mutation, m3-plan G6):
#   each mutant of tools/mutation/mutants.json must turn the per-push
#   differential gates red; nightly (mutation.yml). `make stdlib-gate` —
#   the 486 test/corpus programs with imports vs gc against
#   test/corpus/std-register.tsv; manual (stdlib-gate.yml, BUG-0033).
#


# Generated artifacts and their generators (standing order: every generated
# file committed to the repo must list its generator here; `make all`
# regenerates them and CI fails on drift).
#   - [internal/frontend/gofrontend](internal/frontend/gofrontend) +
#     [overlay.json](internal/frontend/overlay.json): regenerated by
#     `generate` below ([internal/frontend/build.sh](internal/frontend/build.sh)).
#     Both are gitignored build outputs.
#   - 18 [internal/frontend/synth_*.go](internal/frontend/) files: regenerated
#     by `make gen-synth`
#     ([internal/frontend/run_gen_synth.py](internal/frontend/run_gen_synth.py))
#     from gc 1.27.1 sources (the driver version-pins go1.27.1). Committed and
#     drift-gated. Generator -> outputs:
#       gen_synth_archive_mime.py -> synth_archive_tar.go, synth_archive_zip.go,
#         synth_mime.go, synth_mime_multipart.go, synth_mime_quotedprintable.go
#       gen_synth_bufio.py        -> synth_bufio.go (+ gen_synth_adapt/ patch)
#       gen_synth_compress.py     -> synth_compress_{bzip2,flate,gzip,lzw,zlib}.go
#         (+ gen_synth_adapt/ patches)
#       gen_synth_mathbits.py     -> synth_mathbits.go
#       gen_synth_mathcmplx.py    -> synth_mathcmplx.go
#       gen_synth_mathrand.py     -> synth_mathrand.go
#       gen_synth_mathrandv2.py   -> synth_mathrandv2.go
#       gen_synth_path.py         -> synth_path.go
#       gen_synth_regexpsyntax.py -> synth_regexpsyntax.go
#       gen_synth_textscanner.py  -> synth_textscanner.go
#     The six `+ patch` files additionally apply an adaptation patch from
#     [internal/frontend/gen_synth_adapt/](internal/frontend/gen_synth_adapt/)
#     carrying the hand-applied frontend-constraint (WS D/G) adaptations the
#     verbatim generator does not model; see its README.md.
#     (The remaining internal/frontend/synth_*.go files are authored or
#     vendored, not generator outputs.)
#   - GIR codecs: [internal/toolexec/ir/gir_gen.go](internal/toolexec/ir/gir_gen.go),
#     [Gir.java](internal/backend/gir/src/main/java/graalgo/gir/Gir.java) and
#     [GirCodec.java](internal/backend/gir/src/main/java/graalgo/gir/GirCodec.java):
#     regenerated by `generate` (`go generate ./ir` in internal/toolexec,
#     from [schema.txt](internal/toolexec/ir/schema.txt)). Committed and
#     drift-gated.
#   - Intrinsic dispatch table:
#     [IntrinsicTable.java](internal/backend/truffle/src/main/java/graalgo/go/intrinsics/IntrinsicTable.java):
#     regenerated by `generate` (`go generate ./intrinsics` in
#     internal/toolexec, from
#     [registry.txt](internal/toolexec/intrinsics/registry.txt)). Committed
#     and drift-gated.
#   - Syscall role table:
#     [SyscallTable.java](internal/backend/truffle/src/main/java/graalgo/syscall/SyscallTable.java):
#     regenerated by `generate` (`go generate ./syscalls` in
#     internal/toolexec, from
#     [table.txt](internal/toolexec/syscalls/table.txt)). Committed and
#     drift-gated.
# GENERATED_EXTERNAL: committed generated files whose generator is NOT run
# by `make all` (run it by hand when its input changes):
#   - internal/frontend/p3frontend/zz_asm_inventory_gen.go
#     <- tools/stdlib-fork/gen-asm-inventory.py (input: the vendored stdlib)
#   - test/imgcheck/corpus.go
#     <- test/imgcheck/gen.py | gofmt (test/imgcheck/run.sh fails when stale)
# check_makefile.py fails if a tracked file carrying a "Code generated ...
# DO NOT EDIT" header is in neither list.
GENERATED := internal/frontend/gofrontend internal/frontend/overlay.json
GENERATED_SYNTH := \
	internal/frontend/synth_archive_tar.go \
	internal/frontend/synth_archive_zip.go \
	internal/frontend/synth_mime.go \
	internal/frontend/synth_mime_multipart.go \
	internal/frontend/synth_mime_quotedprintable.go \
	internal/frontend/synth_bufio.go \
	internal/frontend/synth_compress_bzip2.go \
	internal/frontend/synth_compress_flate.go \
	internal/frontend/synth_compress_gzip.go \
	internal/frontend/synth_compress_lzw.go \
	internal/frontend/synth_compress_zlib.go \
	internal/frontend/synth_mathbits.go \
	internal/frontend/synth_mathcmplx.go \
	internal/frontend/synth_mathrand.go \
	internal/frontend/synth_mathrandv2.go \
	internal/frontend/synth_path.go \
	internal/frontend/synth_regexpsyntax.go \
	internal/frontend/synth_textscanner.go
GENERATED += $(GENERATED_SYNTH)
GENERATED += internal/toolexec/ir/gir_gen.go \
	internal/backend/gir/src/main/java/graalgo/gir/Gir.java \
	internal/backend/gir/src/main/java/graalgo/gir/GirCodec.java \
	internal/backend/truffle/src/main/java/graalgo/go/intrinsics/IntrinsicTable.java \
	internal/backend/truffle/src/main/java/graalgo/syscall/SyscallTable.java
GENERATED_EXTERNAL := internal/frontend/p3frontend/zz_asm_inventory_gen.go \
	test/imgcheck/corpus.go
# NOTE: [test/snapshots/](test/snapshots/)/go<VERSION>.json are also generated
# and committed, but their generator is `make snapshot` (gc's behavior on
# the corpus: re-record when a corpus program changes or on a toolchain
# upgrade), NOT `make all` — `make all` *verifies* graalgo against them via
# snapshot-check.

GO ?= $(HOME)/tools/go/bin/go

.PHONY: all generate gen-synth build graalgo check check-repo vet-spikes test-pin-smoke test-ci test-jit bench test-java-fast test-java-slow test-java-timing java-test-classes java-test-lists-check test-regression test-p3frontend test-v2 test-toolexec test-gir test-m1a bench-m1a bench-suite bench-realistic bench-goroutines bench-spawn-flat pe-report pe-check-m1a test-m1r test-m1c test-m1d test-goroutine-modes test-m1w13 fuzz-newpath check-links-external test-diffharness pre-land goroot gate1-pilot gate1 test clean snapshot snapshot-check mutation stdlib-gate fast-build fast-test test-imgcheck test-times quick

all: generate build snapshot-check check test-pin-smoke test-v2 test-toolexec test-gir test-p3frontend test-ci test-java-fast test-regression test-m1a pe-check-m1a test-m1w13 test-imgcheck test-m1r test-m1c test-m1d test-goroutine-modes

# Regenerate the synthetic stdlib sources (internal/frontend/synth_*.go) from
# gc 1.27.1 (P1-R1-2/P1-R7-7 wiring). The driver version-pins go1.27.1 and
# fails fast otherwise; CI's `git diff --exit-code` after `make all` fails on
# any drift between the generators (+ adaptation patches) and the committed
# files.
gen-synth:
	GO="$(GO)" python3 internal/frontend/run_gen_synth.py

# Force-regenerate the gofrontend binary and overlay.json from the
# vendored frontend sources (internal/frontend/*.go + internal/frontend/go overlay).
generate: gen-synth
	cd internal/frontend && ./build.sh build
	# GIR codecs (Go + Java) from internal/toolexec/ir/schema.txt and the
	# Java intrinsic table from internal/toolexec/intrinsics/registry.txt and
	# the syscall role table from internal/toolexec/syscalls/table.txt
	cd internal/toolexec && PATH="$(dir $(GO)):$$PATH" $(GO) generate ./ir ./intrinsics ./syscalls

build:
	$(MAKE) -C internal/backend/truffle main
	$(MAKE) -C internal/backend/interop main

# The graalgo driver binary (Phase-4): `alias go=graalgo`.
# Builds cmd/graalgo into ./graalgo.
graalgo:
	$(GO) build -o graalgo ./cmd/graalgo/

check:
	$(MAKE) -C internal/backend/truffle check
	$(MAKE) -C internal/backend/interop check
	python3 .github/scripts/check_duplicate_fqn.py
	# dead-path gate: no executable reference to the deleted per-phase
	# tree or the banned scratch dir (a dead path makes a harness report
	# ClassNotFoundException as a divergence). The gate's own test first.
	python3 .github/scripts/test_check_no_dead_paths.py
	python3 .github/scripts/check_no_dead_paths.py
	GO="$(GO)" python3 .github/scripts/check_mr_table_subset.py
	$(MAKE) check-repo
	$(MAKE) vet-spikes
	# gofrontend gate: gofmt/vet/tests + vendoring diff + frontend/v1
	# wire-format validation (internal/frontend/build.sh check).
	# /tmp is a 512M shared tmpfs: keep the fresh-export JSONs on the
	# persistent disk, OUTSIDE the frontend module (<repo>/.tmp, gitignored):
	# a TMPDIR inside it made every t.TempDir path a test input, so go test
	# never cached the frontend suite (~40 s per check).
	mkdir -p $(CURDIR)/.tmp/frontend-check && \
	cd internal/frontend && TMPDIR="$(CURDIR)/.tmp/frontend-check" \
	GOTMPDIR="$(CURDIR)/.tmp/frontend-check" GO="$(GO)" ./build.sh check

# The pin-audit smoke (Makefile header; internal/backend/truffle's
# pin-smoke): the scheduler/runtime suite in thread mode under
# jdk.VirtualThreadPinned. Shares truffle/out/test-tmp with test-ci and
# test-java-fast (gates.py never runs them at once).
test-pin-smoke: build java-test-classes
	$(MAKE) -C internal/backend/truffle pin-smoke

# The ABI spike's probe modules (test/spikes/abi/*, abi-spike.md) are built
# by no other target and would rot silently: vet (which type-checks and
# compiles) each one here, in `make check`.
SPIKE_MODULES := $(sort $(dir $(wildcard test/spikes/abi/*/go.mod)))
vet-spikes:
	@set -e; test -n "$(SPIKE_MODULES)" || { echo "vet-spikes: no modules found"; exit 1; }; \
	for m in $(SPIKE_MODULES); do echo "== vet $$m"; \
	  (cd $$m && GOTOOLCHAIN=local PATH="$(dir $(GO)):$$PATH" $(GO) vet ./...); done

# Repo-wide static gates (no build needed). Each check_*.py has a
# test_*.py that plants the violation it guards; every test_*.py runs here.
check-repo:
	# Gate self-tests plant violations, so their passing output is full of
	# gate failure lines, and a "file.go:N: ..." line becomes a fake
	# ##[error] annotation through setup-go's problem matcher (the top
	# annotation of a red run pointed at test_check_ratchets.py's planted
	# tools/b.go). A self-test's output is shown only when it fails.
	@set -e; for t in .github/scripts/test_*.py test/java/test_*.py test/lib/test_*.py test/corpus/test_*.py; do \
	  if out=$$(python3 $$t 2>&1); then echo "== $$t: ok"; \
	  else echo "== $$t: FAILED"; echo "$$out"; exit 1; fi; done
	python3 .github/scripts/check_tracked_files.py
	python3 .github/scripts/check_doc_paths.py
	python3 .github/scripts/check_gg_ids.py
	python3 .github/scripts/check_makefile.py
	python3 .github/scripts/check_ratchets.py
	python3 .github/scripts/check_layering.py
	python3 .github/scripts/check_no_bridges.py
	# exclusion registers: every deferred-parity entry cites a bugs.md id
	# and (differential registers) pins a signature; gate tests first.
	cd test/differential && PATH="$(dir $(GO)):$$PATH" $(GO) test -count=1 -run 'Register|IsPinned|LoadBugIDs|Parse' ./internal/harness/ && \
	  PATH="$(dir $(GO)):$$PATH" $(GO) run ./cmd/diffharness registers -bugs ../../docs/dev/bugs.md \
	  -unsigned ../../tools/stdlib-fork/exclusions.yaml \
	  corpus-exclusions.yaml srctest-exclusions.yaml typeparam-exclusions.yaml
	# the mutant set of the nightly mutation oracle stays applicable (anchors
	# occur exactly once, kill programs exist), and the tool's own tests.
	cd tools/mutation && PATH="$(dir $(GO)):$$PATH" $(GO) test .
	python3 .github/scripts/check_markdown_links.py --skip-external
	# the req CLI's own tests first (gate-of-the-gate, ~0.3 s, go-test cached;
	# incl. its .worktrees skip), then the lint itself.
	cd tools/req && PATH="$(dir $(GO)):$$PATH" $(GO) test . && PATH="$(dir $(GO)):$$PATH" $(GO) run . lint

# The rest of per-push CI that `make all` does not cover (2026-11-20: lanes
# that ran a hand-picked subset of the gates landed a girinterp regression
# and dead-looking external links; both reddened master).
#   check-links-external  the markdown-links workflow: check-repo's link
#                         check plus every external URL (~1 min, network)
#   test-diffharness      ci.yml's harness self-test (~12 min): the diffharness
#                         comparator gc-vs-gc on the corpus (no false
#                         positives, rerun stability, mutation sensitivity)
#   pre-land              what a lane runs before reporting done: `make
#                         all`, both of the above, then ci.yml's drift
#                         check (no modified or untracked file left behind)
# Not covered (needs a full GOROOT/test tree, ~10 min): conformance.yml's
# test/differential/run_conformance.sh, path-filtered on push.
check-links-external:
	python3 .github/scripts/check_markdown_links.py

# Timed gate runs ([test/lib/gates.py](test/lib/gates.py); profile and
# costs: [docs/dev/test-profile.md](docs/dev/test-profile.md)). The setup
# targets (generate build graalgo java-test-classes) run first, then the
# gates GATE_JOBS at a time (output buffered per gate, never interleaved),
# each timed (wall + CPU); the table goes to stdout and GATE_TIMES (JSON),
# and each gate's CPU time is ratcheted against
# GATE_BASELINE, the baseline of the environment (the dev host's
# [test/duration-baseline.json](test/duration-baseline.json) by default; CI
# passes [test/duration-baseline-ci.json](test/duration-baseline-ci.json)),
# under its GATE_CACHE section (warm, or cold: CI's caches missed): RATCHET=warn
# (local default) prints SLOWER above 1.2x; RATCHET=fail (CI) also fails
# above 2x + 30 s. GATE_JOBS defaults to 1 (shared hosts: AGENTS.md §7);
# CI runs 2.
#   make test-times                    `make all`'s gates, timed
#   make test-times TARGETS="test-m1a test-m1r"
GATE_JOBS ?= 1
RATCHET ?= warn
GATE_TIMES ?= $(CURDIR)/.tmp/gate-times.json
GATE_BASELINE ?= test/duration-baseline.json
GATE_CACHE ?= warm
GATES = python3 test/lib/gates.py run --make "$(MAKE)" -j $(GATE_JOBS) --ratchet $(RATCHET) --json $(GATE_TIMES) \
	--baseline $(GATE_BASELINE) --cache $(GATE_CACHE)
TARGETS ?= all
test-times:
	$(GATES) $(TARGETS)

# The inner-loop tier (~3 min on a quiet host): never landing evidence —
# `make pre-land` is. The static repo gates, the cheap module suites, and
# slices of the differential gates: the edge-case snapshot slice
# (QUICK_SNAPSHOT: arithmetic, shifts, conversions, slices, maps, defers) and a few programs
# of m1a/m1r/m1d across defers, interfaces, generics, goroutines, panics
# and the real fmt.
QUICK_M1A ?= test/m1a/programs/defer_chain.go test/m1a/programs/flat_values.go test/m1a/programs/generics.go test/m1a/programs/ifaces.go test/m1a/programs/concurrency.go test/m1a/programs/goroutine_panic.go test/m1a/programs/eval_order.go test/m1a/programs/panic_print.go
QUICK_SNAPSHOT ?= 06_nested_defer,60_narrow_unary_widen,49_int_arith_edges,50_shifts,51_int_conversions,52_float_edges,53_compare_ops,55_slice_bounds,56_map_ops,57_defer_order
QUICK_M1R ?= test/m1r/programs/sort_slice.go test/m1r/programs/strconv_conv.go
QUICK_M1D ?= test/m1d/programs/hello_fmt.go
quick: generate build
	$(MAKE) check-repo test-v2 test-gir
	$(MAKE) snapshot-check SNAPSHOT_PROGRAMS=$(QUICK_SNAPSHOT)
	$(MAKE) test-m1a M1A_PROGRAMS="$(QUICK_M1A)"
	$(MAKE) test-m1r M1R_PROGRAMS="$(QUICK_M1R)"
	$(MAKE) test-m1d M1D_PROGRAMS="$(QUICK_M1D)"

test-diffharness: build
	DH_TIMEOUT=$${DH_TIMEOUT:-90} DH_JOBS=$${DH_JOBS:-$(SNAPSHOT_JOBS)} ./test/differential/run.sh

pre-land:
	$(GATES) --tier-base= all check-links-external test-diffharness
	git diff --exit-code
	@if [ -n "$$(git status --porcelain --untracked-files=all)" ]; then \
	  echo "DRIFT: modified or untracked files:"; git status --porcelain --untracked-files=all; exit 1; fi

# CI-feasible tests: truffle module smoke tests (runtime + DSL processor).
# Fast (<2 min) and dependency-free. The full differential corpora are
# `make test` (see header comment).
test-ci: build
	$(MAKE) -C internal/backend/truffle smoke-test
	$(MAKE) test-jit

# JIT regression guard (~10 s): builds test/jit/peguard through graalgo-tool
# and runs it on the new node package with synchronous, traced Truffle
# compilation (default tiers + OSR, then CompileImmediately); fails if any
# compilation fails (e.g. a PermanentBailoutException from partial
# evaluation entering JDK code), if a guarded hot function or an OSR loop is
# never compiled, or if the output differs from gc's; a planted graph-size
# limit must fail compilations. See test/jit/run_pe_guard.sh.
# Keep the JVM temp files off the shared /tmp tmpfs.
test-jit: build
	mkdir -p $(CURDIR)/test/jit/.tmp
	TMPDIR="$(CURDIR)/test/jit/.tmp" test/jit/run_pe_guard.sh

# gc-vs-graalgo benchmarks (not a gate): the benchmark suite, bench-suite
# below (m3-plan D3: v1's test/bench/run.sh and its `pe-check` were deleted;
# the partial-evaluation gate over the benchmark set is pe-check-m1a).
bench: bench-suite

# Regression tests for fixed PKGs ([test/regression/](test/regression/)):
# each test/regression/<pkg>/ directory is a Go test package run through
# the graalgo driver + backend (`graalgo test`). In `all` (hence per-push
# CI): a re-break of a fixed bug fails the gate loudly.
# /tmp is a 512M tmpfs shared with other work and routinely full: keep the
# JVM and driver temp files on the persistent disk (same pattern as the
# snapshot targets). test/regression/.tmp is gitignored.
# `timeout 600` is the failability mechanism for hang regressions: no
# in-test watchdog is possible (timers do not fire under graalgo, PKG-048),
# so a reintroduced hang is killed here and fails the gate instead of
# hanging CI. Post-fix the suite passes in ~75s.
# A directory's optional `jvm.opts` file adds JVM flags after the caller's
# GRAALGO_JAVA_OPTS (so it can raise a heap cap): tls-wrap-hang's test binary
# (crypto/tls and its dependencies) is a ~300 MB v1 JSON wire document, held
# several times over while the v1 runner builds its AST (~1.5 GB live at the
# peak), so the 2 GB cap of AGENTS.md §7 and CI's full-suite job is an OOM
# there. v1-only (the new path loads a binary GIR bundle), deleted at M3:
# BUG-0053.
REGRESSION_TEST_DIRS := $(wildcard test/regression/*/)
REGRESSION_TMP := $(CURDIR)/test/regression/.tmp
test-regression: generate build graalgo
	@test -n "$(strip $(REGRESSION_TEST_DIRS))" || { echo "TEST-REGRESSION: no test/regression/*/ dirs (zero cases is not a pass)"; exit 1; }
	mkdir -p $(REGRESSION_TMP) && \
	export TMPDIR="$(REGRESSION_TMP)" GOTMPDIR="$(REGRESSION_TMP)"; \
	eval "$$(bash test/lib/jvmcds.sh sync-env)"; \
	for d in $(REGRESSION_TEST_DIRS); do \
	  GRAALGO_JAVA_OPTS="$${GRAALGO_JAVA_OPTS:-} $$(cat $${d}jvm.opts 2>/dev/null)" timeout 600 ./graalgo test ./$$d || exit 1; \
	done

# p3frontend drift tests: the phase-3 importer/loader module's own test
# suite, including the synthetic-surface drift tests that the
# internal/frontend/synth_*.go omission comments pin on. Runs `go vet`
# and `go test` in the p3frontend module. Needs the gofrontend binary
# (built by `generate`); the tests resolve it via GRAALGO_GOFRONTEND or
# the <repo>/internal/frontend/gofrontend fallback (export.go). Needs
# the backend too (built by `build`): the differential tests run the
# merged programs under internal/backend/graalgo-sync-run (P3SyncMain),
# the successor of the deleted phases/phase2 `make run` entry point.
#
# In `all` (hence per-push CI) since review-2026-10-31 P1-1: the four
# failures that kept it out are fixed in this lane —
#   TestSyncExportDir, TestSyncFormerScopeErrorsNowCompile (was
#     TestSyncScopeErrorParity) — re-scoped to the real-sync contract:
#     synthetic sync was retired in 503608ea, so sync compiles as
#     ordinary calls and the old M2 scope errors no longer exist.
#   TestTwoPackageDifferential, TestEmbeddedFieldRebinding — the
#     differential runner rewired from phases/phase2 to graalgo-sync-run.
# The same lane also fixed the further drift failures the full run
# exposed (errors/reflectlite and strings/iter oracle imports, the
# context Deadline restoration, the strconv strconvError type, and
# strings SplitSeq landing); see the test comments for each verdict.
# /tmp is a 512M shared tmpfs: keep go build/test temps on persistent
# disk, as test-v2 does (p3frontend/.tmp is gitignored).
P3FRONTEND_TMP := $(CURDIR)/internal/frontend/p3frontend/.tmp
test-p3frontend: generate build
	mkdir -p $(P3FRONTEND_TMP) && \
	cd internal/frontend/p3frontend && TMPDIR="$(P3FRONTEND_TMP)" GOTMPDIR="$(P3FRONTEND_TMP)" $(GO) vet ./... && \
	TMPDIR="$(P3FRONTEND_TMP)" GOTMPDIR="$(P3FRONTEND_TMP)" $(GO) test ./...

# Frontend v2 module tests: the [internal/frontend/v2](internal/frontend/v2)
# module (own go.mod) — parser, checker, emitter, and the Go→Java binary
# encoder, including the byte-exact zigzag wire contract
# (TestEncodeIntContract) and the cross-language golden document
# (TestEncodeGoldenDoc, mirrored by BinaryDecoderTest on the Java side).
# In `all` (hence per-push CI): this module was previously invisible to CI
# (R5-P1-2) and the R5-P1-1 zigzag corruption had no mechanical backstop.
# /tmp is a 512M shared tmpfs: keep go build/test temps on persistent disk.
V2_TMP := $(CURDIR)/internal/frontend/v2/.tmp
test-v2:
	mkdir -p $(V2_TMP) && \
	cd internal/frontend/v2 && TMPDIR="$(V2_TMP)" GOTMPDIR="$(V2_TMP)" $(GO) vet ./... && \
	TMPDIR="$(V2_TMP)" GOTMPDIR="$(V2_TMP)" $(GO) test ./...

# -toolexec wrapper module tests: [internal/toolexec](internal/toolexec)
# (own go.mod) — the graalgo-tool walking skeleton of
# docs/arch/toolexec-design.md. The end-to-end tests drive the stock
# `go build -toolexec` over a two-package module and assert the archive IR
# member, the patched build ID, warm-cache behaviour (zero tool runs) and
# gc/graalgo cache separation. $(GO)'s directory is put first on PATH
# because the tests invoke `go` themselves.
TOOLEXEC_TMP := $(CURDIR)/internal/toolexec/.tmp
# graalgo-tool's end-to-end tests (each its own GOCACHE, module and tool
# binaries) run 4 at a time (t.Parallel; -parallel caps it at the CI
# runner's vCPUs): the package was the gate's critical path, 30 -> 15 s.
test-toolexec:
	mkdir -p $(TOOLEXEC_TMP) && \
	cd internal/toolexec && PATH="$(dir $(GO)):$$PATH" TMPDIR="$(TOOLEXEC_TMP)" GOTMPDIR="$(TOOLEXEC_TMP)" $(GO) vet ./... && \
	PATH="$(dir $(GO)):$$PATH" TMPDIR="$(TOOLEXEC_TMP)" GOTMPDIR="$(TOOLEXEC_TMP)" $(GO) test -parallel 4 ./...

# GIR Java decoder (W2): [internal/backend/gir](internal/backend/gir),
# JDK-only, generated from internal/toolexec/ir/schema.txt. Decodes,
# validates and re-encodes the golden vectors of internal/toolexec/ir/testdata
# byte for byte, checks the malformed vectors are rejected, and fuzzes the
# decoder. The Go side is in test-toolexec (ir/, emit/).
test-gir:
	$(MAKE) -C internal/backend/gir test

# Toolexec milestone M1a differential gate (~2.5 min; per-push since the M1a
# close-out): the corpus programs that import nothing and test/m1a/programs,
# built by the stock `go build` through graalgo-tool and run as linked
# bundles on the new node package
# (internal/backend/truffle/src/main/java/graalgo/go), compared with gc on
# stdout, normalized stderr and exit status (test/m1a/run.sh); then the
# defer-mechanism checks of GG-0071 (test/m1a/defer_path.sh: the open-coding
# limit and the forced-chain differential). M1A_PROGRAMS narrows run.sh to
# the given programs and skips defer_path.sh.
test-m1a: build
	python3 test/m1a/test_transform.py
	python3 test/m1a/test_tbnorm.py
	GO="$(GO)" test/m1a/run.sh $(M1A_PROGRAMS)
	@if [ -z "$(strip $(M1A_PROGRAMS))" ]; then echo GO="$(GO)" test/m1a/defer_path.sh; GO="$(GO)" test/m1a/defer_path.sh; fi
M1A_PROGRAMS ?=

# The test/bench workloads on the new node package (test/m1a/bench.sh):
# bench-m1a prints best-round times against gc (not a gate); pe-check-m1a is
# the partial-evaluation gate of the new nodes (CompilationFailureAction=
# ExitVM, synchronous compilation): any failed compilation fails it; then
# the same check on the standard-library paths of test/bench/realistic (json,
# regexp, flate, template, strings/maps/sort) with its planted violation
# (test/m1a/pe_std.sh). M1A_BENCH narrows both to the given benchmarks and
# skips pe_std.sh.
bench-m1a: build
	GO="$(GO)" test/m1a/bench.sh $(M1A_BENCH)
pe-check-m1a: build
	python3 test/bench/realistic/test_pe_report.py
	GO="$(GO)" test/m1a/bench.sh --pe-check $(M1A_BENCH)
	@if [ -z "$(strip $(M1A_BENCH))" ]; then echo GO="$(GO)" test/m1a/pe_std.sh; GO="$(GO)" test/m1a/pe_std.sh; fi
M1A_BENCH ?=

# The benchmark suite (not a gate; ~25 min for the whole suite with --pgo):
# test/bench/suite/ under gc, gc+PGO and graalgo (graalgo-tool, node
# package), results checked against gc; warmup curves, steady-state medians,
# time to peak, JSON. Method and numbers: docs/arch/performance.md.
# BENCH_ARGS: e.g. "--reps 3 --pgo --json out.json fib loop".
bench-suite: build
	GO="$(GO)" test/bench/suite.sh $(BENCH_ARGS)
BENCH_ARGS ?=

# The realistic benchmarks (not a gate; docs/arch/north-star.md §3):
# stdlib pipelines, an allocation-heavy heap with tail latency, goroutine
# scaling — test/bench/realistic/ on the bench-suite runner, gc legs at
# GOMAXPROCS=4 to match the JVM's -XX:ActiveProcessorCount=4; peak RSS and
# CPU time per leg. BENCH_ARGS as for bench-suite, plus e.g.
# --gg-variant zgc="-XX:+UseZGC" or --gc-variant gogc400="GOGC=400".
bench-realistic: build
	GO="$(GO)" test/bench/suite.sh --dir test/bench/realistic --gc-env GOMAXPROCS=4 $(BENCH_ARGS)

# Goroutine scaling (not a gate; north-star.md §4.3, ADR-0018): the realistic
# `goroutines` program at each GOR_N of GOR_NS, gc against graalgo in the
# default carrier mode (the opt-in thread-per-goroutine mode,
# -Dgraalgo.sched.goroutines=thread, is quadratic: 100,000 take minutes to
# spawn). 1,000,000 goroutines need a ~5 GB heap: GOR_JAVA_OPTS sets -Xmx12g;
# run it alone on a shared host (minutes per N).
GOR_NS ?= 10000 100000 1000000
GOR_JAVA_OPTS ?= -Xmx12g -XX:ActiveProcessorCount=4
bench-goroutines: build
	for n in $(GOR_NS); do \
	  GOR_N=$$n GRAALGO_JAVA_OPTS="$(GOR_JAVA_OPTS)" GO="$(GO)" test/bench/suite.sh \
	    --dir test/bench/realistic --gc-env GOMAXPROCS=4 --reps 1 $(BENCH_ARGS) goroutines || exit 1; \
	done

# Spawn-scaling check (benchmark tier; weekly in ci.yml's full-suite job,
# never per-push: a wall-clock assertion follows host speed and load):
# test/bench/scaling/spawn_flat.go times batches of 1,000 spawns with 1,000
# vs 19,000 goroutines alive and prints "spawn flat" when the late batches
# stay within 3x the early ones (or under 50 ms) — gc's output, which
# test/m1r/run.sh compares with graalgo's in the default carrier mode. The
# O(1)-spawn regression signal of ADR-0018 (the thread-per-goroutine path
# grows ~5x at 20,000); per push, CarriersTest guards the mechanism (no
# polyglot thread per goroutine) and test/m1r/programs/spawn_parked.go the
# workload.
bench-spawn-flat: build
	GO="$(GO)" test/m1r/run.sh test/bench/scaling/spawn_flat.go

# The compilation report of the realistic programs (~1.5 min; nightly as
# .github/workflows/compile-health.yml with --max-churn): failed, deoptimized
# and invalidated Truffle compilations per program
# (test/bench/realistic/pe-report.sh). PE_REPORT_ARGS: e.g. "--sync jsonrt".
pe-report: build
	GO="$(GO)" test/bench/realistic/pe-report.sh $(PE_REPORT_ARGS)
PE_REPORT_ARGS ?=

# Toolexec milestone M1r/M1b differential gate (per-push in `make all`): programs
# that import standard-library packages, built through graalgo-tool against
# graalgo's fork ([stdlib/](stdlib/): facade runtime, everything else
# upstream's source) and run on the node package, compared with gc. See
# test/m1r/run.sh. M1R_PROGRAMS narrows the run to the given programs.
test-m1r: build
	GO="$(GO)" test/m1r/run.sh $(M1R_PROGRAMS)
M1R_PROGRAMS ?=

# The M1c gate (per-push in `make all`): sync's semaphores and notify lists,
# internal/bytealg, time on the runtime timer heap and the syscall executor,
# through graalgo-tool on the node package against gc; programs that need
# another lane's piece are PENDING until their probe passes. See
# test/m1c/run.sh. M1C_PROGRAMS narrows the run.
test-m1c: build
	GO="$(GO)" test/m1c/run.sh $(M1C_PROGRAMS)
M1C_PROGRAMS ?=

# The M1d gate (per-push in `make all`): the real fmt (hello world, the verb
# matrix), os from upstream's source over internal/poll, the runtime poller
# and the syscall role table, reflect (the abi-spike probe: Value.Call,
# MakeFunc, run-time-made types) and the packages that stand on them
# (encoding/json, regexp, text/template, bufio, flag, log, ...), through
# graalgo-tool on the node package against gc. See test/m1d/run.sh;
# the M1D-* mutants of tools/mutation/mutants.json plant bugs the gate must
# catch (`make mutation`, nightly). M1D_PROGRAMS narrows the run.
test-m1d: build
	GO="$(GO)" test/m1d/run.sh $(M1D_PROGRAMS)
M1D_PROGRAMS ?=

# The differential fuzzer of the new path (not per-push; nightly
# fuzz-newpath.yml): test/fuzz/run.py builds every program of the
# hand-written corpus (test/fuzz/corpus/, test/fuzz/repro/) and N programs
# generated by test/fuzz/gen.py from seeds SEED..SEED+N-1 with gc and through
# graalgo-tool, and compares them; a divergence not listed in
# test/fuzz/known.txt fails. FUZZ_ARGS adds run.py flags (e.g. --minimize DIR,
# --keep DIR, -v); FUZZ_CORPUS=0 runs only the generated programs.
fuzz-newpath: build
	python3 test/fuzz/test_fuzz.py
	GO="$(GO)" python3 test/fuzz/run.py $(if $(filter 0,$(FUZZ_CORPUS)),,--corpus) --gen $(N) --seed $(SEED) $(FUZZ_ARGS)
N ?= 200
SEED ?= 1
FUZZ_CORPUS ?= 1
FUZZ_ARGS ?=

# The pointer-model gate (toolexec W13; SPEC §3.4, GG-0072/GG-0062, ADR-0015;
# ~1.5 min, per-push): programs that use package unsafe, through graalgo-tool
# on the node package against gc — the generated load/store program over every
# byte offset of a corpus of struct types per addressable kind, unsafe idioms
# of real code, gc's own -d=checkptr fatal errors, and the documented
# restrictions. See test/m1w13/run.sh. M1W13_PROGRAMS narrows the run.
test-m1w13: build
	GO="$(GO)" test/m1w13/run.sh $(M1W13_PROGRAMS)
M1W13_PROGRAMS ?=

# The descriptor-image gate (toolexec W9a; toolexec-design §3.4/§5.6,
# abi-spike §1.4; ~15 s, per-push): one Go program prints every fact of the
# type descriptors of a corpus of types, read through unsafe as
# internal/reflectlite reads them — built by gc (the compiler's descriptors;
# the walker itself checked against package reflect) and through graalgo-tool
# on the node package (the images graalgo/go/types/Descriptors.java builds).
# The outputs must be identical, and five seeded builder errors must each be
# detected. See test/imgcheck/run.sh. IMGCHECK_ARGS: -v, --no-mutants.
test-imgcheck: build
	GO="$(GO)" test/imgcheck/run.sh $(IMGCHECK_ARGS)
IMGCHECK_ARGS ?=

# The GOROOT a graalgo distribution ships (docs/arch/toolexec-design.md
# §5.1): the upstream distribution of $(GO) with the fork of
# [stdlib/](stdlib/) applied to a copy of its src, assembled at GOROOT_OUT
# by graalgo-goroot ([internal/toolexec/fork](internal/toolexec/fork)),
# which also prints the fork-size audit. Upstream is never modified, and an
# existing GOROOT_OUT is never overwritten: the tool refuses it.
GOROOT_OUT ?= $(CURDIR)/out/goroot
goroot:
	cd internal/toolexec && PATH="$(dir $(GO)):$$PATH" $(GO) run ./cmd/graalgo-goroot \
		-stdlib "$(CURDIR)/stdlib" -upstream "$$($(GO) env GOROOT)" -o "$(GOROOT_OUT)" -audit

# Gate 1: go test ./... trial harness (Phase-3 Gate 1).
#   make gate1-pilot  — 5-package pilot (strconv, errors, sort, strings, bytes)
#   make gate1 PKG=foo — single package trial
GATE1_PKGS ?= strconv errors sort strings bytes
# Runs every package (a failure does not stop the loop), then fails if any
# package failed: a red pilot is never reported as complete.
gate1-pilot: build
	@failed=""; for pkg in $(GATE1_PKGS); do \
		echo "=== gate1 $$pkg ==="; \
		TMPDIR="$(CURDIR)/tools/gate1/.tmp" GOTMPDIR="$(CURDIR)/tools/gate1/.tmp" \
		python3 tools/gate1/run.py $$pkg --out tools/gate1/.tmp/$$pkg \
			--graalgo-run "$(CURDIR)/internal/backend/graalgo-run" || failed="$$failed $$pkg"; \
	done; \
	echo "Gate 1 pilot done. See tools/gate1/.tmp/*/verdict.txt"; \
	if [ -n "$$failed" ]; then echo "GATE1 FAILED:$$failed"; exit 1; fi

gate1: build
	@test -n "$(PKG)" || (echo "usage: make gate1 PKG=<pkg>"; exit 1)
	TMPDIR="$(CURDIR)/tools/gate1/.tmp" GOTMPDIR="$(CURDIR)/tools/gate1/.tmp" \
	python3 tools/gate1/run.py $(PKG) --out tools/gate1/.tmp/$(PKG) \
		--graalgo-run "$(CURDIR)/internal/backend/graalgo-run"

# Full test suite: every class the module runners register (the union of
# the fast and slow lists below, enforced by java-test-lists-check), each on
# its module's correct classpath, with the slow half judged against
# test/java/quarantine.txt. LONG; weekly CI (full-suite), not per-push.
# It used to run the module runners directly (graalgo.test.Runner on a
# classpath WITHOUT interop's ServiceLoader registrations, no quarantine):
# every fmt-using corpus program failed there with "unsupported expression
# kind ifacemethod" while passing under test-java-slow.
test: test-java-fast test-java-slow

# Backend Java unit tests, split by measured cost (SPEC §5.10: fast→slow).
# The module runners (graalgo.test.Runner, SchedTestRunner, IfaceTestRunner)
# run everything in `make test` (weekly). Here
# [test/java/ClassListRunner.java](test/java/ClassListRunner.java) runs named
# classes with each module's own classpath and per-test timing:
#   test-java-fast  per-push (in `make all`): classes measured fast
#   test-java-slow  nightly/weekly: the corpus differentials and stress tests
# Split measured 2026-11-18 (make test-java-timing, nice 19, -Xmx2g): fast
# lists total ~30 s of test time (classes <= ~4 s each); slow lists hold the
# classes > ~4 s. Known failures live ONLY in
# [test/java/quarantine.txt](test/java/quarantine.txt) (class | reason |
# owner | expiry): test-java-slow fails on any failure outside it and on a
# quarantined class that now passes (quarantine_verdict.py). The truffle
# slow list runs on JT_RUNTIME_CP: its corpus differentials run whole Go
# programs, which need interop's ServiceLoader registrations.
# Re-measure with `make test-java-timing` (all classes, timings printed)
# before moving a class between lists. A class missing from both lists is
# caught by `java-test-lists-check` (every class registered in a module
# runner must be in exactly one list).
JT_BK := $(CURDIR)/internal/backend
JT_JARS := $(subst $(eval) ,:,$(strip $(wildcard $(JT_BK)/truffle/lib/*.jar)))
JT_RUNNER := $(CURDIR)/test/java/out
JT_TRUFFLE_CP := $(JT_RUNNER):$(JT_BK)/truffle/out/test-classes:$(JT_BK)/truffle/out/classes:$(JT_JARS)
# The runtime/scheduler lists run whole Go programs: interop's ServiceLoader
# registrations (wire kinds, OS surface) must be on the run-time classpath.
JT_RUNTIME_CP := $(JT_TRUFFLE_CP):$(JT_BK)/interop/out/classes
JT_INTEROP_CP := $(JT_RUNNER):$(JT_BK)/interop/out/test-classes:$(JT_BK)/interop/out/classes:$(JT_BK)/truffle/out/classes:$(JT_JARS)
JT_JAVA := $(or $(GRAALVM_HOME),$(HOME)/tools/graalvm-jdk-25)/bin/java
JT_FLAGS := --enable-native-access=ALL-UNNAMED --add-exports java.base/jdk.internal.vm=ALL-UNNAMED -Xss32m $(JAVA_TEST_OPTS)
JAVA_TEST_OPTS ?=
# RangeConcurrentRegressionTest runs P3SyncMain 50 times in one JVM and the
# live set grows ~130 MB per run (measured: 6.4 GB after 50, G1 evacuation
# failures at 4 GB, "internal error building AST: Java heap space"): it gets
# its own cap, above CI's global JAVA_TEST_OPTS one (a cap only: the JVM uses
# what it needs). The retention is v1's launcher by design (BUG-0051): P3SyncMain
# never closes its context, so each run's carriers, drainer and timer threads
# stay alive and hold the whole run; v1 is deleted at M3, and the new path's
# context lifecycle is gated by test/m1r/leak.sh (BUG-0052).
JT_INTEROP_SLOW_OPTS ?= -Xmx8g
# run_java_tests <module> <classpath> <classes> <extra JVM flags>; the extra
# flags come AFTER JAVA_TEST_OPTS so a call can override a global heap cap.
define run_java_tests
	cd $(JT_BK)/$(1) && mkdir -p out/test-tmp && \
	  TMPDIR="$(JT_BK)/$(1)/out/test-tmp" GOTMPDIR="$(JT_BK)/$(1)/out/test-tmp" \
	  GRAALGO_GOFRONTEND="$(CURDIR)/internal/frontend/gofrontend" \
	  $(JT_JAVA) -Djava.io.tmpdir="$(JT_BK)/$(1)/out/test-tmp" $(JT_FLAGS) $(4) \
	  -cp "$(2)" ClassListRunner $(3)
endef
JT_TRUFFLE_PROPS := -Dgraalgo.repo.dir="$(CURDIR)"
JAVA_FAST_TRUFFLE := graalgo.frontend.SectionClampTest graalgo.values.ValuesTest graalgo.values.CallSiteProbesTest graalgo.values.CellIdentityTest graalgo.runtime.FreezeSignatureTest graalgo.frontend.WrapInterfaceTest graalgo.frontend.BoxingArrayStorageTest graalgo.frontend.ArrayValueSemanticsTest graalgo.frontend.AtomicCellFieldAddrTest graalgo.frontend.CommaOkLvalueTest graalgo.frontend.v2.BinaryDecoderTest graalgo.frontend.ComplexBinaryOpTest graalgo.runtime.SliceGrowthTest graalgo.frontend.FrontendRobustnessTest graalgo.frontend.StraightLineCorpusTest graalgo.frontend.ArithmeticCorpusTest graalgo.frontend.ControlFlowCorpusTest graalgo.frontend.CompositeCorpusTest graalgo.go.types.LeafTableTest graalgo.go.runtime.HandlesTest graalgo.go.nodes.KeyOpsTest graalgo.go.nodes.EntryPollTest graalgo.go.nodes.SourcesTest graalgo.go.intrinsics.BytealgKernelTest graalgo.conventions.NodeConventionsTest
JAVA_SLOW_TRUFFLE := graalgo.launcher.MainTest graalgo.frontend.PanicCorpusTest graalgo.frontend.GenericsCorpusTest graalgo.frontend.VarsCorpusTest graalgo.runtime.BuiltinsCorpusTest graalgo.frontend.TypeparamDifferentialTest
JAVA_FAST_RUNTIME := graalgo.jfr.JfrRegistrationTest graalgo.sched.audit.PinAuditTest graalgo.sched.SchedulerCoreTest graalgo.sched.LifecycleTest graalgo.sched.ChannelTest graalgo.runtime.SelectSelfClaimTest graalgo.sched.ChannelDifferentialTest graalgo.sched.IoSlotsTest graalgo.sched.TimerDifferentialTest graalgo.runtime.FreezeGapTest graalgo.runtime.ZeroSizeSliceTest graalgo.runtime.StructSizeNestedTest graalgo.runtime.GcInteropTest graalgo.runtime.GcVisibilityTest graalgo.jfr.JfrEventsTest graalgo.runtime.sync.SyncStressTest graalgo.runtime.sync.GoPoolTest graalgo.sched.WakeupModelTest graalgo.sched.WakeupRaceStressTest graalgo.sched.CarriersTest graalgo.syscall.BlockingCallTest graalgo.sched.JfrParkCostTest
JAVA_SLOW_RUNTIME := graalgo.sched.launcher.GoDebugTest graalgo.launcher.LauncherOsArgsTest graalgo.runtime.TimerTest graalgo.runtime.GcDifferentialTest graalgo.runtime.GcTortureTest
JAVA_FAST_INTEROP := graalgo.p3sync.launcher.PolyglotEmbeddingTest graalgo.iface.IfaceDispatchNodeTest graalgo.frontend.DescriptorNamesTest graalgo.iface.LinknameTest graalgo.iface.SyncMapTest graalgo.iface.ItabForIntrinsicTest graalgo.iface.AtomicContentionTest graalgo.p3time.TimeChanLenCapTest graalgo.p3reflect.ReflectTypesTest graalgo.p3link.CondIsolationTest graalgo.rtmemstats.RtMemStatsTest graalgo.frontend.RtMemStatsBuilderTest graalgo.p3unsafe.UnsafeDeriveTest graalgo.p3unsafe.HeaderViewTest graalgo.p3unsafe.PrefixViewTest graalgo.p3unsafe.PrefixParentTest graalgo.p3reflect.AbiNameEncodingTest graalgo.p3reflect.GcMaskTest graalgo.p3reflect.MapKeyHashTest graalgo.iface.IfaceMethodTablesTest graalgo.frontend.ChanBuilderTest graalgo.p3os.GoOsErrnoTest graalgo.p3sync.launcher.StackDepthLimitTest graalgo.p3sync.launcher.CallPathWarmupTest
JAVA_SLOW_INTEROP := graalgo.p3sync.launcher.LinknameEndToEndTest graalgo.p3sync.launcher.RangeConcurrentRegressionTest

java-test-classes: build
	$(MAKE) -C internal/backend/truffle out/test-classes/.built
	$(MAKE) -C internal/backend/interop out/test-classes/.built
	mkdir -p $(JT_RUNNER) && $(dir $(JT_JAVA))javac -Werror -d $(JT_RUNNER) test/java/ClassListRunner.java

# Every class a module runner registers must be in exactly one list.
JAVA_QUARANTINE := test/java/quarantine.txt
JAVA_SLOW_ALL = $(JAVA_SLOW_TRUFFLE) $(JAVA_SLOW_RUNTIME) $(JAVA_SLOW_INTEROP)
java-test-lists-check:
	python3 test/java/check_lists.py --quarantine $(JAVA_QUARANTINE) --slow "$(JAVA_SLOW_ALL)" \
	  internal/backend/truffle/src/test/java/graalgo/test/Runner.java "$(JAVA_FAST_TRUFFLE) $(JAVA_SLOW_TRUFFLE)" \
	  internal/backend/truffle/src/test/java/graalgo/sched/SchedTestRunner.java "$(JAVA_FAST_RUNTIME) $(JAVA_SLOW_RUNTIME)" \
	  internal/backend/interop/src/test/java/graalgo/iface/IfaceTestRunner.java "$(JAVA_FAST_INTEROP) $(JAVA_SLOW_INTEROP)"

test-java-fast: java-test-classes java-test-lists-check
	$(call run_java_tests,truffle,$(JT_TRUFFLE_CP),$(JAVA_FAST_TRUFFLE),$(JT_TRUFFLE_PROPS))
	$(call run_java_tests,truffle,$(JT_RUNTIME_CP),$(JAVA_FAST_RUNTIME),)
	$(call run_java_tests,interop,$(JT_INTEROP_CP),$(JAVA_FAST_INTEROP),)

# The goroutine-mode matrix (GG-0074; per-push in `make all`). Carrier mode is
# the default, so every other gate runs it; this target keeps the opt-in
# thread-per-goroutine mode honest and checks what only a mode switch or
# a carrier migration can break: (1) a few goroutine-heavy m1a/m1r/m1c
# programs with -Dgraalgo.sched.goroutines=thread — the scheduler suites run
# in thread mode in test-pin-smoke: every class SchedTestRunner registers, a
# superset of the four this target ran too until 2026-11-25 (LifecycleTest,
# ChannelTest, WakeupRaceStressTest, JfrEventsTest), the same JVM mode;
# (2) goroutine programs in carrier mode under -ea, so Truffle's per-thread assertions
# (EncapsulatingNodeReference) see goroutines migrate between carriers;
# (3) test/m1r/cancel.sh: Context.close(true) of a running program, bounded
# and leak-free, in both modes; (4) test/m1r/leak.sh: successive contexts in
# one JVM leave no thread and no live heap behind (BUG-0052), in both modes;
# (5) test/m1r/batch.sh: the planted hazards of the gates' batch runner
# (test/lib/batchrun.sh: every m1* gate runs its graalgo legs in one JVM).
THREAD_MODE_M1A := test/m1a/programs/concurrency.go test/m1a/programs/goroutine_panic.go test/m1a/programs/traceback_goroutine.go
THREAD_MODE_M1R := test/m1r/programs/preempt_spin.go test/m1r/programs/select_send_races.go test/m1r/programs/carrier_migration.go test/m1r/programs/callers.go
THREAD_MODE_M1C := test/m1c/programs/sync_contention.go test/m1c/programs/time_timers.go
THREAD_MODE_OPTS = $(GRAALGO_JAVA_OPTS) -Dgraalgo.sched.goroutines=thread
test-goroutine-modes: build java-test-classes
	GRAALGO_JAVA_OPTS="$(THREAD_MODE_OPTS)" GO="$(GO)" test/m1a/run.sh $(THREAD_MODE_M1A)
	GRAALGO_JAVA_OPTS="$(THREAD_MODE_OPTS)" GO="$(GO)" test/m1r/run.sh $(THREAD_MODE_M1R)
	GRAALGO_JAVA_OPTS="$(THREAD_MODE_OPTS)" GO="$(GO)" test/m1c/run.sh $(THREAD_MODE_M1C)
	GRAALGO_JAVA_OPTS="$(GRAALGO_JAVA_OPTS) -ea" GO="$(GO)" test/m1r/run.sh test/m1r/programs/carrier_migration.go $(THREAD_MODE_M1A)
	GO="$(GO)" test/m1r/cancel.sh
	GO="$(GO)" test/m1r/tools.sh carrier,thread
	GO="$(GO)" test/m1r/leak.sh
	GO="$(GO)" test/m1r/batch.sh

# Each module's run is logged (exit status ignored: a JVM crash or INFRA
# exit shows up as a class with no CLASS line); quarantine_verdict.py then
# decides. Logs live under the gitignored out/ dirs. Each run is also
# streamed (tee) so a hang or a lost runner leaves its progress in the CI
# log instead of nothing (the CLASS lines carry per-class timings).
JT_SLOW_LOGS := $(JT_BK)/truffle/out/test-tmp/java-slow-truffle.log $(JT_BK)/truffle/out/test-tmp/java-slow-runtime.log $(JT_BK)/interop/out/test-tmp/java-slow-interop.log
test-java-slow: java-test-classes java-test-lists-check
	rm -f $(JT_SLOW_LOGS)
	-$(call run_java_tests,truffle,$(JT_RUNTIME_CP),$(JAVA_SLOW_TRUFFLE),$(JT_TRUFFLE_PROPS)) 2>&1 | tee $(word 1,$(JT_SLOW_LOGS))
	-$(call run_java_tests,truffle,$(JT_RUNTIME_CP),$(JAVA_SLOW_RUNTIME),) 2>&1 | tee $(word 2,$(JT_SLOW_LOGS))
	-$(call run_java_tests,interop,$(JT_INTEROP_CP),$(JAVA_SLOW_INTEROP),$(JT_INTEROP_SLOW_OPTS)) 2>&1 | tee $(word 3,$(JT_SLOW_LOGS))
	python3 test/java/quarantine_verdict.py $(JAVA_QUARANTINE) "$(JAVA_SLOW_ALL)" $(JT_SLOW_LOGS)

test-java-timing: java-test-classes
	-$(call run_java_tests,truffle,$(JT_TRUFFLE_CP),$(JAVA_FAST_TRUFFLE),$(JT_TRUFFLE_PROPS))
	-$(call run_java_tests,truffle,$(JT_RUNTIME_CP),$(JAVA_SLOW_TRUFFLE),$(JT_TRUFFLE_PROPS))
	-$(call run_java_tests,truffle,$(JT_RUNTIME_CP),$(JAVA_FAST_RUNTIME) $(JAVA_SLOW_RUNTIME),)
	-$(call run_java_tests,interop,$(JT_INTEROP_CP),$(JAVA_FAST_INTEROP) $(JAVA_SLOW_INTEROP),)

# Behavior snapshots (SPEC §5.9, GG-0036). The committed snapshot
# [test/snapshots/](test/snapshots/)go<VERSION>.json records gc's (the
# REFERENCE toolchain's) observable behavior on the differential corpus
# ([test/differential/corpus/](test/differential/corpus/)), content-addressed
# by program source hash. graalgo is checked against gc's snapshot; its own
# output is never the baseline. See
# [test/snapshots/README.md](test/snapshots/README.md).
#
# `make snapshot` RE-RECORDS gc's behavior: run when a corpus program is
# added/edited or for a toolchain upgrade (then triage any gc drift), and
# commit the result. It never needs graalgo.
#
# `make snapshot-check` (per-push, via `make all`) runs every corpus program
# on the new path (m3-plan D1: backends-newpath.json through
# test/differential/newpath.sh — each program built by `go build -toolexec
# graalgo-tool` with the stdlib fork, the built programs run in one JVM, a
# fresh context each; GRAALGO_BATCH=off: one JVM per program) and compares
# with the gc snapshot. Expected divergences are explicit, reviewed entries
# with a SPEC §5.6 reason code in
# [test/differential/corpus-exclusions.yaml](test/differential/corpus-exclusions.yaml);
# an expected divergence that now matches gc FAILS (stale entry: prune it).
# `-verify-ref gc-a` also re-runs gc and fails if gc no longer matches its
# own snapshot. Exit 3 = infrastructure failure (no verdict).
# Slice: `make snapshot-check SNAPSHOT_PROGRAMS=49_int_arith,50_shifts`.
DH := test/differential
SNAPSHOT_TOOLCHAIN := $(shell $(GO) version 2>/dev/null | awk '{print $$3}')
# /tmp may be a small shared tmpfs: keep go build temps and harness scratch
# dirs on the persistent disk. $(DH)/.tmp is gitignored.
SNAPSHOT_TMP := $(CURDIR)/$(DH)/.tmp
# SNAPSHOT_JOBS: programs built at a time, and batch JVMs (each runs a
# slice of the corpus). Sized to the machine, capped at 4 (CI runners have
# 4 vCPUs). The per-run timeout is 120s. Shared hosts: run with
# SNAPSHOT_JOBS=1 GRAALGO_JAVA_OPTS="-Xmx2g -XX:ActiveProcessorCount=4".
SNAPSHOT_JOBS ?= $(shell n=$$(nproc 2>/dev/null || echo 4); [ $$n -gt 4 ] && echo 4 || echo $$n)
SNAPSHOT_PROGRAMS ?=
# DH_GO pins the gc reference to $(GO)'s root (never whatever `go` is on
# PATH); snapshot_guard asserts both are the pinned release.
SNAPSHOT_GOROOT := $(abspath $(dir $(GO))..)
SNAPSHOT_PIN := $(shell cat tools/stdlib-fork/pinned-version.txt 2>/dev/null)
SNAPSHOT_ENV := TMPDIR="$(SNAPSHOT_TMP)" GOTMPDIR="$(SNAPSHOT_TMP)" \
	GRAALGO_BACKEND_DIR="$(CURDIR)/internal/backend" DH_GO="$(SNAPSHOT_GOROOT)"
define snapshot_guard
	@test "$(SNAPSHOT_TOOLCHAIN)" = "$(SNAPSHOT_PIN)" || { echo "SNAPSHOT: GO=$(GO) is '$(SNAPSHOT_TOOLCHAIN)', pinned reference is '$(SNAPSHOT_PIN)' (tools/stdlib-fork/pinned-version.txt)"; exit 3; }
	@test "$$($(SNAPSHOT_GOROOT)/bin/go version | awk '{print $$3}')" = "$(SNAPSHOT_PIN)" || { echo "SNAPSHOT: DH_GO=$(SNAPSHOT_GOROOT) is not $(SNAPSHOT_PIN)"; exit 3; }
endef
snapshot:
	$(snapshot_guard)
	mkdir -p $(SNAPSHOT_TMP) && cd $(DH) && $(SNAPSHOT_ENV) \
	  $(GO) run ./cmd/diffharness snapshot record \
	  -config backends-newpath.json -corpus corpus -backend gc-a \
	  -corpus-label test/differential/corpus \
	  -out ../../test/snapshots/$(SNAPSHOT_TOOLCHAIN).json \
	  -toolchain $(SNAPSHOT_TOOLCHAIN) -runs 3 -jobs $(SNAPSHOT_JOBS) -timeout 120

snapshot-check: build
	$(snapshot_guard)
	mkdir -p $(SNAPSHOT_TMP) && cd $(DH) && $(SNAPSHOT_ENV) GO="$(GO)" \
	  bash newpath.sh snapshot check \
	  -config backends-newpath.json -corpus corpus -backend graalgo \
	  -snapshot ../../test/snapshots/$(SNAPSHOT_TOOLCHAIN).json \
	  -expected corpus-exclusions.yaml -verify-ref gc-a \
	  $(if $(SNAPSHOT_PROGRAMS),-programs $(SNAPSHOT_PROGRAMS)) \
	  -jobs $(SNAPSHOT_JOBS) -timeout 120

# Oracle validation by mutation of the new path (SPEC §5.7, GG-0034;
# m3-plan G6): tools/mutation applies each curated mutant of
# tools/mutation/mutants.json (node package, runtime, scheduler,
# graalgo.syscall, the -toolexec emitter, the stdlib/ fork) to a scratch
# copy of the tree, rebuilds, and requires the per-push differential gates
# to go red on the mutant's own kill programs (snapshot-check corpus names,
# test/m1a, m1r, m1c, m1d, m1w13 programs); a slice survivor is re-run on
# every one of those gates in full. Exit 1 = unjustified survivor, 3 =
# infra error / red baseline. Nightly in .github/workflows/mutation.yml, not
# per-push (one rebuild per mutant); check-repo validates the set (anchors
# apply exactly once, kill programs exist). MUTANTS=ID,... narrows the run.
MUTATION_WORK ?=
# Programs/JVMs the snapshot gate runs at a time (1: shared host;
# mutation.yml's 4-vCPU runner passes 4).
MUTATION_JOBS ?= 1
mutation: build
	cd tools/mutation && GO="$(GO)" $(GO) run . -repo $(CURDIR) -jobs $(MUTATION_JOBS) \
	  $(if $(MUTATION_WORK),-work $(MUTATION_WORK)) $(if $(MUTANTS),-only $(MUTANTS))

# The standard-library corpus gate (SPEC §5.3 layer 3; m3-plan D3, which
# deleted v1's wave runners; manual: .github/workflows/stdlib-gate.yml): every
# test/corpus program that imports a package (486; the import-free ones run
# per push in test-m1a) through test/m1r/run.sh against gc, judged by
# test/corpus/std_gate.py against the reviewed register
# test/corpus/std-register.tsv (DRIFT, STALE and malformed entries fail).
# ~10 min on one job. STDLIB_GATE_REPORT is the per-program report.
STDLIB_GATE_REPORT ?= $(CURDIR)/.tmp/stdlib-gate.report
stdlib-gate: build
	mkdir -p $(dir $(STDLIB_GATE_REPORT)) && : > $(STDLIB_GATE_REPORT)
	st=0; M1R_REPORT="$(STDLIB_GATE_REPORT)" GO="$(GO)" test/m1r/run.sh \
	  $$(python3 test/corpus/std_gate.py list) || st=$$?; \
	  [ $$st -le 1 ] || exit $$st
	python3 test/corpus/std_gate.py judge $(STDLIB_GATE_REPORT)

# Fast Java debug cycle (see internal/backend/fast-cycle/fast-cycle).
# Uses a persistent warm JVM: in-process compilation (~0.5s) and Java test
# runs without JVM startup (~1s). (Its v1 `run`/`watch` of a Go file lost
# their targets at m3-plan D3; a Go program runs through graalgo-tool.)
#   make fast-build          incremental compile of changed files
#   make fast-test TEST=com.foo.Bar [METHOD=testX]  compile + run Java test
fast-build:
	internal/backend/fast-cycle/fast-cycle build

fast-test:
	@test -n "$(TEST)" || (echo "usage: make fast-test TEST=<class> [METHOD=<m>]"; exit 1)
	internal/backend/fast-cycle/fast-cycle test "$(TEST)" $(METHOD)

clean:
	$(MAKE) -C internal/backend/truffle clean
	$(MAKE) -C internal/backend/interop clean
