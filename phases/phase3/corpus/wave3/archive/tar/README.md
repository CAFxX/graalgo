# P3-M9 wave-3 differential corpus (tier-5: archive/tar)

Wave-3 corpus for the stdlib `archive/tar` package (P3-M9 tier-5
worker). Conventions are the wave-1 ones (`../wave1/README.md`).
Pure Go over `bytes.Buffer`; no filesystem, no network.

## Programs

- `00_write_read.go` — write+read round-trip of every `Typeflag`
  (Reg, Dir, Symlink, Hardlink, Char, Block, Fifo): `Name`, `Size`,
  `Mode`, `Uid`/`Gid`, `Uname`/`Gname`, `ModTime` (USTAR 1s truncation
  pinned), `Linkname`, `Devmajor`/`Devminor`; auto format selection
  (150-char name → PAX); `Xattrs` → PAX round-trip; `PAXRecords`
  verbatim round-trip.
- `01_roundtrip_formats.go` — forced `FormatUSTAR` (field round-trip,
  1s `ModTime` truncation), `FormatPAX` (nanosecond `ModTime`, 150-char
  name preserved), `FormatGNU` (200-char name via `././@LongLink`
  record), USTAR rejects 9GB `Size` ("cannot encode" error class),
  PAX accepts it.
- `02_corrupt.go` — truncated mid-archive → `io.ErrUnexpectedEOF`;
  truncated header → `io.ErrUnexpectedEOF`; flipped name byte →
  `tar.ErrHeader`; trailing garbage after the two zero blocks ignored;
  `Next` past end stays `io.EOF`; empty input → `io.EOF`.
- `03_adversarial.go` — sizes at 512-block edges (0, 511, 512, 513,
  1023, 1024); names of exactly 100 vs 101 chars (USTAR vs PAX);
  `FormatUnknown` auto-selects (not an error); write-contract
  violations (`ErrWriteTooLong` for over-size and pre-header writes,
  `ErrWriteAfterClose`, idempotent `Close`); GNU sparse PAX records
  carried as plain records (reader does not interpret them —
  documented non-behavior); `Close` with no files → valid 1024-byte
  empty archive.

## Scope limits

- No synthetic `archive/tar` source exists in frontend/v1, so all
  programs are `.scopelimited`; the gc leg validates each is real
  deterministic Go with exit 0. `retest:` synthetic `archive/tar`
  lands in `internal/frontend`, coordinated with the parallel
  `compress/*` worker (the tar corpus exercises the std reader/writer
  only; no compress imports in the corpus programs themselves).
