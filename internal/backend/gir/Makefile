# GIR decoder module (docs/arch/toolexec-design.md §3.2, work package W2):
# the Java side of the GIR container, generated from
# internal/toolexec/ir/schema.txt (Gir.java, GirCodec.java, GirLocals.java) plus the
# hand-written primitives. Depends on the JDK only; the loader/node factory
# that consumes it (W4) comes later.
#
#   make test                golden vectors: decode, validate, re-encode == input,
#                            malformed vectors rejected, mutation fuzzing
#   make test GIR_EXTRA=dir  also round-trip every .gir under dir (e.g. the
#                            std set written by `girstat -out dir`)

JAVA_HOME ?= $(HOME)/tools/graalvm-jdk-25
JAVAC ?= $(JAVA_HOME)/bin/javac
JAVA ?= $(JAVA_HOME)/bin/java
OUT := out
VECTORS := ../../toolexec/ir/testdata
GIR_FUZZ ?= 300
GIR_EXTRA ?=

MAIN_SRC := $(wildcard src/main/java/graalgo/gir/*.java)
TEST_SRC := $(wildcard src/test/java/graalgo/gir/*.java)

.PHONY: build test clean

build: $(OUT)/.stamp

$(OUT)/.stamp: $(MAIN_SRC) $(TEST_SRC)
	rm -rf $(OUT)/classes
	mkdir -p $(OUT)/classes
	$(JAVAC) -Xlint:all -Werror -d $(OUT)/classes $(MAIN_SRC) $(TEST_SRC)
	touch $@

test: build
	nice -n 10 $(JAVA) -Xmx1g -Xss8m -cp $(OUT)/classes graalgo.gir.GirTests -fuzz $(GIR_FUZZ) -stats $(VECTORS) $(GIR_EXTRA)

clean:
	rm -rf $(OUT)
