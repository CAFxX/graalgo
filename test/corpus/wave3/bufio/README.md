# `bufio` wave-3 corpus

Differential corpus for the synthetic `bufio` package (full port).

## Scope boundary

Deviations (see `synth_bufio.go` header):
- `unicode/utf8` inlined as `__bufio*` helpers.
- 9 multi-result methods via Appendix M (desugar entries).
- 3-result methods (ReadRune, ReadLine) dropped.

## Programs

- `00_basic.go` — Reader (Read/ReadByte/Peek/ReadBytes/ReadString/ReadSlice/Discard), Writer (Write/WriteString/Flush).
- `01_scanner.go` — Scanner (ScanLines/ScanWords/Bytes/Text/Err).

## Results

TBD (2026-10-09).
