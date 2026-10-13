# graalgo backend — truffle module build (Truffle AST implementation)
#
# Own javac module: phases/phase1's language-core sources, relocated per the
# Phase-4 repo restructure (lane 3):
#   internal/backend/truffle/src/main/java — graalgo.{nodes,values,frontend}
#     (AST node implementations) + the 8 frozen phase-1 stubs
#     (graalgo/runtime/{ChanOps,GcInterop,GoroutineState,Netpoll,Panic,
#     Scheduler,Timers}, graalgo/safepoint/Safepoint).
#
# The stubs live (and compile) in THIS module, not the runtime module: their
# same-FQN phase-2 replacements live there, and one javac round cannot hold
# two classes with the same fully-qualified name. Classpath order stays
# load-bearing: runtime/out/classes comes BEFORE truffle/out/classes, so the
# replacements shadow the stubs exactly as phase2/out shadowed phase1/out.
#
# Compile order: this module builds FIRST (nothing depends on it at javac
# time — the stubs only need graalgo.values.*, compiled in the same round).
# The runtime module then compiles against truffle/out/classes.
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

SRC_MAIN := $(shell find src/main/java -name '*.java')
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
GO_FRONTEND_DIR ?= ../../../phases/phase1/frontend
GOFRONTEND := $(GO_FRONTEND_DIR)/gofrontend
# P3-M7: the gofrontend binary embeds every non-test frontend/*.go source
# (synth_*.go, synth_machinery.go, ...). An explicit file list goes stale
# the moment a new synthetic package lands — a stale binary then silently
# tests the wrong sources (the errors/sort wave-1 packages hit this).
GOFRONTEND_SRC := $(wildcard $(GO_FRONTEND_DIR)/*.go) $(GO_FRONTEND_DIR)/build.sh $(GO_FRONTEND_DIR)/go.mod
$(GOFRONTEND): $(GOFRONTEND_SRC)
	cd $(GO_FRONTEND_DIR) && PATH="$(GO_BIN_DIR):$$PATH" ./build.sh build

JAVA_FLAGS := --enable-native-access=ALL-UNNAMED -Xss32m
# -Xss32m (M5 stance): guest-language recursion burns host stack — the
# panic corpus's 10000-deep unwind (pn12) needs ~13-16m (~1.3-1.6KB per
# nested call through Truffle's exception profiling); 32m is ~2x headroom.
# A stackless/trampolined call path is Phase-2+ work; re-measure at M10
# self-attack if deeper adversarial recursion lands.
JAVAC_FLAGS := -Xlint:deprecation -Werror

.PHONY: all main test smoke smoke-test clean check

all: main

main: $(OUT_MAIN)/.built $(GOFRONTEND)
$(OUT_MAIN)/.built: $(SRC_MAIN) lib/SHA256SUMS.txt
	mkdir -p $(OUT_MAIN)
	$(JAVAC) -version
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(CP)" -processorpath "$(CP)" -d $(OUT_MAIN) $(SRC_MAIN)
	touch $(OUT_MAIN)/.built

# toolchain smoke tests (Truffle runtime + DSL processor); green on the pin
smoke-test: $(OUT_SMOKE)/.built
	$(JAVA) $(JAVA_FLAGS) -cp "$(OUT_SMOKE):$(CP)" Smoke
	$(JAVA) $(JAVA_FLAGS) -cp "$(OUT_SMOKE):$(CP)" DslSmoke
$(OUT_SMOKE)/.built: $(SRC_SMOKE) lib/SHA256SUMS.txt
	mkdir -p $(OUT_SMOKE)
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(CP)" -processorpath "$(CP)" -d $(OUT_SMOKE) $(SRC_SMOKE)
	touch $(OUT_SMOKE)/.built

# The runtime module must be built first: graalgo.launcher.Main lives there
# now (internal/backend/runtime), and the runtime classes shadow this
# module's 8 frozen stubs on the classpath (overlay order, see header).
$(RUNTIME_CLASSES)/.built:
	$(MAKE) -C $(RUNTIME) main

# graalgo-run: frontend + Truffle AST execution for one Go file
run: main $(RUNTIME_CLASSES)/.built
	$(JAVA) $(JAVA_FLAGS) -cp "$(RUNTIME_CLASSES):$(OUT_MAIN):$(CP)" graalgo.launcher.Main $(ARGS)

# The differential test runner (graalgo.test.Runner) exercises the launcher
# too, so the runtime classes join the test classpath (shadow order).
test: main $(RUNTIME_CLASSES)/.built $(OUT_TEST)/.built
	mkdir -p out/test-tmp
	# /tmp is a 512M tmpfs: go builds and Java temp dirs in the differential
	# tests would fill it. GOTMPDIR covers go; -Djava.io.tmpdir covers Java
	# (Java ignores the TMPDIR env var for java.io.tmpdir).
	TMPDIR="$(CURDIR)/out/test-tmp" GOTMPDIR="$(CURDIR)/out/test-tmp" \
	$(JAVA) -Djava.io.tmpdir="$(CURDIR)/out/test-tmp" $(JAVA_FLAGS) \
	-cp "$(OUT_TEST):$(RUNTIME_CLASSES):$(OUT_MAIN):$(CP)" graalgo.test.Runner
$(OUT_TEST)/.built: $(SRC_TEST) $(OUT_MAIN)/.built $(RUNTIME_CLASSES)/.built
	mkdir -p $(OUT_TEST)
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(OUT_MAIN):$(RUNTIME_CLASSES):$(CP)" -d $(OUT_TEST) $(SRC_TEST)
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
