# `compress/gzip` wave-3 corpus

Differential corpus for the synthetic `compress/gzip` package (pure-Go
port of gc 1.27.1, value-identical to gc).

## Scope boundary

The whole package is in scope: `NewWriter`/`NewWriterLevel`, `Writer`
(`Write`/`Flush`/`Close`/`Reset` + `Header`), `NewReader`, `Reader`
(`Read`/`Close` + header fields + `Multistream`). No `.scopelimited`
programs.

## Programs

- `00_roundtrip_levels.go` — every level -2..9; header fields round-trip
  (`Name`, `Comment`, `ModTime` truncated to 1s, `OS`); multistream
  (two concatenated members, different levels).
- `01_header.go` — XFL byte mapping (2 = best, 4 = fastest); full
  `Writer.Header` round-trip; empty name (no FNAME flag); OS values.
- `02_corrupt.go` — bad magic -> "gzip: invalid header"; truncated ->
  `io.ErrUnexpectedEOF`; flipped payload bit -> "gzip: invalid
  checksum"; empty input -> `io.EOF` from `NewReader`.
- `03_adversarial.go` — 64KB comment: writer accepts it, reader rejects
  with "gzip: invalid header" (512-byte header buffer); empty name;
  Latin-1 name round-trip; non-Latin-1 name -> Write error; pre-epoch
  `ModTime`; double `Close` -> nil (both); `Reset` reuse; 1-byte reads.
