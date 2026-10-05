# P2-M2 channel differential corpus

Go programs exercising channel + select semantics. Each has a
gc-recorded `.expected` file (stdout; panic programs assert the panic
message in the test).

`ChannelDifferentialTest` executes mechanical Java translations on the
user-space scheduler and diffs byte-for-byte. Translation convention is
documented in the test's javadoc.

**Frontend dependency:** the Phase-1 exporter rejects these programs
(`SendStmt`/`SelectStmt` are "not supported in frontend/v1 scope"), so
end-to-end `graalgo-run` execution awaits the exporter lowering (parent
track). These files are the future end-to-end corpus: once the lowering
lands, the same programs must produce identical output through the
full pipeline.
