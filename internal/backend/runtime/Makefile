# graalgo backend — runtime module build (Go runtime on the JVM)
#
# Own javac module: the merged runtime sources, relocated per the Phase-4
# repo restructure (lane 3):
#   internal/backend/runtime/src/main/java — phase-1 graalgo.{runtime
#     (minus the 7 frozen stubs),launcher,safepoint} (stub moved out) +
#     phase-2 graalgo.{sched,jfr,runtime,safepoint} (the scheduler, incl. the
#     same-FQN replacements that shadow the phase-1 stubs).
#
# COMPILE CYCLE (read before restructuring): the phase-1 AST sources in the
# truffle module (graalgo.frontend.*Node) call graalgo.runtime.* intrinsics,
# while graalgo.runtime/launcher/safepoint call back into graalgo.values/
# graalgo.frontend — 33 files one way, 11 the other. The two source trees
# cannot compile as separate javac rounds. This module therefore compiles
# the JOINT round: its own sources plus the truffle module's sources, in a
# single javac invocation. The truffle module's own main target then
# compiles only the 8 frozen stubs (against this module's classes); one
# javac round cannot hold two classes with the same fully-qualified name.
#
# Classpath order stays load-bearing: runtime/out/classes comes BEFORE
# truffle/out/classes, so the phase-2 "owns" replacements shadow the
# truffle module's stubs — if the stubs loaded instead, their loud
# UnsupportedOperationExceptions would fire (fail-fast, never silent). The
# stub-shadow gate below fails the build if any stub/replacement pair goes
# missing (a deleted replacement would otherwise silently re-expose a stub).
#
# Compile order: this module builds FIRST; truffle and interop build
# against its out/classes.
#
# Pinned toolchain: Oracle GraalVM 25.0.4.1.1 LTS, plain javac,
# -Xlint:deprecation -Werror (standing directive 2026-10-04).

GRAALVM_HOME ?= $(HOME)/tools/graalvm-jdk-25
JAVAC := $(GRAALVM_HOME)/bin/javac
JAVA  := $(GRAALVM_HOME)/bin/java

TRUFFLE := ../truffle
TRUFFLE_CLASSES := $(TRUFFLE)/out/classes
INTEROP := ../interop
INTEROP_CLASSES := $(INTEROP)/out/classes
LIB := $(wildcard $(TRUFFLE)/lib/*.jar)
CP := $(INTEROP_CLASSES):$(subst $(eval) ,:,$(strip $(LIB)))

# The 8 frozen phase-1 stubs, as paths relative to a src/main/java root.
# Historical artifacts of the GG-0020 interface freeze: they compile in the
# truffle module (same-FQN phase-2 replacements compile here), and the
# runtime classpath order makes the replacements win at load time.
STUB_REL := graalgo/runtime/ChanOps.java graalgo/runtime/GcInterop.java \
  graalgo/runtime/GoroutineState.java graalgo/runtime/Netpoll.java \
  graalgo/runtime/Panic.java graalgo/runtime/Scheduler.java \
  graalgo/runtime/Timers.java graalgo/safepoint/Safepoint.java

SRC_MAIN := $(shell find src/main/java -name '*.java')
# Truffle-module sources for the joint round, minus the 8 stubs (compiled
# by the truffle module's own main target — see the cycle note above).
TRUFFLE_SRC := $(filter-out $(addprefix $(TRUFFLE)/src/main/java/,$(STUB_REL)),$(shell find $(TRUFFLE)/src/main/java -name '*.java'))
SRC_TEST := $(shell find src/test/java -name '*.java')
SRC_RES := $(shell find src/main/resources -type f 2>/dev/null)
OUT_MAIN := out/classes
OUT_TEST := out/test-classes

# Runtime classpath: this module's classes FIRST (overlay), then the truffle
# module's (the 8 stubs, shadowed), then jars.
RUN_CP := $(OUT_MAIN):$(TRUFFLE_CLASSES):$(CP)
TEST_CP := $(OUT_TEST):$(RUN_CP)

JAVA_FLAGS := --enable-native-access=ALL-UNNAMED -Xss32m
# BRIDGE (frontend lane): subprocess tests (GcDifferentialTest, GoDebugTest)
# resolve gofrontend via ToolPaths' walk-up, which no longer reaches
# phases/phase1/frontend from the new class locations. Export the current
# build location until the frontend lane moves it to internal/frontend/
# (the walk-up then finds it unaided; a stale env var is skipped via the
# isExecutable check).
export GRAALGO_GOFRONTEND := $(CURDIR)/../../../internal/frontend/gofrontend
JAVAC_FLAGS := -Xlint:deprecation -Werror

.PHONY: all main test check clean run

all: main

main: $(OUT_MAIN)/.built
$(OUT_MAIN)/.built: $(SRC_MAIN) $(TRUFFLE_SRC) $(SRC_RES)
	@for s in $(STUB_REL); do \
	  [ -f $(TRUFFLE)/src/main/java/$$s ] || { echo "STUB-SHADOW GATE FAILED: stub missing: $$s"; exit 1; }; \
	  [ -f src/main/java/$$s ] || { echo "STUB-SHADOW GATE FAILED: replacement missing: $$s"; exit 1; }; \
	done
	mkdir -p $(OUT_MAIN)
	$(JAVAC) -version
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(CP)" -processorpath "$(CP)" \
		-d $(OUT_MAIN) $(SRC_MAIN) $(TRUFFLE_SRC)
	@if [ -d src/main/resources ]; then \
		cp -r src/main/resources/. $(OUT_MAIN)/; fi
	touch $(OUT_MAIN)/.built

# graalgo-run --gmp=N file.go — scheduler entry point (SchedMain).
run: main
	$(JAVA) $(JAVA_FLAGS) -cp "$(RUN_CP)" graalgo.sched.launcher.SchedMain $(ARGS)

test: main $(OUT_TEST)/.built
	mkdir -p out/test-tmp
	TMPDIR="$(CURDIR)/out/test-tmp" GOTMPDIR="$(CURDIR)/out/test-tmp" \
	$(JAVA) -Djava.io.tmpdir="$(CURDIR)/out/test-tmp" $(JAVA_FLAGS) \
	-cp "$(TEST_CP)" graalgo.sched.SchedTestRunner
$(OUT_TEST)/.built: $(SRC_TEST) $(OUT_MAIN)/.built
	mkdir -p $(OUT_TEST)
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(OUT_MAIN):$(TRUFFLE_CLASSES):$(CP)" -d $(OUT_TEST) $(SRC_TEST)
	touch $(OUT_TEST)/.built

# placeholder + debug-leftover grep gates (Phase-1 done-gate 5 discipline).
# The launchers (SchedMain, and Main inherited from the phase-1 tree) are
# excluded from the debug gate: printing usage and diagnostics to stderr is
# their job. SchedulerCore's panic/crash reporting and Drainer's fatal-halt
# message are likewise excluded: printing "panic: ..." on an uncaught Go
# panic is Go parity (gc prints it), and a violated scheduler invariant must
# be LOUD before halting (P1-4: drainer death is never silent). These are
# production diagnostics, not debug leftovers. TimerHeap's
# callback-threw message is excluded on the same grounds: a throwing
# timer callback must be loud, and letting it kill the timer thread
# silently would stall every timer with no diagnostic.
# GcInterop's finalizer-panicked message is excluded on the same grounds:
# a panicking Go finalizer must be loud (gc crashes the program); letting
# the finalizer thread die silently would lose the diagnostic.
# SchedTrace's SCHED/SCHED-DETAIL lines and GcTrace's per-cycle lines are
# excluded on the same grounds: they ARE the GODEBUG=schedtrace/gctrace
# feature (P2-M5); the user explicitly asked for stderr diagnostics.
check: main $(OUT_TEST)/.built
	@echo "== placeholder grep gate =="
	@if grep -rn --include='*.java' -E 'TODO|FIXME|XXX|panic\("not implemented"\)|throw new UnsupportedOperationException\("stub' src/; then \
		echo "PLACEHOLDER GATE FAILED"; exit 1; fi
	@echo "placeholder gate clean"
	@echo "== debug-leftover grep gate (src/main only; launcher output is legitimate) =="
	@if grep -rn --include='*.java' -E 'System\.(out|err)\.println' src/main/ | grep -v 'src/main/java/graalgo/launcher/Main.java' | grep -v 'src/main/java/graalgo/sched/launcher/SchedMain.java' | grep -v 'src/main/java/graalgo/sched/SchedulerCore.java' | grep -v 'src/main/java/graalgo/sched/Drainer.java' | grep -v 'src/main/java/graalgo/runtime/TimerHeap.java' | grep -v 'src/main/java/graalgo/runtime/GcInterop.java' | grep -v 'src/main/java/graalgo/sched/SchedTrace.java' | grep -v 'src/main/java/graalgo/sched/launcher/GcTrace.java' | grep -v 'src/main/java/graalgo/sched/audit/PinAudit.java'; then \
		echo "DEBUG-LEFTOVER GATE FAILED"; exit 1; fi
	@echo "debug-leftover gate clean"
	@echo "== netpoller-deletion gate (GG-0018, P2-M3; updated P3-M9) =="
	@# P3-M9: the netpoller was re-integrated (phase3 graalgo.p3net.GoNetpoll), so the
	@# P2-M3 deletion premise no longer holds. This disjunct now guards against
	@# *code* references to netpoller paths in this module; the ParkReason.java javadoc
	@# {@code GoNetpoll.pollWait} is documentation of the real interop-module API,
	@# not a code path, and is excluded.
	@if grep -rn --include='*.java' -E 'Netpoll\.' src/ | grep -v 'src/main/java/graalgo/runtime/Netpoll.java' | grep -v 'src/test/java' | grep -v 'src/main/java/graalgo/jfr/ParkReason.java'; then \
		echo "NETPOLLER GATE FAILED: reachable netpoller code path"; exit 1; fi
	@# Exclusion: ToolPaths (phase-1 launcher, startup-only, never
	@# goroutine-reachable) imports java.net.URISyntaxException for
	@# code-source URI parsing — not network I/O; the phase-1 tree never
	@# ran this gate.
	@if grep -rn --include='*.java' -E 'java\.nio\.channels|java\.net\.' src/main/ | grep -v 'java\.net\.URISyntaxException'; then \
		echo "NETPOLLER GATE FAILED: NIO bypassing slot discipline (GG-0018 second disjunct)"; exit 1; fi
	@echo "netpoller-deletion gate clean"
	@echo "== off-heap/barrier gate (ADR-0002 D3/F3) =="
	@echo "-- each pattern guards a way Go references could bypass JVM GC barriers;"
	@echo "-- allocateMemory/MemorySegment/Arena/allocateDirect: off-heap Go-object storage"
	@echo "-- theUnsafe/reflective Unsafe: raw-address access outside barrier discipline"
	@echo "-- sun.misc/jdk.internal imports + fully-qualified sun.misc.Unsafe:"
	@echo "--   unvetted barrier-bypass surface (incl. Class.forName evasion)"
	@if grep -rn --include='*.java' -E 'allocateMemory|MemorySegment|Arena|theUnsafe|allocateDirect|import sun\.misc\.|import jdk\.internal|sun\.misc\.Unsafe|Class\.forName\("sun' src/; then \
		echo "OFF-HEAP/BARRIER GATE FAILED"; exit 1; fi
	@echo "off-heap/barrier gate clean"
	@echo "== pin-surface grep gate (P2-M7, GG-0057) =="
	@echo "-- no JNI/FFM on goroutine-reachable paths: a native method or an"
	@echo "-- FFM downcall on a goroutine vthread is the only remaining"
	@echo "-- pinning class on JDK 25 (JEP 491 retired synchronized pinning)."
	@echo "-- Any new native/FFM surface must be allowlisted in the PinAudit"
	@echo "-- budget or this gate fails."
	@if grep -rn --include='*.java' -E '^[[:space:]]*(public|private|protected|static|final|synchronized|strictfp|[[:space:]])*native[[:space:]]' src/main/ ../truffle/src/main/; then \
		echo "PIN-SURFACE GATE FAILED: native method declaration"; exit 1; fi
	@if grep -rn --include='*.java' 'java\.lang\.foreign' src/main/ ../truffle/src/main/; then \
		echo "PIN-SURFACE GATE FAILED: FFM usage"; exit 1; fi
	@echo "pin-surface gate clean"
	@echo "== pin-audit smoke (P2-M7, GG-0057) =="
	@echo "-- runs the runtime-module suite in a child JVM with jdk.VirtualThreadPinned"
	@echo "-- enabled; FAILS on any unexpected pin. Full corpus: make pin-audit."
	mkdir -p out/test-tmp
	TMPDIR="$(CURDIR)/out/test-tmp" GOTMPDIR="$(CURDIR)/out/test-tmp" \
	$(JAVA) --enable-native-access=ALL-UNNAMED -Xss32m \
	-Dpinaudit.test.cp="$(TEST_CP)" \
	-cp "$(RUN_CP)" graalgo.sched.audit.PinAudit --tests --gmp 2 \
	> out/test-tmp/pin-smoke.log 2>&1; \
	rc=$$?; tail -3 out/test-tmp/pin-smoke.log; exit $$rc

# P2-M7 pinning audit (WS G, GG-0057): full conformance corpus + test
# suite under jdk.VirtualThreadPinned. Slow (~15 min); run explicitly and
# before milestone sign-off.
# BRIDGE (test lane): the corpora still live under phases/ until the test
# code-move lane relocates them to test/corpus/; repoint P1_CORPUS/P2_CORPUS.
P1_CORPUS ?= ../../../test/corpus/phase1
P2_CORPUS ?= ../../../test/corpus/phase2
pin-audit: main $(OUT_TEST)/.built
	mkdir -p out/test-tmp
	TMPDIR="$(CURDIR)/out/test-tmp" GOTMPDIR="$(CURDIR)/out/test-tmp" \
	$(JAVA) --enable-native-access=ALL-UNNAMED -Xss32m \
	-Dpinaudit.test.cp="$(TEST_CP)" \
	-cp "$(RUN_CP)" graalgo.sched.audit.PinAudit \
	--corpus $(P1_CORPUS) --corpus $(P2_CORPUS) --tests --gmp 2 \
	--out out/pinaudit-report.md

# P2-M8 preemption-torture battery (spike 2b F1–F6 on the production path).
# Takes ~10 min. BRIDGE (phase-2 tooling still under phases/phase2/tools).
torture: main $(OUT_TEST)/.built
	bash ../../../tools/torture/run_battery.sh

# P2-M8 GOMAXPROCS matrix (differential vs gc).
gmp-matrix: main $(OUT_TEST)/.built
	bash ../../../tools/torture/gmp_matrix.sh

clean:
	rm -rf out
