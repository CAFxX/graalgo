# Pinned Java dependencies — graalgo Phase 1

All jars fetched from Maven Central 2026-10-04, sha256 in [SHA256SUMS.txt](SHA256SUMS.txt)
(each jar's sha1 verified against the `.jar.sha1` published on Central at
download time).

**GraalVM pin (program-owner decision 2026-10-04): Oracle GraalVM 25.0.4.1.1,
LTS line.** This closes the SPEC Appendix C `[OPEN]` (GraalVM pin version
string). The Innovation line (25.4.x) is re-evaluated at Phase 5; see the
2026-10-04 program decision record.

**Exact pin state:**
- Truffle language jars (below): **25.0.4.1.1** — the Truffle language API is
  NOT in the JDK distribution (verified: only `truffle-compiler.jar` ships in
  the JDK's `lib/truffle/`), so the jars are the version that matters for
  language implementation.
- JDK distribution at `~/tools/graalvm-jdk-25`: **25.0.4+7.1** (unchanged) —
  provides `javac`/`java`/JVMCI only. Bumping the JDK distribution is
  deferred until a 25.0.4.1.1+ JDK feature is needed; the Truffle API surface
  is fully determined by the jars above.

| Artifact | Version | Purpose |
|---|---|---|
| org.graalvm.truffle:truffle-api | 25.0.4.1.1 | Language implementation API |
| org.graalvm.truffle:truffle-runtime | 25.0.4.1.1 | HotSpot Truffle runtime |
| org.graalvm.truffle:truffle-dsl-processor | 25.0.4.1.1 | @Specialization annotation processor |
| org.graalvm.sdk:graal-sdk | 25.0.4.1.1 | (placeholder jar; API moved) |
| org.graalvm.sdk:collections | 25.0.4.1.1 | EconomicMap etc. (truffle-api dep) |
| org.graalvm.sdk:nativeimage | 25.0.4.1.1 | native-image annotations (truffle-api dep) |
| org.graalvm.polyglot:polyglot | 25.0.4.1.1 | Polyglot engine API (truffle-api dep) |
| org.graalvm.tools:profiler-tool | 25.0.4.1.1 | CPU sampler/tracer, memory tracer (`--cpusampler` …) |
| org.graalvm.tools:chromeinspector-tool | 25.0.4.1.1 | Chrome DevTools inspector (`--inspect`) |
| org.graalvm.tools:dap-tool | 25.0.4.1.1 | Debug Adapter Protocol server (`--dap`) |
| org.graalvm.tools:coverage-tool | 25.0.4.1.1 | Truffle coverage instrument (`--coverage`) |
| org.graalvm.tools:insight-tool | 25.0.4.1.1 | GraalVM Insight (`--insight`) |
| org.graalvm.shadowed:json | 25.0.4.1.1 | JSON library shared by the tools (tool dep) |
| org.graalvm.sdk:polyglot-tck | 25.0.4.1.1 | Polyglot TCK API (`LanguageProvider`, for language tests) |

The seven tool/TCK artifacts were added 2026-11-18 (RECOVERY TR-0,
[truffle-design.md §6.1](../../../../docs/arch/truffle-design.md)) from Maven
Central, each jar's sha1 verified against Central's `.jar.sha1` at download;
the transitive dependency set was read from each artifact's POM (the tools
need only `truffle-api` and `json`; `polyglot-tck` needs `polyglot` and
`collections`). Not added: `org.graalvm.truffle:truffle-tck`,
`truffle-tck-common`, `truffle-tck-tests` — they pull JUnit 4 and run the
TCK's own JUnit suites; nothing here uses JUnit yet. Add them (plus
`junit:junit`) when the polyglot TCK run of TR-8 lands. The instruments
register through `META-INF/services` and stay inactive unless enabled by an
option. `make check` verifies every jar against
[SHA256SUMS.txt](SHA256SUMS.txt) and that no jar is unlisted.

**Build policy (standing directive 2026-10-04):** `javac` runs with
`-Xlint:deprecation -Werror` — any use of a deprecated Truffle/JDK API is a
build break, not a warning. Rationale: 25.4 removed long-deprecated Truffle
APIs; code written now must survive the Phase-5 upgrade to the Innovation
line without rework.

Upgrade rule: bump all jars in lockstep with the GraalVM pin; record the
decision, date, and checksums here.
