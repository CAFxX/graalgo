# ir — GIR, the per-package typed IR (status: **new**)

The container written into each package archive and read by the JVM:
string table, type table keyed by canonical type keys, one lazily decodable
blob per function. [schema.txt](schema.txt) is the single source of truth;
[gen/](gen/) + [schema/](schema/) generate [gir_gen.go](gir_gen.go) here and
`Gir.java`/`GirCodec.java` in [internal/backend/gir/](../../backend/gir/README.md).
[typekey.go](typekey.go) defines canonical type keys (also used by the
intrinsic registry's signatures). [testdata/](testdata/) holds golden and
malformed vectors both codecs must agree on.
Design: [toolexec-design.md](../../../docs/arch/toolexec-design.md) §3.2–§3.4.

**Change the schema:** edit `schema.txt`, `make generate`, commit the
regenerated Go and Java files together (CI fails on drift).

**Test:** `cd internal/toolexec && go test ./ir`; `make test-gir` (Java side).
