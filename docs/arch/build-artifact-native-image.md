# `graalgo build` artifacts: path to native-image

## Current state (2026-10-15)

`graalgo build` produces a **launcher binary**: a small Go program that
embeds the frontend output (gzip-compressed JSON) and, at run time, feeds
it to the graalgo backend via `graalgo-sync-run --wire-input`, which runs
the Truffle JIT on the JVM.

This is a proper executable (ELF, not a shell script), and it does not
re-run the frontend on every execution. But it is not a true native
binary like `go build` produces: it still requires a JVM and the graalgo
backend installation at run time.

## Target: native-image AOT binary

A true `go build` replacement produces a self-contained native binary.
For graalgo, this means GraalVM native-image with Truffle runtime
compilation embedded, so guest (Go) code still JIT-compiles at run time
with speculation and deoptimization.

The T6 optimization inventory
([docs/arch/research/T6-optimization-inventory.md](research/T6-optimization-inventory.md))
documents that this is real and shipping:

- Truffle runtime compilation inside a Native Image is demonstrated (Protos
  project, Sept 2026: "native executable with Truffle guest compilation").
- During closed-world analysis, Native Image computes "all methods that
  are reachable for runtime compilation" (oracle/graal `HostCompilation.md`).
- The build output reports "Runtime Compiled Methods" accounting for graph
  encodings in the heap.

### What's needed

1. **Native-image build configuration** for the backend:
   - `reflect-config.json`, `resource-config.json`, `proxy-config.json`
     for the Truffle language, the frontend JSON parser, and all
     guest-language node classes.
   - `--enable-native-access` at build time (FFM support is on by default
     since GraalVM 25).
   - Closed-world-friendly code: no runtime reflection scanning (the
     `graalgo test` driver already generates `ProxyExecutable`
     registrations at build time; see T7).

2. **Truffle language registration** for native-image:
   - The language must be registered via `TruffleLanguage.Registration`
     with `bytecodeInterpreter` or AST interpreter reachable for the
     runtime-compilation analysis.
   - Graph encodings for all guest node types must be retained in the
     heap (the "Runtime Compiled Methods" build output).

3. **Artifact format change**:
   - Instead of embedding frontend JSON, the build would run
     `native-image` on the backend + embedded program, producing a
     self-contained ELF binary.
   - Build time is minutes-scale (vs `go build`'s seconds); a build
     cache is required for the drop-in story (see T6 §5).

4. **Isolates** (Phase 5 `graalgo/x`):
   - Isolates are native-image-only. In pure-JIT mode the fallback is
     polyglot `Context`s in one shared heap (weaker isolation).
   - See [docs/arch/SPEC.md](SPEC.md) §583 and
     [docs/arch/research/T7-jvm-integration.md](research/T7-jvm-integration.md).

### Non-goals for the launcher binary

The current launcher binary is the correct intermediate artifact. It must
not grow into a poor man's native-image:

- Do not attempt to bundle a JRE inside the artifact (use the system's
  GraalVM via `GRAALVM_HOME`, as `graalgo-sync-run` does).
- Do not attempt ahead-of-time Truffle compilation without native-image;
  the Truffle JIT needs the JVM runtime.
- The `--wire-input` / `--emit-wire` flags are the stable interface
  between the build artifact and the backend; the native-image build
  will consume the same frontend JSON.

## References

- [T6 optimization inventory](research/T6-optimization-inventory.md) —
  hybrid mode existence proof, build-time costs.
- [T7 JVM integration](research/T7-jvm-integration.md) — isolates,
  native-access, JFR in native-image mode.
- [SPEC.md](SPEC.md) §583, §637, §990 — isolates native-image-only,
  reflection scanning, closure decision.
- [FEASIBILITY.md](FEASIBILITY.md) §187, §404 — isolate containment modes.
