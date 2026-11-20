# internal/backend/gir — Java GIR codec (status: **new**)

JDK-only decoder/encoder of GIR (package `graalgo.gir`). `Gir.java` and
`GirCodec.java` are **generated** from
[internal/toolexec/ir/schema.txt](../../toolexec/ir/schema.txt) by `make
generate` — never edit them by hand. The sources are also compiled into the
truffle module, where the bundle loader (`graalgo.go.loader`) uses them.

**Not here:** the schema and the Go codec ([internal/toolexec/ir/](../../toolexec/ir/README.md));
what the decoded GIR becomes ([graalgo/go/build/](../truffle/src/main/java/graalgo/go/build/)).

**Test:** `make test-gir` (decodes, validates and re-encodes the golden
vectors of [internal/toolexec/ir/testdata/](../../toolexec/ir/testdata/)
byte for byte, rejects the malformed ones, fuzzes the decoder);
`make -C internal/backend/gir test GIR_EXTRA=<dir>` adds GIR files from
[girstat](../../toolexec/cmd/girstat/README.md).
