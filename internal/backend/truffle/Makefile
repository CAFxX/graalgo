# graalgo backend — truffle module build (Truffle AST implementation)
#
# Own javac module: phases/phase1's language-core sources, relocated per the
# Phase-4 repo restructure (lane 3):
#   internal/backend/truffle/src/main/java — graalgo.{nodes,values,frontend}
#     (AST node implementations) + the 8 frozen phase-1 stubs
#     (graalgo/runtime/{ChanOps,GcInterop,GoroutineState,Netpoll,Panic,
#     Scheduler,Timers}, graalgo/safepoint/Safepoint).
#
# COMPILE CYCLE (read before restructuring): the AST sources call
# graalgo.runtime.* intrinsics while graalgo.runtime/launcher/safepoint call
# back into graalgo.values/graalgo.frontend — the two trees cannot compile
# as separate javac rounds. The RUNTIME module (internal/backend/runtime)
# therefore compiles the JOINT round: its own sources plus this module's
# sources except the 8 stubs below. This module's main target compiles only
# the stubs, against the runtime module's classes: one javac round cannot
# hold two classes with the same fully-qualified name, and compiling the
# stubs here preserves the overlay semantics — the runtime classpath puts
# runtime/out/classes FIRST so the phase-2 same-FQN replacements shadow the
# stubs, exactly as phase2/out shadowed phase1/out.
#
# Compile order: the runtime module builds first; this module then compiles
# the stubs against runtime/out/classes; interop builds against both.
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

RUNTIME := ../runtime
RUNTIME_CLASSES := $(RUNTIME)/out/classes

LIB := $(wildcard lib/*.jar)
CP  := $(subst $(eval) ,:,$(strip $(LIB)))

# The 8 frozen phase-1 stubs: the only sources this module's main target
# compiles (the rest of src/main/java compiles in the runtime module's joint
# round — see the cycle note above). Kept as an explicit list: these are
# historical GG-0020 freeze artifacts that never grow.
STUBS := src/main/java/graalgo/runtime/ChanOps.java \
  src/main/java/graalgo/runtime/GcInterop.java \
  src/main/java/graalgo/runtime/GoroutineState.java \
  src/main/java/graalgo/runtime/Netpoll.java \
  src/main/java/graalgo/runtime/Panic.java \
  src/main/java/graalgo/runtime/Scheduler.java \
  src/main/java/graalgo/runtime/Timers.java \
  src/main/java/graalgo/safepoint/Safepoint.java
SRC_TEST := $(shell find src/test/java -name '*.java')
SRC_SMOKE := $(shell find src/smoke -name '*.java')
OUT_MAIN := out/classes
OUT_TEST := out/test-classes
OUT_SMOKE := out/smoke-classes

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

JAVA_FLAGS := --enable-native-access=ALL-UNNAMED -Xss32m
# gofrontend location: the frontend already lives at internal/frontend/, so
# ToolPaths' walk-up finds it unaided; the explicit export is a harmless
# redundancy (a stale env var is skipped via the isExecutable check).
# (Retired 2026-10-15: was a "# BRIDGE (frontend lane)" comment awaiting the
# code-move that already landed.)
export GRAALGO_GOFRONTEND := $(CURDIR)/../../../internal/frontend/gofrontend
# Corpus differential tests locate the corpus via -Dgraalgo.repo.dir.
P1_DIR_PROP := -Dgraalgo.repo.dir="$(CURDIR)/../../.."
# -Xss32m (M5 stance): guest-language recursion burns host stack — the
# panic corpus's 10000-deep unwind (pn12) needs ~13-16m (~1.3-1.6KB per
# nested call through Truffle's exception profiling); 32m is ~2x headroom.
# A stackless/trampolined call path is Phase-2+ work; re-measure at M10
# self-attack if deeper adversarial recursion lands.
JAVAC_FLAGS := -Xlint:deprecation -Werror

.PHONY: all main test smoke smoke-test smoke-v2 clean check

all: main

main: $(RUNTIME_CLASSES)/.built $(OUT_MAIN)/.built $(GOFRONTEND)
# The runtime module builds first (its joint round compiles this module's
# non-stub sources — see the cycle note in the header).
$(RUNTIME_CLASSES)/.built:
	$(MAKE) -C $(RUNTIME) main
$(OUT_MAIN)/.built: $(STUBS) $(RUNTIME_CLASSES)/.built lib/SHA256SUMS.txt
	mkdir -p $(OUT_MAIN)
	$(JAVAC) -version
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(RUNTIME_CLASSES):$(CP)" -d $(OUT_MAIN) $(STUBS)
	touch $(OUT_MAIN)/.built

# toolchain smoke tests (Truffle runtime + DSL processor); green on the pin
smoke-test: $(OUT_SMOKE)/.built
	$(JAVA) $(JAVA_FLAGS) -cp "$(OUT_SMOKE):$(CP)" Smoke
	$(JAVA) $(JAVA_FLAGS) -cp "$(OUT_SMOKE):$(CP)" DslSmoke
$(OUT_SMOKE)/.built: $(SRC_SMOKE) lib/SHA256SUMS.txt
	mkdir -p $(OUT_SMOKE)
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(CP)" -processorpath "$(CP)" -d $(OUT_SMOKE) $(SRC_SMOKE)
	touch $(OUT_SMOKE)/.built

# v2 wire-contract tests in the fast smoke path (per-push CI via the root
# `test-ci` target): the Java side of the Go→Java binary interface
# (BinaryDecoder + its golden tests). These previously ran only via
# graalgo.test.Runner under the 30+-minute differential `make test`
# (V2-11); a wire-format regression now fails the per-push gate instead of
# hiding until the next full differential run.
#
# Fast at run time: no gofrontend, no corpus, no network — the decoder and
# the runner are plain java.* only. The compile classpath is the full test
# classpath so Truffle-dependent v2 tests compile here too when they land.
#
# WIRING HOOK (for the v2-factorytests lane): the runner takes an optional
# class-name filter, so this target runs exactly the classes in
# V2_WIRE_TESTS. To add a v2 test to the smoke path: register it in
# graalgo.test.Runner TESTS (the existing convention), append its binary
# class name to V2_WIRE_TESTS, and keep its sources under
# src/test/java/graalgo/frontend/v2/ (picked up by the wildcard in
# V2_WIRE_SRC). graalgo.frontend.v2.NodeFactoryV2Test plugs in here when
# the v2-factorytests lane writes it — no Makefile restructuring needed.
V2_WIRE_TESTS := graalgo.frontend.v2.BinaryDecoderTest
V2_WIRE_SRC := src/main/java/graalgo/frontend/v2/BinaryDecoder.java \
  $(wildcard src/test/java/graalgo/frontend/v2/*Test.java) \
  src/test/java/graalgo/test/Runner.java
OUT_V2WIRE := out/v2-wire-classes
$(OUT_V2WIRE)/.built: $(V2_WIRE_SRC) $(RUNTIME_CLASSES)/.built $(OUT_MAIN)/.built lib/SHA256SUMS.txt
	mkdir -p $(OUT_V2WIRE)
	$(JAVAC) -version
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(RUNTIME_CLASSES):$(OUT_MAIN):$(CP)" -d $(OUT_V2WIRE) $(V2_WIRE_SRC)
	touch $(OUT_V2WIRE)/.built

smoke-v2: $(OUT_V2WIRE)/.built
	$(JAVA) $(JAVA_FLAGS) -cp "$(OUT_V2WIRE):$(RUNTIME_CLASSES):$(OUT_MAIN):$(CP)" graalgo.test.Runner $(V2_WIRE_TESTS)

# graalgo-run: frontend + Truffle AST execution for one Go file.
# (graalgo.launcher.Main compiles in the runtime module's joint round.)
run: main
	$(JAVA) $(JAVA_FLAGS) -cp "$(RUNTIME_CLASSES):$(OUT_MAIN):$(CP)" graalgo.launcher.Main $(ARGS)

# The differential test runner (graalgo.test.Runner) exercises the launcher
# too, so the runtime classes join the test classpath (shadow order:
# replacements before stubs).
test: main $(OUT_TEST)/.built
	mkdir -p out/test-tmp
	# /tmp is a 512M tmpfs: go builds and Java temp dirs in the differential
	# tests would fill it. GOTMPDIR covers go; -Djava.io.tmpdir covers Java
	# (Java ignores the TMPDIR env var for java.io.tmpdir).
	TMPDIR="$(CURDIR)/out/test-tmp" GOTMPDIR="$(CURDIR)/out/test-tmp" \
	$(JAVA) -Djava.io.tmpdir="$(CURDIR)/out/test-tmp" $(P1_DIR_PROP) $(JAVA_FLAGS) \
	-cp "$(OUT_TEST):$(RUNTIME_CLASSES):$(OUT_MAIN):$(CP)" graalgo.test.Runner
$(OUT_TEST)/.built: $(SRC_TEST) $(OUT_MAIN)/.built $(RUNTIME_CLASSES)/.built
	mkdir -p $(OUT_TEST)
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(RUNTIME_CLASSES):$(OUT_MAIN):$(CP)" -d $(OUT_TEST) $(SRC_TEST)
	touch $(OUT_TEST)/.built

# placeholder grep gate (done-gate 5): fails on TODO/FIXME/stub markers in src,
# plus the gofrontend check (gofmt/vet/tests/vendoring diff gate).
check:
	@echo "== placeholder grep gate =="
	@if grep -rn --include='*.java' -E 'TODO|FIXME|XXX|panic\("not implemented"\)|throw new UnsupportedOperationException\("stub' src/; then \
		echo "PLACEHOLDER GATE FAILED"; exit 1; fi
	@echo "placeholder gate clean"
	@echo "== debug-leftover grep gate (src/main only) =="
	@if grep -rn --include='*.java' -E 'System\.(out|err)\.println' src/main/; then \
		echo "DEBUG-LEFTOVER GATE FAILED"; exit 1; fi
	@echo "debug-leftover gate clean"
	@echo "== gofrontend check (gofmt/vet/tests/vendoring diff gate) =="
	cd $(GO_FRONTEND_DIR) && PATH="$(GO_BIN_DIR):$$PATH" ./build.sh check

# (Retired 2026-10-15: was a "# BRIDGE (test lane)" comment; the corpus
# already lives at test/corpus/phase1.)
P1_CORPUS_DIR ?= ../../../test/corpus/phase1
smoke:
	$(MAKE) run ARGS="$(P1_CORPUS_DIR)/hello.go"

clean:
	rm -rf out /tmp/smoke-classes /tmp/dsl-classes $(GOFRONTEND) $(GO_FRONTEND_DIR)/overlay.json
