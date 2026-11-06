# `regexp/syntax` wave-3 corpus

Differential corpus for the synthetic `regexp/syntax` package.

## Scope boundary

SCOPE-LIMITED: Blocked on a concrete method expression in value
position — `(*Regexp).Equal` at `synth_regexpsyntax.go:3301`
(`slices.EqualFunc(x.Sub, y.Sub, (*Regexp).Equal)`). The frontend
exports `x.X` unguarded for concrete receivers (`export.go:12018`),
so `exportIdent` fails with the internal error `identifier "Regexp"
denotes *types.TypeName` (gofrontend exit 2). The named P3-M3 scope
error for concrete method expressions (`export.go:12045-12047`) is
never reached. See REVIEW-2026-11-05-05 P1-2; the owning lane is
`review-1105-regexp-methodexpr` (fix committed `915a766a`, unmerged).

Retest history: the original trigger — `unicode` tables (Scripts,
Categories, SimpleFold) landing in the frontend overlay — fired:
`unicode` + `unicode/utf8` are in `syntheticSources`
(`synth_machinery.go:137-138`) and `synth_unicode.go` defines all
three tables. Retested 2026-11-06 on master `69957b40`: the gc leg
passes (`Concat`, `Concat`, `syntax ok`), the graalgo leg fails with
the method-expression error above, so the program stays
scope-limited for the new, precise reason.

Retest trigger: concrete method expressions in value position land
in the frontend.

## Programs

- `00_parse.go` — Parse, Simplify (`.scopelimited`).

## Results

0 passed, 1 scope-limited, 0 failed (2026-11-06; retest of the fired unicode trigger — now blocked on the method-expression frontend gap, REVIEW-2026-11-05-05 P1-2).
