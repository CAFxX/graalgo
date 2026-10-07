# graalgo Phase 3 — sync-bindings Java module (P3-M2b, WS D, ADR-0002)
#
# Own build module mirroring the Phase-2 layout. Phase 1 and Phase 2 are
# consumed as FROZEN dependencies: this module compiles against their
# out/classes and does not modify their sources. Documented exceptions:
# the exporter's synthetic sync package + syncnew/syncop wire nodes (a
# separate Phase-1 commit under the freeze change policy; the Java side
# consumes the new kinds through the public NodeFactory.registerExprKind
# hook — no NodeFactory change). Phase-2 sources are frozen post-sign-off;
# M2a's edits (052c752: GoChan un-final'd, GoTimer, new TimerChan +
# phase-2 test — behavior-preserving, red-green evidenced in
# phases/phase3/hidden_files/m2/redgreen.md, 194/194 green) were the first
# documented exception, and the P3-M2remed P1-2 one-word gc-parity message
# fix in GoRWMutex the second. Future phase-2 source edits each get their
# own ADR.
#
# Classpath order is load-bearing: phase3/out/classes comes FIRST, then
# phase2/out/classes (so the Phase-2 "owns" same-FQN replacements shadow
# the Phase-1 stubs), then phase1/out/classes, then the Truffle jars.
#
# Pinned toolchain: Oracle GraalVM 25.0.4.1.1 LTS, plain javac,
# -Xlint:deprecation -Werror (standing directive 2026-10-04).

GRAALVM_HOME ?= $(HOME)/tools/graalvm-jdk-25
JAVAC := $(GRAALVM_HOME)/bin/javac
JAVA  := $(GRAALVM_HOME)/bin/java

P1 := ../phase1
P2 := ../phase2
P1_CLASSES := $(P1)/out/classes
P2_CLASSES := $(P2)/out/classes
LIB := $(wildcard $(P1)/lib/*.jar)
CP := $(subst $(eval) ,:,$(strip $(LIB)))

SRC_MAIN := $(shell find src/main/java -name '*.java')
SRC_TEST := $(shell find src/test/java -name '*.java' 2>/dev/null)
OUT_MAIN := out/classes
OUT_TEST := out/test-classes

# Phase-3 runtime classpath: phase3 FIRST, then phase2 (overlay), then
# phase1, then jars.
RUN_CP := $(OUT_MAIN):$(P2_CLASSES):$(P1_CLASSES):$(CP)
TEST_CP := $(OUT_TEST):$(RUN_CP)

JAVA_FLAGS := --enable-native-access=ALL-UNNAMED -Xss32m
JAVAC_FLAGS := -Xlint:deprecation -Werror

.PHONY: all main test check clean run

all: main

# Phases 1 and 2 must be built first (frozen dependencies).
$(P1_CLASSES)/.built:
	$(MAKE) -C $(P1) main
$(P2_CLASSES)/.built:
	$(MAKE) -C $(P2) main

main: $(P1_CLASSES)/.built $(P2_CLASSES)/.built $(OUT_MAIN)/.built
$(OUT_MAIN)/.built: $(SRC_MAIN)
	mkdir -p $(OUT_MAIN)
	$(JAVAC) -version
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(P2_CLASSES):$(P1_CLASSES):$(CP)" -processorpath "$(CP)" \
		-d $(OUT_MAIN) $(SRC_MAIN)
	touch $(OUT_MAIN)/.built

# graalgo-sync-run --gmp=N file.go — Phase-3 sync-bindings entry point.
run: main
	$(JAVA) $(JAVA_FLAGS) -cp "$(RUN_CP)" graalgo.p3sync.launcher.P3SyncMain $(ARGS)

test: main $(OUT_TEST)/.built
	$(JAVA) $(JAVA_FLAGS) -cp "$(TEST_CP)" graalgo.iface.IfaceTestRunner
$(OUT_TEST)/.built: $(SRC_TEST) $(OUT_MAIN)/.built
	mkdir -p $(OUT_TEST)
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(OUT_MAIN):$(P2_CLASSES):$(P1_CLASSES):$(CP)" -d $(OUT_TEST) $(SRC_TEST)
	touch $(OUT_TEST)/.built

# placeholder + debug-leftover grep gates (Phase-1 done-gate 5 discipline).
# The launcher (P3SyncMain) is excluded from the debug gate: printing usage
# and diagnostics to stderr is its job (same exclusion as SchedMain and
# Phase-1 Main).
check: main $(OUT_TEST)/.built
	@echo "== placeholder grep gate =="
	@if grep -rn --include='*.java' -E 'TODO|FIXME|XXX|panic\("not implemented"\)|throw new UnsupportedOperationException\("stub' src/; then \
		echo "PLACEHOLDER GATE FAILED"; exit 1; fi
	@echo "placeholder gate clean"
	@echo "== debug-leftover grep gate (src/main only; launcher output is legitimate) =="
	@if grep -rn --include='*.java' -E 'System\.(out|err)\.println' src/main/ | grep -v 'src/main/java/graalgo/p3sync/launcher/P3SyncMain.java'; then \
		echo "DEBUG-LEFTOVER GATE FAILED"; exit 1; fi
	@echo "debug-leftover gate clean"
	@echo "== stdlib exclusion-list gate (P3-M6) =="
	@if [ -f stdlib-fork/exclusions.yaml ]; then \
		if [ -x "$(HOME)/tools/go/bin/go" ]; then \
			cd ../../tools/stdlib-excl && GOTOOLCHAIN=local $(HOME)/tools/go/bin/go run . validate --list ../../phases/phase3/stdlib-fork/exclusions.yaml; \
		else \
			echo "WARNING: $(HOME)/tools/go/bin/go not found; exclusion-list gate skipped"; \
		fi; \
	fi
	@echo "== asm-inventory sync gate (P3-M6) =="
	@python3 stdlib-fork/gen-asm-inventory.py --check

clean:
	rm -rf out
