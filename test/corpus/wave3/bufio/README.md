# `bufio` wave-3 corpus

Differential corpus for the synthetic `bufio` package (full port).

## Scope boundary

Deviations (see `synth_bufio.go` header):
- `unicode/utf8` inlined as `__bufio*` helpers.
- 9 multi-result methods via Appendix M (desugar entries).
- 3-result methods (ReadRune, ReadLine) dropped.
- Scanner split: `Scan()` is gc-verbatim and calls the stored split
  through the general multi-result func-field desugar (named func types
  such as `SplitFunc` unwrapped; table-driven values such as
  `ScanLines` lowered to their `__`-helpers). `__bufioScanLines` is
  retained for direct `ScanLines` calls via the Appendix M table.

## Programs

- `00_basic.go` — Reader (Read/ReadByte/Peek/ReadBytes/ReadString/ReadSlice/Discard), Writer (Write/WriteString/Flush).
- `01_scanner.go` — Scanner (ScanLines/ScanWords/Bytes/Text/Err).
- `02_split_builtins.go` — Scanner with every built-in split
  (ScanWords/ScanLines/ScanRunes/ScanBytes) plus the default.
- `03_split_custom.go` — Scanner with custom splits (named, func-lit,
  error-returning, ErrFinalToken).

## Results

TBD (2026-10-09).
