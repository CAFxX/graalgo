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
# BRIDGE (frontend lane): the Go frontend sources still live at
# phases/phase1/frontend until the frontend code-move lane relocates them to
# internal/frontend/; repoint GO_FRONTEND_DIR then. The launcher locates the
# binary via GRAALGO_GOFRONTEND (exported by internal/backend/graalgo-run),
# else by walking up from its own classes dir to internal/frontend/.
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
# BRIDGE (frontend lane): subprocess tests and `run` resolve gofrontend via
# ToolPaths' walk-up, which no longer reaches phases/phase1/frontend from
# the new class locations. Export the current build location until the
# frontend lane moves it to internal/frontend/ (the walk-up then finds it
# unaided; a stale env var is skipped via the isExecutable check).
export GRAALGO_GOFRONTEND := $(CURDIR)/../../../internal/frontend/gofrontend
# BRIDGE (test lane): the corpus differential tests locate the corpus via
# -Dgraalgo.phase1.dir (default: the old phase-1 working directory).
P1_DIR_PROP := -Dgraalgo.phase1.dir="$(CURDIR)/../../../phases/phase1"
# -Xss32m (M5 stance): guest-language recursion burns host stack — the
# panic corpus's 10000-deep unwind (pn12) needs ~13-16m (~1.3-1.6KB per
# nested call through Truffle's exception profiling); 32m is ~2x headroom.
# A stackless/trampolined call path is Phase-2+ work; re-measure at M10
# self-attack if deeper adversarial recursion lands.
JAVAC_FLAGS := -Xlint:deprecation -Werror

.PHONY: all main test smoke smoke-test clean check

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

# BRIDGE (test lane): the corpus still lives at phases/phase1/corpus until
# the test code-move lane relocates it to test/corpus/; repoint P1_CORPUS_DIR.
P1_CORPUS_DIR ?= ../../../phases/phase1/corpus
smoke:
	$(MAKE) run ARGS="$(P1_CORPUS_DIR)/hello.go"

clean:
	rm -rf out /tmp/smoke-classes /tmp/dsl-classes $(GOFRONTEND) $(GO_FRONTEND_DIR)/overlay.json
