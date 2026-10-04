# `req` — requirements registry CLI (graalgo fork)

Forked 2026-10-03 from `~/workspace/momo/tools/req`. The upstream tool hardcodes
the `MM-` requirement-ID prefix (regexes, ID minting, usage strings); this fork
is a mechanical `MM-` → `GG-` rename (plus the module path `momo/tools/req` →
`graalgo/tools/req`) so graalgo mints `GG-0001…` IDs. No behavioral changes.

The full test suite was carried over with the rename (`gg-0042` replaces
`mm-0042` in the sloppy-ID fixture) and is green: `go test -count=1 ./...`,
`go vet ./...` clean, `gofmt` clean.

Upstream improvements should be forward-ported by re-applying the same rename
to the upstream tree and diffing; do not let the two forks diverge silently.

## Usage

```sh
export PATH=~/tools/go/bin:$PATH
cd ~/workspace/graalgo/tools/req
go run . add --title "..." --statement "..."   # mints next GG-XXXX
go run . lint --root ~/workspace/graalgo
go run . trace GG-0007
go run . lock GG-0001 --all                    # draft -> locked (SPEC acceptance)
go run . activate GG-0027                      # locked -> active (implementation started)
```

## Lifecycle

`draft` → `locked` (SPEC acceptance; immutable except via supersede/retire) →
`active` (implementation underway; lint rule (c) requires ≥1 `verifies:`
annotation) → `retired` / `superseded`. Use `lock` / `activate` (both atomic);
never hand-edit the `status:` field.
