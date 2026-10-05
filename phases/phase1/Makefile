# graalgo Phase 1 — language core build
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
SRC_TEST := $(shell find src/test/java -name '*.java')
SRC_SMOKE := $(shell find src/smoke -name '*.java')
OUT_MAIN := out/classes
OUT_TEST := out/test-classes
OUT_SMOKE := out/smoke-classes

# gofrontend: the Go frontend service binary. Built from the vendored go/*
# tree via frontend/build.sh (overlay build; see frontend/VENDORING.md).
# graalgo-run locates it via GRAALGO_GOFRONTEND, else at this path
# (see graalgo.launcher.ToolPaths).
GOFRONTEND := frontend/gofrontend
GOFRONTEND_SRC := frontend/main.go frontend/export.go frontend/build.sh frontend/go.mod
$(GOFRONTEND): $(GOFRONTEND_SRC)
	cd frontend && PATH="$(GO_BIN_DIR):$$PATH" ./build.sh build

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

# graalgo-run: frontend + Truffle AST execution for one Go file
run: main
	$(JAVA) $(JAVA_FLAGS) -cp "$(OUT_MAIN):$(CP)" graalgo.launcher.Main $(ARGS)

test: main $(OUT_TEST)/.built
	mkdir -p out/test-tmp
	# /tmp is a 512M tmpfs: go builds in the differential tests would fill it.
	TMPDIR="$(CURDIR)/out/test-tmp" GOTMPDIR="$(CURDIR)/out/test-tmp" \
	$(JAVA) $(JAVA_FLAGS) -cp "$(OUT_TEST):$(OUT_MAIN):$(CP)" graalgo.test.Runner
$(OUT_TEST)/.built: $(SRC_TEST) $(OUT_MAIN)/.built
	mkdir -p $(OUT_TEST)
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(OUT_MAIN):$(CP)" -d $(OUT_TEST) $(SRC_TEST)
	touch $(OUT_TEST)/.built

# placeholder grep gate (done-gate 5): fails on TODO/FIXME/stub markers in src,
# plus the gofrontend check (gofmt/vet/tests/vendoring diff gate).
check:
	@echo "== placeholder grep gate =="
	@if grep -rn --include='*.java' -E 'TODO|FIXME|XXX|panic\("not implemented"\)|throw new UnsupportedOperationException\("stub' src/; then \
		echo "PLACEHOLDER GATE FAILED"; exit 1; fi
	@echo "placeholder gate clean"
	@echo "== gofrontend check (gofmt/vet/tests/vendoring diff gate) =="
	cd frontend && PATH="$(GO_BIN_DIR):$$PATH" ./build.sh check

smoke:
	$(MAKE) run ARGS="corpus/hello.go"

clean:
	rm -rf out /tmp/smoke-classes /tmp/dsl-classes frontend/gofrontend frontend/overlay.json
