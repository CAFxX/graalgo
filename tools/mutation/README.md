# mutation

Curated source-level mutation testing for the graalgo backend
(`tools/mutation/mutants.json`).

For each mutant it builds an overlay (Java) or a rebuilt binary (Go),
runs the full `graalgo.test.Runner` suite, and reports KILLED (suite
red) or SURVIVED (suite green). A baseline control first requires the
clean tree to be green — otherwise every mutant is trivially "killed".

Usage:

```
go run . <repo-root> <mutants.json>
```

Java mutants target the language core in
[internal/backend/truffle/src/main/java/](../../internal/backend/truffle/src/main/java/)
(the sources compile in the runtime module's joint round, so the
overlay compiles against `internal/backend/runtime/out/classes` +
`internal/backend/truffle/out/classes` + `internal/backend/truffle/lib/*.jar`,
and the suite runs with the overlay first on the classpath, shadowing
the baseline class). Go mutants target the exporter
(`internal/frontend/`) and run with `GRAALGO_GOFRONTEND` pointing at
the rebuilt binary.

The report is written to the run workdir only (`report.json`); per repo
AGENTS.md §5, `hidden_files/` must never reappear and per-run evidence
belongs in commit messages.

## Tool-rot remediation (2026-10-19)

Decision: **repaired**, not retired. The tool was fully broken by the
2026-10-14 `phases/` removal (javac/test classpaths built on
`phases/phase1`, mutant targets at removed paths, reports persisted
into a banned `hidden_files/` dir) — every verdict came back ERROR. It
is unwired (no Makefile target, no workflow) but its function —
proving the backend test suite actually kills mutants — is not
duplicated anywhere else, and the repoint is mechanical, so repair was
cheaper than losing the capability. Retargeted to the current module
outputs and source paths, `mutants.json` retargeted, outputs relocated
out of `hidden_files/`.
