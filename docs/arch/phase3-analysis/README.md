# Phase-3 analysis docs (restored)

These files are the Phase-3 working analyses that live code headers still
cite. They were tracked in git under `phases/phase3/hidden_files/` and
deleted by the Phase-4 migration (`e5e6dbd9`, "move active code out of
`phases/`"), which left the citing headers dangling. They are restored
here **verbatim** — byte-identical to their content at `e5e6dbd9^`
(verified by byte count) — so the citations resolve again. They are
historical working notes (October 2026), not current normative specs:
where a restored note disagrees with current code, current code wins and
the [synthetic retirement tracker](../synth-retirement-tracker.md) is the
live record.

Restored (only the docs still cited by live code or live headers):

- [`m8-analysis/crypto.md`](m8-analysis/crypto.md) — P3-M8 crypto
  analysis; cited by the `synth_crypto*.go` headers and
  [`synth_machinery.go`](../../../internal/frontend/synth_machinery.go).
- [`m8-analysis/os.md`](m8-analysis/os.md) — P3-M8 `os` bring-up
  analysis; cited by [`synth_os.go`](../../../internal/frontend/synth_os.go)
  (`os.md` §§2.2–2.4, §5, §6) and
  [`synth_iofs.go`](../../../internal/frontend/synth_iofs.go) (§6 — see
  note below).
- [`m9-tmp/net-fork-contract.md`](m9-tmp/net-fork-contract.md) — the
  `internal/poll` + `syscall` socket-surface fork contract; cited by
  [`synth_net.go`](../../../internal/frontend/synth_net.go) and
  [`synth_poll.go`](../../../internal/frontend/synth_poll.go).
- [`m7-enc-xml/redgreen.md`](m7-enc-xml/redgreen.md) — `encoding/xml`
  rationale; cited by
  [`synth_xml.go`](../../../internal/frontend/synth_xml.go).
- [`m2/redgreen.md`](m2/redgreen.md) — timer-channel red-green evidence;
  cited by
  [`GoTimer.java`](../../../internal/backend/runtime/src/main/java/graalgo/runtime/GoTimer.java).
- [`m7-bytes/redgreen.md`](m7-bytes/redgreen.md) and
  [`m7-strings/redgreen.md`](m7-strings/redgreen.md) — the `bytes` /
  `strings` red-green evidence. (The `m7-bytes/linkname-routing.md`,
  `m7-bytes/gen_tables.go`, and `m7-strings/gen_tables.py` paths cited
  by `synth_bytes.go` / `synth_strings.go` were never tracked in git and
  cannot be restored; those headers now say the tables are committed
  inline instead. See the tracker.)

Corrections made while repointing, not by editing the restored files:

- `synth_iofs.go` cited `os.md §6.8`. The restored `os.md` has §6 (the
  fail-fast list) but no §6.8; the citation is repointed to §6.
- `synth_machinery.go` / `synth_reflect.go` cited the Phase-3 `p3reflect`
  package; that package now lives at
  [`internal/backend/interop/src/main/java/graalgo/p3reflect/`](../../../internal/backend/interop/src/main/java/graalgo/p3reflect/)
  and the headers point there.
- `synth_crypto_x509.go` cited
  `phases/phase3/src/main/java/graalgo/p3x509/GoX509.java`; that file now
  lives at
  [`internal/backend/interop/src/main/java/graalgo/p3x509/GoX509.java`](../../../internal/backend/interop/src/main/java/graalgo/p3x509/GoX509.java).
- `mrdesugar_m5.go` cited `phases/phase3/hidden_files/m9-tmp/tier5crypto/`
  (the `transform.go` generator). The generator and its `merged/` outputs
  were Phase-3 working files, deleted by the migration and not restored:
  the desugar tables are committed in `mrdesugar_m5.go` itself, and the
  comment now says so instead of pointing at a deleted directory.

Not restored: the rest of `phases/phase3/hidden_files/` (harness
scratch, `tier5crypto/merged/`, `tier1d-harness/`, …). Nothing live
cites those paths, and re-adding them would reintroduce the research
debris the Phase-4 migration deliberately deleted (see
[`REPO_STRUCTURE.md`](../../REPO_STRUCTURE.md) principle 3).
