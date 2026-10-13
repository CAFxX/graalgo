# P3-M7 wave-1 corpus: strings

Differential tests for the synthetic `strings` package
(`internal/frontend/synth_strings.go`).

## Programs

| File | Coverage |
|------|----------|
| 00_basic.go | Contains/HasPrefix/Index/Count/Compare/EqualFold/Clone |
| 01_case.go | ToUpper/ToLower/ToTitle/Map/ToValidUTF8/TrimSpace |
| 02_split_join.go | Split/SplitN/SplitAfter/Fields/Join/Replace/Repeat/Trim |
| 03_builder.go | Builder (single-result methods: WriteByte/Len/Cap/Grow/Reset/String) |
| 04_replacer.go | NewReplacer + Replace (byte, byte-string, generic, single) |
| 05_reader.go | Reader (Len/Size/Reset; multi-result methods omitted) |
| 06_adversarial.go | Empty, unicode edge cases, malformed UTF-8, huge inputs |
| 07_combined.go | Replacer+Builder, Split+Join, Trim+ToUpper, Fields+Join, Map+ReplaceAll |
| 08_panic_repeat.go | Repeat negative count panics |
| 09_panic_title.go | Title fail-fast panics |

## Limitations

Multi-result functions/methods (Cut, CutPrefix, CutSuffix, CutLast,
Builder.Write/WriteRune/WriteString, Reader.Read/ReadAt/ReadByte/ReadRune/
Seek) fail fast at export time under WS D and are not exercised here.
They have exact gc signatures and real implementations in the synthetic
source (validated by the gc differential in synthcheck), but the exporter
rejects calls until WS D lands.

## Running

```bash
./run_differential.sh
# or
../run_wave.sh strings
```
