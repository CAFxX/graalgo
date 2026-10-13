# Pinned Java dependencies — graalgo Phase 1

All jars fetched from Maven Central 2026-10-04, sha256 in `SHA256SUMS.txt`
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

**Build policy (standing directive 2026-10-04):** `javac` runs with
`-Xlint:deprecation -Werror` — any use of a deprecated Truffle/JDK API is a
build break, not a warning. Rationale: 25.4 removed long-deprecated Truffle
APIs; code written now must survive the Phase-5 upgrade to the Innovation
line without rework.

Upgrade rule: bump all seven in lockstep with the GraalVM pin; record the
decision, date, and checksums here.
