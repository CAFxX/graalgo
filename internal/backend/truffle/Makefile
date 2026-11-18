# graalgo backend — the core backend module (Truffle language + Go runtime)
#
# One javac module, one source root (src/main/java):
#   graalgo.frontend (+ .v2)  AST node classes and the v1/v2 node factories
#   graalgo.values            the Go value model (GoStruct, GoSlice, ...)
#   graalgo.runtime (+ .sync) runtime intrinsics: channels, panics, maps,
#                             timers, GC interop, sync primitives
#   graalgo.safepoint         the preemption poll
#   graalgo.sched (+ ...)     the user-space scheduler, its launcher
#                             (SchedMain), pin audit and JFR glue
#   graalgo.jfr               JFR event classes
#   graalgo.launcher          graalgo-run's entry point (Main) and the
#                             polyglot embedding helper (GoPolyglot)
#   graalgo.language          the Truffle language: GoLanguage (id "go"),
#                             GoContext, the program root (TR-1)
#
# The AST nodes and the runtime call each other (a genuine compile cycle),
# so they are one module compiled in one javac round. (Until T0.D,
# 2026-11-18, they were two modules plus 8 same-FQN phase-1 stub classes
# shadowed by classpath order; see docs/arch/RECOVERY.md.)
# The interop module (../interop) builds downstream against out/classes.
#
# Pinned toolchain: Oracle GraalVM 25.0.4.1.1 LTS (Truffle jars in lib/),
# JDK 25.0.4+7.1 at ~/tools/graalvm-jdk-25 (see lib/LIB.md + lib/SHA256SUMS.txt).
# Plain javac; no Maven/Gradle.
# Standing directive 2026-10-04: -Xlint:deprecation -Werror — any deprecated
# API use is a build break (code must survive the Phase-5 Innovation upgrade).

GRAALVM_HOME ?= $(HOME)/tools/graalvm-jdk-25
JAVAC := $(GRAALVM_HOME)/bin/javac
JAVA  := $(GRAALVM_HOME)/bin/java
GO    ?= $(HOME)/tools/go/bin/go
GO_BIN_DIR := $(dir $(GO))

LIB := $(wildcard lib/*.jar)
CP  := $(subst $(eval) ,:,$(strip $(LIB)))

SRC_MAIN := $(shell find src/main/java -name '*.java')
SRC_RES := $(shell find src/main/resources -type f 2>/dev/null)
SRC_TEST := $(shell find src/test/java -name '*.java')
SRC_SMOKE := $(shell find src/smoke -name '*.java')
OUT_MAIN := out/classes
OUT_TEST := out/test-classes
OUT_SMOKE := out/smoke-classes

RUN_CP := $(OUT_MAIN):$(CP)
TEST_CP := $(OUT_TEST):$(RUN_CP)
# The scheduler-side launchers and tests run whole Go programs, whose wire
# kinds the interop module registers through ServiceLoader
# (WireKindRegistrar, OsSurfaceInitializer): a run-time-only dependency on
# the downstream module, exactly like graalgo-run's classpath. Never on a
# javac classpath.
INTEROP_CLASSES := ../interop/out/classes
SCHED_RUN_CP := $(RUN_CP):$(INTEROP_CLASSES)
SCHED_TEST_CP := $(TEST_CP):$(INTEROP_CLASSES)

# gofrontend: the Go frontend service binary. Built from the vendored go/*
# tree via frontend/build.sh (overlay build; see frontend/VENDORING.md).
# The launcher locates the binary via GRAALGO_GOFRONTEND (exported by
# internal/backend/graalgo-run), else by walking up from its own classes
# dir to internal/frontend/.
GO_FRONTEND_DIR ?= ../../../internal/frontend
GOFRONTEND := $(GO_FRONTEND_DIR)/gofrontend
# P3-M7: the gofrontend binary embeds every non-test frontend/*.go source
# (synth_*.go, synth_machinery.go, ...). An explicit file list goes stale
# the moment a new synthetic package lands — a stale binary then silently
# tests the wrong sources (the errors/sort wave-1 packages hit this).
GOFRONTEND_SRC := $(wildcard $(GO_FRONTEND_DIR)/*.go) $(GO_FRONTEND_DIR)/build.sh $(GO_FRONTEND_DIR)/go.mod
$(GOFRONTEND): $(GOFRONTEND_SRC)
	cd $(GO_FRONTEND_DIR) && PATH="$(GO_BIN_DIR):$$PATH" ./build.sh build

# -Xss32m (M5 stance): guest-language recursion burns host stack — the
# panic corpus's 10000-deep unwind (pn12) needs ~13-16m (~1.3-1.6KB per
# nested call through Truffle's exception profiling); 32m is ~2x headroom.
JAVA_FLAGS := --enable-native-access=ALL-UNNAMED -Xss32m
# gofrontend location: ToolPaths' walk-up finds internal/frontend/ unaided;
# the explicit export is a harmless redundancy (a stale env var is skipped
# via the isExecutable check).
export GRAALGO_GOFRONTEND := $(CURDIR)/../../../internal/frontend/gofrontend
# Corpus differential tests locate the corpus via -Dgraalgo.repo.dir.
REPO_DIR_PROP := -Dgraalgo.repo.dir="$(CURDIR)/../../.."
JAVAC_FLAGS := -Xlint:deprecation -Werror

.PHONY: all main test smoke smoke-test clean check run run-sched pin-audit torture

all: main

main: $(OUT_MAIN)/.built $(GOFRONTEND)
$(OUT_MAIN)/.built: $(SRC_MAIN) $(SRC_RES) lib/SHA256SUMS.txt
	mkdir -p $(OUT_MAIN)
	$(JAVAC) -version
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(CP)" -processorpath "$(CP)" \
		-d $(OUT_MAIN) $(SRC_MAIN)
	@if [ -d src/main/resources ]; then \
		cp -r src/main/resources/. $(OUT_MAIN)/; fi
	touch $(OUT_MAIN)/.built

# toolchain smoke tests (Truffle runtime + DSL processor); green on the pin
smoke-test: $(OUT_SMOKE)/.built
	$(JAVA) $(JAVA_FLAGS) -cp "$(OUT_SMOKE):$(CP)" Smoke
	$(JAVA) $(JAVA_FLAGS) -cp "$(OUT_SMOKE):$(CP)" DslSmoke
$(OUT_SMOKE)/.built: $(SRC_SMOKE) lib/SHA256SUMS.txt
	mkdir -p $(OUT_SMOKE)
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(CP)" -processorpath "$(CP)" -d $(OUT_SMOKE) $(SRC_SMOKE)
	touch $(OUT_SMOKE)/.built

# graalgo-run: frontend + Truffle AST execution for one Go file.
run: main
	$(JAVA) $(JAVA_FLAGS) -cp "$(RUN_CP)" graalgo.launcher.Main $(ARGS)

# --gmp=N file.go — scheduler entry point (SchedMain).
run-sched: main
	$(JAVA) $(JAVA_FLAGS) -cp "$(SCHED_RUN_CP)" graalgo.sched.launcher.SchedMain $(ARGS)

# Both module runners: graalgo.test.Runner (AST/values/launcher tests and
# corpus differentials) and graalgo.sched.SchedTestRunner (scheduler and
# runtime tests).
test: main $(OUT_TEST)/.built
	mkdir -p out/test-tmp
	# /tmp is a 512M tmpfs: go builds and Java temp dirs in the differential
	# tests would fill it. GOTMPDIR covers go; -Djava.io.tmpdir covers Java
	# (Java ignores the TMPDIR env var for java.io.tmpdir).
	TMPDIR="$(CURDIR)/out/test-tmp" GOTMPDIR="$(CURDIR)/out/test-tmp" \
	$(JAVA) -Djava.io.tmpdir="$(CURDIR)/out/test-tmp" $(REPO_DIR_PROP) $(JAVA_FLAGS) \
	-cp "$(TEST_CP)" graalgo.test.Runner
	TMPDIR="$(CURDIR)/out/test-tmp" GOTMPDIR="$(CURDIR)/out/test-tmp" \
	$(JAVA) -Djava.io.tmpdir="$(CURDIR)/out/test-tmp" $(JAVA_FLAGS) \
	-cp "$(SCHED_TEST_CP)" graalgo.sched.SchedTestRunner
$(OUT_TEST)/.built: $(SRC_TEST) $(OUT_MAIN)/.built
	mkdir -p $(OUT_TEST)
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(RUN_CP)" -d $(OUT_TEST) $(SRC_TEST)
	touch $(OUT_TEST)/.built

# Grep gates (done-gate 5 discipline), the pin-audit smoke, and the
# gofrontend check (gofmt/vet/tests/vendoring diff gate).
#
# debug-leftover exclusions: the launchers (Main, SchedMain) print usage and
# diagnostics to stderr by design. SchedulerCore's panic/crash reporting and
# Drainer's fatal-halt message: printing "panic: ..." on an uncaught Go
# panic is Go parity (gc prints it), and a violated scheduler invariant must
# be LOUD before halting (P1-4: drainer death is never silent). TimerHeap's
# callback-threw message: a throwing timer callback must be loud. GcInterop's
# finalizer-panicked message: a panicking Go finalizer must be loud (gc
# crashes the program). SchedTrace's SCHED/SCHED-DETAIL lines and GcTrace's
# per-cycle lines ARE the GODEBUG=schedtrace/gctrace feature (P2-M5).
# PinAudit is a reporting tool.
check: main $(OUT_TEST)/.built
	@echo "== pinned-jar checksum gate (lib/LIB.md) =="
	@cd lib && sha256sum --quiet --strict -c SHA256SUMS.txt || { echo "PINNED-JAR GATE FAILED: checksum mismatch"; exit 1; }
	@cd lib && for j in *.jar; do grep -q "  $$j\$$" SHA256SUMS.txt || { echo "PINNED-JAR GATE FAILED: $$j not in SHA256SUMS.txt"; exit 1; }; done
	@echo "pinned-jar gate clean"
	@echo "== placeholder grep gate =="
	@if grep -rn --include='*.java' -E 'TODO|FIXME|XXX|panic\("not implemented"\)|throw new UnsupportedOperationException\("stub' src/; then \
		echo "PLACEHOLDER GATE FAILED"; exit 1; fi
	@echo "placeholder gate clean"
	@echo "== debug-leftover grep gate (src/main only; launcher output is legitimate) =="
	@if grep -rn --include='*.java' -E 'System\.(out|err)\.println' src/main/ | grep -v 'src/main/java/graalgo/launcher/Main.java' | grep -v 'src/main/java/graalgo/sched/launcher/SchedMain.java' | grep -v 'src/main/java/graalgo/sched/SchedulerCore.java' | grep -v 'src/main/java/graalgo/sched/Drainer.java' | grep -v 'src/main/java/graalgo/runtime/TimerHeap.java' | grep -v 'src/main/java/graalgo/runtime/GcInterop.java' | grep -v 'src/main/java/graalgo/sched/SchedTrace.java' | grep -v 'src/main/java/graalgo/sched/launcher/GcTrace.java' | grep -v 'src/main/java/graalgo/sched/audit/PinAudit.java'; then \
		echo "DEBUG-LEFTOVER GATE FAILED"; exit 1; fi
	@echo "debug-leftover gate clean"
	@echo "== netpoller-deletion gate (GG-0018, P2-M3; updated P3-M9) =="
	@# P3-M9: the netpoller was re-integrated (interop graalgo.p3net.GoNetpoll), so
	@# the P2-M3 deletion premise no longer holds. This disjunct now guards against
	@# *code* references to netpoller paths in this module; the ParkReason.java javadoc
	@# {@code GoNetpoll.pollWait} is documentation of the real interop-module API,
	@# not a code path, and is excluded.
	@if grep -rn --include='*.java' -E 'Netpoll\.' src/ | grep -v 'src/main/java/graalgo/runtime/Netpoll.java' | grep -v 'src/test/java' | grep -v 'src/main/java/graalgo/jfr/ParkReason.java'; then \
		echo "NETPOLLER GATE FAILED: reachable netpoller code path"; exit 1; fi
	@# Exclusion: ToolPaths (launcher, startup-only, never goroutine-reachable)
	@# imports java.net.URISyntaxException for code-source URI parsing — not
	@# network I/O.
	@if grep -rn --include='*.java' -E 'java\.nio\.channels|java\.net\.' src/main/ | grep -v 'java\.net\.URISyntaxException'; then \
		echo "NETPOLLER GATE FAILED: NIO bypassing slot discipline (GG-0018 second disjunct)"; exit 1; fi
	@echo "netpoller-deletion gate clean"
	@echo "== off-heap/barrier gate (ADR-0002 D3/F3) =="
	@echo "-- each pattern guards a way Go references could bypass JVM GC barriers;"
	@echo "-- allocateMemory/MemorySegment/Arena/allocateDirect: off-heap Go-object storage"
	@echo "-- theUnsafe/reflective Unsafe: raw-address access outside barrier discipline"
	@echo "-- sun.misc/jdk.internal imports + fully-qualified sun.misc.Unsafe:"
	@echo "--   unvetted barrier-bypass surface (incl. Class.forName evasion)"
	@# Exclusion: PanicCorpusTest mentions sun.misc.Unsafe in javadoc only
	@# (it filters the JDK's Unsafe deprecation warning out of stderr).
	@if grep -rn --include='*.java' -E 'allocateMemory|MemorySegment|Arena|theUnsafe|allocateDirect|import sun\.misc\.|import jdk\.internal|sun\.misc\.Unsafe|Class\.forName\("sun' src/ | grep -v 'src/test/java/graalgo/frontend/PanicCorpusTest.java'; then \
		echo "OFF-HEAP/BARRIER GATE FAILED"; exit 1; fi
	@echo "off-heap/barrier gate clean"
	@echo "== pin-surface grep gate (P2-M7, GG-0057) =="
	@echo "-- no JNI/FFM on goroutine-reachable paths: a native method or an"
	@echo "-- FFM downcall on a goroutine vthread is the only remaining"
	@echo "-- pinning class on JDK 25 (JEP 491 retired synchronized pinning)."
	@echo "-- Any new native/FFM surface must be allowlisted in the PinAudit"
	@echo "-- budget or this gate fails."
	@if grep -rn --include='*.java' -E '^[[:space:]]*(public|private|protected|static|final|synchronized|strictfp|[[:space:]])*native[[:space:]]' src/main/; then \
		echo "PIN-SURFACE GATE FAILED: native method declaration"; exit 1; fi
	@if grep -rn --include='*.java' 'java\.lang\.foreign' src/main/; then \
		echo "PIN-SURFACE GATE FAILED: FFM usage"; exit 1; fi
	@echo "pin-surface gate clean"
	@echo "== pin-audit smoke (P2-M7, GG-0057) =="
	@echo "-- runs the scheduler/runtime suite in a child JVM with jdk.VirtualThreadPinned"
	@echo "-- enabled; FAILS on any unexpected pin. Full corpus: make pin-audit."
	mkdir -p out/test-tmp
	TMPDIR="$(CURDIR)/out/test-tmp" GOTMPDIR="$(CURDIR)/out/test-tmp" \
	$(JAVA) --enable-native-access=ALL-UNNAMED -Xss32m \
	-Dpinaudit.test.cp="$(SCHED_TEST_CP)" \
	-cp "$(SCHED_RUN_CP)" graalgo.sched.audit.PinAudit --tests --gmp 2 \
	> out/test-tmp/pin-smoke.log 2>&1; \
	rc=$$?; tail -40 out/test-tmp/pin-smoke.log; exit $$rc
	@# NB: the tail must cover the whole report + the CHILD_FAIL summary block
	@# PinAudit prints last on a failed tests leg (2026-11-04: the CI failure
	@# named no test because the tail showed only the verdict lines).
	@echo "== gofrontend check (gofmt/vet/tests/vendoring diff gate) =="
	cd $(GO_FRONTEND_DIR) && PATH="$(GO_BIN_DIR):$$PATH" ./build.sh check

# P2-M7 pinning audit (WS G, GG-0057): full conformance corpus + test
# suite under jdk.VirtualThreadPinned. Slow (~15 min); run explicitly and
# before milestone sign-off.
P1_CORPUS ?= ../../../test/corpus/phase1
P2_CORPUS ?= ../../../test/corpus/phase2
pin-audit: main $(OUT_TEST)/.built
	mkdir -p out/test-tmp
	TMPDIR="$(CURDIR)/out/test-tmp" GOTMPDIR="$(CURDIR)/out/test-tmp" \
	$(JAVA) --enable-native-access=ALL-UNNAMED -Xss32m \
	-Dpinaudit.test.cp="$(SCHED_TEST_CP)" \
	-cp "$(SCHED_RUN_CP)" graalgo.sched.audit.PinAudit \
	--corpus $(P1_CORPUS) --corpus $(P2_CORPUS) --tests --gmp 2 \
	--out out/pinaudit-report.md

# P2-M8 preemption-torture battery (spike 2b F1–F6 on the production path).
# Takes ~10 min.
torture: main $(OUT_TEST)/.built
	bash ../../../tools/torture/run_battery.sh

smoke:
	$(MAKE) run ARGS="$(P1_CORPUS)/hello.go"

clean:
	rm -rf out /tmp/smoke-classes /tmp/dsl-classes $(GOFRONTEND) $(GO_FRONTEND_DIR)/overlay.json
