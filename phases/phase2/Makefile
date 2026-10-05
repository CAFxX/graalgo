# graalgo Phase 2 — scheduler core build (P2-M1, WS A)
#
# Own build module mirroring the Phase-1 layout (P2-D8). Phase 1 is consumed
# as a FROZEN dependency: this module compiles against phase1's out/classes
# and NEVER modifies phase1 sources (the single documented exception is the
# exporter's `go`-statement emission, a separate Phase-1 commit under the
# freeze change policy).
#
# Classpath order is load-bearing: phase2/out/classes comes FIRST so the
# three "Phase 2 owns" same-FQN replacements (graalgo.safepoint.Safepoint,
# graalgo.runtime.Scheduler, graalgo.runtime.GoroutineState) shadow the
# Phase-1 stubs. If phase1 classes came first, the stubs' loud
# UnsupportedOperationExceptions would fire — a fail-fast, never silent.
#
# Pinned toolchain: Oracle GraalVM 25.0.4.1.1 LTS, plain javac,
# -Xlint:deprecation -Werror (standing directive 2026-10-04).

GRAALVM_HOME ?= $(HOME)/tools/graalvm-jdk-25
JAVAC := $(GRAALVM_HOME)/bin/javac
JAVA  := $(GRAALVM_HOME)/bin/java

P1 := ../phase1
P1_CLASSES := $(P1)/out/classes
LIB := $(wildcard $(P1)/lib/*.jar)
CP := $(subst $(eval) ,:,$(strip $(LIB)))

SRC_MAIN := $(shell find src/main/java -name '*.java')
SRC_TEST := $(shell find src/test/java -name '*.java')
OUT_MAIN := out/classes
OUT_TEST := out/test-classes

# Phase-2 runtime classpath: phase2 FIRST (overlay), then phase1, then jars.
RUN_CP := $(OUT_MAIN):$(P1_CLASSES):$(CP)
TEST_CP := $(OUT_TEST):$(RUN_CP)

JAVA_FLAGS := --enable-native-access=ALL-UNNAMED -Xss32m
JAVAC_FLAGS := -Xlint:deprecation -Werror

.PHONY: all main test check clean run

all: main

# Phase 1 must be built first (frozen dependency).
$(P1_CLASSES)/.built:
	$(MAKE) -C $(P1) main

main: $(P1_CLASSES)/.built $(OUT_MAIN)/.built
$(OUT_MAIN)/.built: $(SRC_MAIN)
	mkdir -p $(OUT_MAIN)
	$(JAVAC) -version
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(P1_CLASSES):$(CP)" -processorpath "$(CP)" \
		-d $(OUT_MAIN) $(SRC_MAIN)
	touch $(OUT_MAIN)/.built

# graalgo-run --gmp=N file.go — Phase-2 scheduler entry point.
run: main
	$(JAVA) $(JAVA_FLAGS) -cp "$(RUN_CP)" graalgo.sched.launcher.SchedMain $(ARGS)

test: main $(OUT_TEST)/.built
	mkdir -p out/test-tmp
	TMPDIR="$(CURDIR)/out/test-tmp" GOTMPDIR="$(CURDIR)/out/test-tmp" \
	$(JAVA) -Djava.io.tmpdir="$(CURDIR)/out/test-tmp" $(JAVA_FLAGS) \
	-cp "$(TEST_CP)" graalgo.sched.SchedTestRunner
$(OUT_TEST)/.built: $(SRC_TEST) $(OUT_MAIN)/.built
	mkdir -p $(OUT_TEST)
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(OUT_MAIN):$(P1_CLASSES):$(CP)" -d $(OUT_TEST) $(SRC_TEST)
	touch $(OUT_TEST)/.built

# placeholder + debug-leftover grep gates (Phase-1 done-gate 5 discipline).
# The launcher (SchedMain) is excluded from the debug gate: printing usage
# and diagnostics to stderr is its job (same exclusion as Phase-1 Main).
# SchedulerCore's panic/crash reporting and Drainer's fatal-halt message
# are likewise excluded: printing "panic: ..." on an uncaught Go panic is
# Go parity (gc prints it), and a violated scheduler invariant must be
# LOUD before halting (P1-4: drainer death is never silent). These are
# production diagnostics, not debug leftovers. TimerHeap's
# callback-threw message is excluded on the same grounds: a throwing
# timer callback must be loud, and letting it kill the timer thread
# silently would stall every timer with no diagnostic.
# GcInterop's finalizer-panicked message is excluded on the same grounds:
# a panicking Go finalizer must be loud (gc crashes the program); letting
# the finalizer thread die silently would lose the diagnostic.
check:
	@echo "== placeholder grep gate =="
	@if grep -rn --include='*.java' -E 'TODO|FIXME|XXX|panic\("not implemented"\)|throw new UnsupportedOperationException\("stub' src/; then \
		echo "PLACEHOLDER GATE FAILED"; exit 1; fi
	@echo "placeholder gate clean"
	@echo "== debug-leftover grep gate (src/main only; launcher output is legitimate) =="
	@if grep -rn --include='*.java' -E 'System\.(out|err)\.println' src/main/ | grep -v 'src/main/java/graalgo/sched/launcher/SchedMain.java' | grep -v 'src/main/java/graalgo/sched/SchedulerCore.java' | grep -v 'src/main/java/graalgo/sched/Drainer.java' | grep -v 'src/main/java/graalgo/runtime/TimerHeap.java' | grep -v 'src/main/java/graalgo/runtime/GcInterop.java'; then \
		echo "DEBUG-LEFTOVER GATE FAILED"; exit 1; fi
	@echo "debug-leftover gate clean"
	@echo "== netpoller-deletion gate (GG-0018, P2-M3) =="
	@if grep -rn --include='*.java' -E 'Netpoll\.' src/ | grep -v 'src/main/java/graalgo/runtime/Netpoll.java' | grep -v 'src/test/java'; then \
		echo "NETPOLLER GATE FAILED: reachable netpoller code path"; exit 1; fi
	@if grep -rn --include='*.java' -E 'java\.nio\.channels|java\.net\.' src/main/; then \
		echo "NETPOLLER GATE FAILED: NIO bypassing slot discipline (GG-0018 second disjunct)"; exit 1; fi
	@echo "netpoller-deletion gate clean"
	@echo "== off-heap/barrier gate (ADR-0002 D3/F3) =="
	@echo "-- each pattern guards a way Go references could bypass JVM GC barriers;"
	@echo "-- allocateMemory/MemorySegment/Arena/allocateDirect: off-heap Go-object storage"
	@echo "-- theUnsafe/reflective Unsafe: raw-address access outside barrier discipline"
	@echo "-- sun.misc/jdk.internal imports: unvetted barrier-bypass surface"
	@if grep -rn --include='*.java' -E 'allocateMemory|MemorySegment|Arena\.|theUnsafe|allocateDirect|import sun\.misc\.|import jdk\.internal' src/; then \
		echo "OFF-HEAP/BARRIER GATE FAILED"; exit 1; fi
	@echo "off-heap/barrier gate clean"

clean:
	rm -rf out
