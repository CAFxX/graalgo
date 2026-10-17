# graalgo backend — interop module build (stdlib bindings: Go<->Java interop)
#
# Own javac module: phases/phase3's sync-bindings Java sources, relocated per
# the Phase-4 repo restructure (lane 3):
#   internal/backend/interop/src/main/java — graalgo.{iface,p3chan,p3link,
#     p3math,p3net,p3os,p3reflect,p3sync,p3time,p3tls,p3unsafe,p3x509}.
#
# The truffle and runtime modules are consumed as FROZEN dependencies: this
# module compiles against their out/classes and does not modify their
# sources.
#
# Classpath order is load-bearing: interop/out/classes comes FIRST, then
# runtime/out/classes (so the runtime "owns" same-FQN replacements shadow
# the truffle module's phase-1 stubs), then truffle/out/classes, then the
# Truffle jars.
#
# Pinned toolchain: Oracle GraalVM 25.0.4.1.1 LTS, plain javac,
# -Xlint:deprecation -Werror (standing directive 2026-10-04).

GRAALVM_HOME ?= $(HOME)/tools/graalvm-jdk-25
JAVAC := $(GRAALVM_HOME)/bin/javac
JAVA  := $(GRAALVM_HOME)/bin/java

TRUFFLE := ../truffle
RUNTIME := ../runtime
TRUFFLE_CLASSES := $(TRUFFLE)/out/classes
RUNTIME_CLASSES := $(RUNTIME)/out/classes
LIB := $(wildcard $(TRUFFLE)/lib/*.jar)
CP := $(subst $(eval) ,:,$(strip $(LIB)))

SRC_MAIN := $(shell find src/main/java -name '*.java')
SRC_TEST := $(shell find src/test/java -name '*.java' 2>/dev/null)
SRC_RES := $(shell find src/main/resources -type f 2>/dev/null)
OUT_MAIN := out/classes
OUT_TEST := out/test-classes

# Interop runtime classpath: interop FIRST, then runtime (overlay), then
# truffle, then jars.
RUN_CP := $(OUT_MAIN):$(RUNTIME_CLASSES):$(TRUFFLE_CLASSES):$(CP)
TEST_CP := $(OUT_TEST):$(RUN_CP)

# p3frontend Go sources and stdlib-fork tooling now live in their
# Phase-4 homes (moved from phases/phase3 during the migration cleanup).
P3FRONTEND := ../../frontend/p3frontend
STDLIB_FORK := ../../../tools/stdlib-fork

JAVA_FLAGS := --enable-native-access=ALL-UNNAMED -Xss32m
# gofrontend location: ToolPaths' walk-up reaches internal/frontend/ unaided;
# the explicit export is a harmless redundancy (a stale env var is skipped
# via the isExecutable check). (Retired 2026-10-15: was a "# BRIDGE (frontend
# lane)" comment awaiting the code-move that already landed.)
export GRAALGO_GOFRONTEND := $(CURDIR)/../../../internal/frontend/gofrontend
JAVAC_FLAGS := -Xlint:deprecation -Werror

.PHONY: all main test check clean run

all: main

# The truffle and runtime modules must be built first (frozen dependencies).
$(TRUFFLE_CLASSES)/.built:
	$(MAKE) -C $(TRUFFLE) main
$(RUNTIME_CLASSES)/.built:
	$(MAKE) -C $(RUNTIME) main

main: $(TRUFFLE_CLASSES)/.built $(RUNTIME_CLASSES)/.built $(OUT_MAIN)/.built
$(OUT_MAIN)/.built: $(SRC_MAIN) $(SRC_RES)
	mkdir -p $(OUT_MAIN)
	$(JAVAC) -version
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(RUNTIME_CLASSES):$(TRUFFLE_CLASSES):$(CP)" -processorpath "$(CP)" \
		-d $(OUT_MAIN) $(SRC_MAIN)
	@if [ -d src/main/resources ]; then \
		cp -r src/main/resources/. $(OUT_MAIN)/; fi
	touch $(OUT_MAIN)/.built

# graalgo-sync-run --gmp=N file.go — interop (product) entry point.
run: main
	$(JAVA) $(JAVA_FLAGS) -cp "$(RUN_CP)" graalgo.p3sync.launcher.P3SyncMain $(ARGS)

test: main $(OUT_TEST)/.built
	$(JAVA) $(JAVA_FLAGS) -cp "$(TEST_CP)" graalgo.iface.IfaceTestRunner
$(OUT_TEST)/.built: $(SRC_TEST) $(OUT_MAIN)/.built
	mkdir -p $(OUT_TEST)
	$(JAVAC) $(JAVAC_FLAGS) -cp "$(OUT_MAIN):$(RUNTIME_CLASSES):$(TRUFFLE_CLASSES):$(CP)" -d $(OUT_TEST) $(SRC_TEST)
	touch $(OUT_TEST)/.built

# placeholder + debug-leftover grep gates (Phase-1 done-gate 5 discipline).
# The launcher (P3SyncMain) is excluded from the debug gate: printing usage
# and diagnostics to stderr is its job (same exclusion as SchedMain and Main).
# The "fatal error: " idiom on System.err is allowlisted: gc's runtime.throw /
# runtime.fatal (reached here as sync.throw / sync.fatal in SyncRuntime.java)
# MUST print "fatal error: <s>" to stderr before exit(2) — that is the gc
# contract, not a debug leftover. Any other System.out/err.println still
# fails the gate.
check: main $(OUT_TEST)/.built
	@echo "== placeholder grep gate =="
	@if grep -rn --include='*.java' -E 'TODO|FIXME|XXX|panic\("not implemented"\)|throw new UnsupportedOperationException\("stub' src/; then \
		echo "PLACEHOLDER GATE FAILED"; exit 1; fi
	@echo "placeholder gate clean"
	@echo "== debug-leftover grep gate (src/main only; launcher output is legitimate) =="
	@if grep -rn --include='*.java' -E 'System\.(out|err)\.println' src/main/ | grep -v 'src/main/java/graalgo/p3sync/launcher/P3SyncMain.java' | grep -v -F 'System.err.println("fatal error: '; then \
		echo "DEBUG-LEFTOVER GATE FAILED"; exit 1; fi
	@echo "debug-leftover gate clean"
	@echo "== stdlib exclusion-list gate (P3-M6) =="
	@if [ -f $(STDLIB_FORK)/exclusions.yaml ]; then \
		if [ -x "$(HOME)/tools/go/bin/go" ]; then \
			cd ../../../tools/stdlib-excl && GOTOOLCHAIN=local $(HOME)/tools/go/bin/go run . validate --list ../stdlib-fork/exclusions.yaml; \
		else \
			echo "WARNING: $(HOME)/tools/go/bin/go not found; exclusion-list gate skipped"; \
		fi; \
	fi
	@echo "== asm-inventory sync gate (P3-M6) =="
	@if [ -n "$(GO)" ]; then \
		GOROOT=$$(dirname $$(dirname "$(GO)")); \
		python3 $(STDLIB_FORK)/gen-asm-inventory.py --check --goroot "$$GOROOT"; \
	else \
		python3 $(STDLIB_FORK)/gen-asm-inventory.py --check; \
	fi
	@echo "== p3frontend Go build/vet gate (P3-M10 self-attack FINDING-4) =="
	@GO_BIN="$${GO:-$(HOME)/tools/go/bin/go}"; \
	if [ -x "$$GO_BIN" ]; then \
		cd $(P3FRONTEND) && GOTOOLCHAIN=local "$$GO_BIN" build ./... && GOTOOLCHAIN=local "$$GO_BIN" vet ./...; \
	else \
		echo "WARNING: Go toolchain not found ($$GO_BIN); p3frontend gate skipped"; \
	fi

clean:
	rm -rf out
