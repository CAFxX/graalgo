# graalgo-stub — the executable's front (status: **new**)

The small Go program that `graalgo-tool link` copies to the front of every
linked executable. At run time it finds the bundle appended to itself and
execs the runner [graalgo-bundle-run](../../../backend/graalgo-bundle-run)
(JVM, `graalgo.launcher.BundleMain`), forwarding args, env, streams and exit
status. Linked with a placeholder build ID that link patches
([toolexec-design.md](../../../../docs/arch/toolexec-design.md) §2.4, §4.2).

**Not here:** the bundle format ([../../bundle/](../../bundle/)).

**Test:** covered by `go test ./cmd/graalgo-tool` (link end-to-end) and
every new-path gate (`make test-m1a`).
