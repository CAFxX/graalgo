# corpus/builtins — builtin function tests (WS G)

Differential tests for Go builtins, vs stock gc 1.27.1. Each program is
run with `go run` (expected outputs below, captured 2026-10-04) and, once
the frontend pipeline supports builtins (NodeFactory.buildCall dispatch +
gofrontend builtin emission, WS B/G), through graalgo for comparison.

## Programs

| File | Tests | Key expectation |
|------|-------|-----------------|
| bi01_append_growth.go | append growth | caps valid (cap>=len, non-decreasing); sum 190. Exact caps are compiler-dependent (see note) |
| bi02_append_alias.go | append aliasing | input slice not mutated; t[0]=99, s[0]=1 |
| bi03_copy_overlap.go | copy overlap | memmove semantics: n=4, s=[1,1,2,3,4] |
| bi04_delete_lookup.go | delete+lookup | b absent (0,false); len=2 after delete |
| bi05_make_lencap.go | make len/cap | [3]cap5 zeros; [4]cap4; map len 1 |
| bi06_complex.go | complex/real/imag | (1.5,2.5); sum (4,6); prod (-5,10); quot (0.44,0.08) |
| bi07_print_stderr.go | print/println | **stderr** (gc parity); stdout empty |
| bi08_len_cap.go | len/cap types | "héllo"=6 bytes; arr 5/5; map 2 |
| bi09_new.go | new | allocates (non-nil) |
| bi10_append_varieties.go | append forms | len 3, elems 1 2 3; t len 8; bytes "hi!!" (caps not asserted) |
| bi11_panic.go | panic | panics "boom", exit 2, stderr |
| bi12_fmt_stub.go | fmt D4 stub | Print/Printf/Println/Sprint/Sprintf/Sprintln/Errorf |
| bi13_append_sizes.go | growth by elem size | len 10, valid caps, elems correct (exact caps compiler-dependent) |
| bi14_map_lifecycle.go | map ops | make/assign/len/delete/lookup |
| bi15_copy_counts.go | copy counts | n=3 (truncated); n2=5 |
| bi23_delete_during_range.go | delete during range | current-key delete: all 8 produced (360); not-yet-reached delete: only first produced (stderr) |

**Note on exact capacities (bi01, bi10, bi13):** gc's observable `cap`
after `append` depends on a compiler optimization (the `growsliceBuf`
stack-buffer path in `runtime/slice.go`), not just the documented
`growslice`/`nextslicecap` runtime algorithm. The Go language does not
specify growth, so exact caps are not a differential property across
different backends. graalgo implements the pure runtime algorithm
(pinned by unit tests); the corpus programs assert validity
(`cap >= len`, monotonic) and element correctness, not exact values.

## Expected outputs (gc 1.27.1, 2026-10-04)

### bi01
```
valid: true len: 20
sum: 190
```

### bi02
```
s: 1 2 3 len: 3
t: 1 2 3 4 len: 4
s0: 1 t0: 99
```

### bi03
```
n: 4
1 1 2 3 4
m: 4
2 3 4 5 5
k: 2 d: 1 1
```

### bi04
```
b: 0 false
a: 1 true
len: 2
b2: 20 true
```

### bi05
```
len: 3 cap: 5
elems: 0 0 0
len: 4 cap: 4
map len: 1
map2 len: 0
```

### bi06
```
real: 1.5 imag: 2.5
sum real: 4 imag: 6
prod real: -5 imag: 10
quot real: 0.44 imag: 0.08
```

### bi07 (stderr; stdout empty)
```
a1b2x 10 y 20
done
```

### bi08
```
str: 6
empty: 0
arr len: 5 cap: 5
slice len: 3 cap: 3
map len: 2
```

### bi09
```
allocated: true
```

### bi10
```
len: 3 s: 1 2 3
t len: 8 t: 1 8
bytes: hi!!
```

### bi11 (panics; exit 2; stderr)
```
before
panic: boom
```

### bi12 (stdout via fmt; bi07 covers stderr)
```
a11+2=3
x y
p5
00042
m 7
oops 7
```

### bi13
```
bytes len: 10 valid: true b9: 9
u64 len: 10 valid: true w9: 9
```

### bi14
```
len: 2
len: 1
v:  ok: false
v: two ok: true
```

### bi15
```
n: 3 dst: 1 2 3
n2: 5 len: 10
```

### bi23
```
delcur: 360 8
delnotreached: 10 1
```

## Red-green / mutation story (SPEC §5.2)

Two mutants were run against the fmt matrix and builtin logic; logs in
`../hidden_files/ws-g/`:

1. **append tail-copy drop** (`Intrinsics.growslice`: guarded the memmove
   with `if (false && ...)`): append(0..19) sum becomes 19 (not 190).
   Killed by bi01 logic.
2. **verb `%x` on strings uppercased** (`Fmt.fmtString`: `LDIGITS` →
   `UDIGITS` for case 'x'): `Sprintf("%x", "é")` gives `"C3A9"`, gc
   expects `"c3a9"`. Killed by the matrix.

## Wiring status

- `NodeFactory.buildCall` (WS G): dispatches `builtin` names to the
  `*Node` classes in `graalgo.frontend` (LenNode, CapNode, MakeNode,
  NewNode, AppendNode, CopyNode, DeleteNode, ComplexNode, PrintNode,
  PanicNode, FmtCallNode). **Pending**: frontend worker owns NodeFactory;
  integration after they stabilize.
- gofrontend builtin emission (WS B): `call.fields.builtin` must be set
  for builtin calls. **Pending**.
- `fmt` import overlay (WS B/G): `fmtcall` wire node. **Pending**.
