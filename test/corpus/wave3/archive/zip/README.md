# P3-M9 wave-3 differential corpus (tier-5: archive/zip)

Wave-3 corpus for the stdlib `archive/zip` package (P3-M9 tier-5
worker). Conventions are the wave-1 ones (`../wave1/README.md`).
Pure Go over `bytes.Buffer` (`OpenReader` uses a hermetic temp dir);
no network.

## Programs

- `00_write_read.go` — files + dirs, `Store` and `Deflate`; `Reader`
  over bytes: `Name`, sizes, `CRC32`, `Modified` (MS-DOS 2s granularity
  pinned: odd second → even), `UncompressedSize64`; `OpenReader` on a
  temp-dir file (hermetic `MkdirTemp` + `defer RemoveAll`).
- `01_roundtrip_attrs.go` — `ExternalAttrs`, `CreatorVersion`, file +
  archive `Comment`, non-UTF8 names (`NonUTF8` flag, bytewise name),
  `Extra` fields preserved exactly; duplicate file names → both
  entries readable in order.
- `02_corrupt.go` — truncated central directory → `ErrFormat`;
  flipped data bit → `ErrChecksum` on body read (directory still
  parses); unknown method rejected at write time; `NewReader` with
  oversized size works (EOCD at physical end), undersized fails.
- `03_adversarial.go` — empty archive (valid, zero files); 0-length
  file (CRC32 0); data-descriptor streaming write via non-`io.Seeker`
  writer (descriptor signature pinned, read-back exact); zip-slip-style
  `../` and absolute names NOT sanitized by `Reader` (documented
  non-behavior); 65536-byte comment rejected, 65535 accepted and
  round-tripped.
- `04_direntry_info.go` — `fs.DirEntry`/`fs.File` interface dispatch
  (A-P1-11): `fs.ReadDir` on the zip root and inside a directory,
  `DirEntry.Info()` on dir and file entries, `Reader.Open(dir)` +
  `Stat` + `Read` (gc-exact "is a directory" `*fs.PathError`).

## Scope limits

- The synthetic `archive/zip` source exists in
  [internal/frontend/](../../../../../internal/frontend/) (`synth_archive_zip.go`),
  but the programs are `.scopelimited` until the carried FixedZone fix
  lands (branch `review-1105-zip-fixedzone`:
  `synth_archive_zip.go:2397`, `time.FixedZone`) — the synthetic package
  does not type-check without it. The gc leg validates each program is
  real deterministic Go with exit 0.
