# P3-M8 crypto analysis (read-only worker, 2026-10-07)

Synthetic bring-up of Go `crypto/*` (emulate leg of the M6
emulate-then-intrinsify strategy; intrinsification deferred to Phase 5+).
Source of truth: `~/tools/go-1.27.1/src` (= `~/workspace/graalgo-stdlib/go1.27.1`,
verified identical). All 13 candidate packages build under `-tags=purego`
(real `go build`, linux/amd64), proving the pure-Go file selection below is
complete and has no dangling asm-symbol references.

## 1. The one architectural finding that shapes everything

Every `crypto/*` package in go1.27.1 is written against three build modes:

1. **boringcrypto** (`crypto/internal/boring`, cgo + BoringSSL) — `boring.Enabled`
   is `const available = false` in the standard toolchain (`notboring.go`);
   every `if boring.Enabled` branch is dead code and `boring.Unreachable()`
   is a documented no-op there. Trim: delete import + dead branches.
2. **FIPS 140-3 mode** (opt-in via `GODEBUG=fips140=...`) — `fips140.Enabled`
   is a `var` defaulting false; `fips140only.Enforced()` defaults false;
   with FIPS off, `crypto/internal/fips140/drbg.Read` degenerates to
   `sysrand.Read` (no DRBG at all), and all `fips140.RecordApproved/
   RecordNonApproved` + `CAST(...)` self-test `init`s are module bookkeeping.
   Trim: treat as false / delete calls. **Re-test trigger (record): if
   graalgo ever enables FIPS or boringcrypto mode, the entire crypto set
   must be re-verified — the synthetic sources implement the standard
   non-FIPS, non-boring, pure-Go path only.**
3. **pure-Go** (`-tags=purego`) — `*_generic.go` / `*_noasm.go` files provide
   complete pure-Go compression/block functions; `*_decl.go` + `.s` files
   are excluded by build tag. This is the file set the synthetic sources
   are derived from.

Consequence: the synthetic strategy is **vendored-copy-and-trim** for the
algorithm files (delete boring/FIPS branches, drop `cast.go` self-test
inits, drop `*_decl.go`/`*_asm.go` dispatch) plus **targeted rewrites**
for exactly four constructs the v1 frontend scope cannot express:
`unsafe` (2 sites), generics (2 sites), `//go:linkname` (3 sites), and the
`internal/`-only imports (inlined as unexported helpers per synthetic file).

## 2. Per-subpackage inventory (go1.27.1, linux/amd64, `-tags=purego`)

"Go files" = selected by the purego build. "Internal imports" = non-std
imports that must be inlined/trimmed. All rows verified with real `go list`
+ `go build`.

| # | package | pure-Go files (non-test) | internal imports | linkname | asm files (M6: emulate-then-intrinsify) |
|---|---|---|---|---|---|
| 1 | `crypto` (root) | `crypto.go` | `hash`, `io`, `strconv` (all synthetic/M7/M8) | none | 0 |
| 2 | `crypto/md5` | `md5.go`, `md5block.go`, `md5block_generic.go` (glue: `block`→`blockGeneric`) | `crypto/internal/fips140only` (Enforced), `internal/byteorder` | none | 8 (md5block_*.s) |
| 3 | `crypto/sha1` | `sha1.go`, `sha1block.go`, `sha1block_generic.go` (glue) | `crypto/internal/boring` (dead), `crypto/internal/fips140only` (Enforced), `internal/byteorder` | none | 7 (sha1block_*.s) |
| 4 | `crypto/sha256` | wrapper `sha256.go` + `crypto/internal/fips140/sha256/{sha256.go, sha256block.go, sha256block_noasm.go}` | `crypto/internal/boring` (dead), `crypto/internal/fips140` (RecordApproved), `crypto/internal/fips140deps/byteorder` | none | 7 (fips140/sha256/*.s) |
| 5 | `crypto/sha512` | wrapper `sha512.go` + `crypto/internal/fips140/sha512/{sha512.go, sha512block.go, sha512block_noasm.go}` | same pattern as sha256 | none | 6 (fips140/sha512/*.s) |
| 6 | `crypto/sha3` | wrapper `sha3.go` + `crypto/internal/fips140/sha3/{sha3.go, hashes.go, shake.go, keccakf.go, sha3_noasm.go}` | `crypto/internal/fips140` (Record*), `crypto/internal/fips140/subtle` | `fips140hash_sha3Unwrap` (wrapper indirection — droppable, §3) | 3 (fips140/sha3/*.s) |
| 7 | `crypto/subtle` | `constant_time.go`, `xor.go`, `dit.go` | `crypto/internal/constanttime` (pure Go), `crypto/internal/fips140/subtle` (ConstantTimeCompare/Copy + XORBytes) | `setDITEnabled`/`setDITDisabled` in `dit.go` (runtime-provided) | 0 in package; fips140/subtle has 5 (xor_*.s) |
| 8 | `crypto/hmac` | wrapper `hmac.go` + `crypto/internal/fips140/hmac/{hmac.go, cast.go→drop}` | `crypto/internal/boring` (dead), `crypto/internal/fips140` (Record*), `crypto/internal/fips140/sha{256,512,3}` (digest types), `crypto/internal/fips140hash` (UnwrapNew), `crypto/internal/fips140only` | none | 0 |
| 9 | `crypto/cipher` | `cipher.go`, `cbc.go`, `cfb.go`, `ctr.go`, `ofb.go`, `io.go`, `gcm.go` | `crypto/internal/fips140/aes` (AES fast-path dispatch), `crypto/internal/fips140/aes/gcm` (GCM for AES + `GHASH`), `crypto/internal/fips140/alias` (overlap), `crypto/internal/fips140only`, `internal/byteorder`, `crypto/subtle` | none | 0 in package (but see aes) |
| 10 | `crypto/aes` | wrapper `aes.go` + `crypto/internal/fips140/aes/{aes.go, aes_generic.go, cbc.go, ctr.go, const.go}` | `crypto/internal/boring` (dead), `crypto/internal/fips140` (Record*), `crypto/internal/fips140/alias`, `crypto/internal/fips140/subtle` | none | 6 (aes_*.s, ctr_*.s) |
| 11 | `crypto/des` | `block.go`, `cipher.go`, `const.go` (fully pure Go, zero asm) | `crypto/internal/fips140/alias`, `crypto/internal/fips140only`, `internal/byteorder` | none | 0 |
| 12 | `crypto/rc4` | `rc4.go` (fully pure Go, zero asm) | `crypto/internal/fips140/alias`, `crypto/internal/fips140only` | none | 0 |
| 13 | `crypto/rand` | `rand.go`, `text.go`, `util.go` | `crypto/internal/boring` (dead), `crypto/internal/fips140/drbg` (degenerates to sysrand when FIPS off), `crypto/internal/rand`+`sysrand` (getrandom loop), `crypto/internal/fips140only`, `math/big` (util.go: `Int`, `Prime`) | `fatal` (body in runtime — synthetic provides one) | 0 |
| — | `crypto/ecdh` | `ecdh.go`, `nist.go`, `x25519.go` + fips140 `ecdh/nistec/edwards25519` | fips140 module tree, `crypto/internal/rand` | none | 4 (nistec) + 1 (edwards25519/field) + 2 (boring/sig) |
| — | `crypto/ecdsa` | `ecdsa.go`, `ecdsa_legacy.go` (+boring/notboring) | `math/big`, fips140 `ecdsa/bigmod`, `encoding/asn1` (M7 done) | none | 8 (bigmod) + 1 (ecdsa) |
| — | `crypto/rsa` | `rsa.go`, `pkcs1v15.go`, `pss.go`... (+boring/notboring, fips.go) | `math/big`, fips140 `rsa`, `crypto/rand`, `hash` | none | (bigmod shared) |
| — | `crypto/x509` | 14 files | `net`, `net/netip`, `net/url`, `os`, `time`, full asymmetric set | — | 2 (x509/internal/macos — irrelevant) |
| — | `crypto/tls` | 21 files | `net`, `crypto/hpke`, `crypto/mldsa`, `crypto/mlkem`, `crypto/hkdf`, full asymmetric set | — | 0 |

Not in the task's list, noted for completeness: `crypto/dsa`, `crypto/ed25519`,
`crypto/elliptic`, `crypto/hkdf`, `crypto/pbkdf2`, `crypto/mldsa`,
`crypto/mlkem`, `crypto/hpke`, `crypto/boring`, `crypto/fips140`,
`golang.org/x/crypto` (toolchain-vendored, out of scope). All DEFERRED (M9+).

Internal packages referenced by the IN-scope set — disposition (none are
imported as packages; all are inlined/trimmed into the synthetic sources):

1. `crypto/internal/fips140only` → trim to `false` const (its `Enforced()`
   calls `crypto/fips140.Enforced()` → GODEBUG-gated, default false).
2. `crypto/internal/boring` → delete; all branches dead (`Enabled=false` const).
3. `crypto/internal/constanttime` → inline (42 lines, pure Go, no asm).
4. `crypto/internal/fips140/subtle` → inline `constant_time.go` (75 lines);
   **rewrite** `xor_generic.go` (uses generics `xorLoop[T byte|uintptr]` +
   `unsafe` in `words()`) as a plain byte loop — behavior-identical, perf-only
   divergence (intrinsify leg later).
5. `crypto/internal/fips140/alias` → **rewrite**; see risk §5.2 (needs
   `unsafe` for exact parity — conditional on the M8 unsafe track).
6. `crypto/internal/fips140/sha256|sha512|sha3` → inline trimmed (drop
   `cast.go` self-test inits, `RecordApproved` calls, `sha3_amd64.go` dispatch).
7. `crypto/internal/fips140/aes` (+`aes/gcm`'s `GHASH` only) → inline trimmed.
8. `crypto/internal/fips140/hmac` → **rewrite** non-generic (internal
   `New[H hash.Hash]` uses generics; public signature doesn't).
9. `crypto/internal/fips140` (module pkg), `crypto/internal/fips140hash`,
   `crypto/internal/fips140deps/*` (except byteorder helpers),
   `crypto/internal/boring/*`, `crypto/internal/rand`, `crypto/internal/fips140/drbg`
   → **not needed** (all uses trim away under non-FIPS/non-boring).
10. `internal/byteorder`, `crypto/internal/fips140deps/byteorder` → inline
    5-line BE/LE helpers per synthetic file (both are unsafe-free).

New synthetic dependencies the crypto set needs: `hash` (92 lines, trivial:
`Hash`/`Hash32`/`Hash64`/`Cloner`/`XOF` interfaces over `io`), `crypto` root
(`Hash` consts, `RegisterHash`/`Available`, `Signer` — needs `io` + `strconv`,
both available), and the M8 `io` track (`hash.Hash` embeds `io.Writer`;
`cipher/io.go` needs `io.Reader/Writer`; `crypto.SignerOpts`... `crypto.Signer`
uses `io.Reader`).

## 3. Proposed M8 scope

### IN (recommend)

1. `hash` (new synthetic) + `crypto` (root) — prerequisites, ~300 lines combined.
2. `crypto/md5`, `crypto/sha1` — trivially synthetic-ready: self-contained
   pure-Go (`blockGeneric` complete in `md5block.go`/`sha1block.go`; the
   `_generic.go` files are 5-line glue); only trims are `fips140only` and dead
   boring branches. ~700 lines each incl. comments.
3. `crypto/sha256`, `crypto/sha512` — wrapper (1 file each) + trimmed
   fips140 digest (`sha256.go`/`sha512.go` minus `RecordApproved`, minus
   `cast.go`) + `sha256block_noasm.go` verbatim. Digest API includes
   `MarshalBinary/AppendBinary/UnmarshalBinary/Clone` — needed for HMAC.
4. `crypto/sha3` — wrapper minus the `fips140hash_sha3Unwrap` linkname
   (the linkname exists so `fips140hash.Unwrap` can map public `*SHA3` to the
   internal digest; the synthetic sha3 exposes the digest type directly, so
   the indirection — and the `fips140hash` package — vanish) + trimmed
   fips140/sha3; `keccakf.go`'s `unsafe` becomes the big-endian copy path
   (always copy through `[25]uint64`; the LE alias was perf-only).
5. `crypto/subtle` — inline `constanttime` + fips140 `constant_time.go`;
   plain-loop `XORBytes` rewrite; `dit.go` trimmed to `func
   WithDataIndependentTiming(f func()) { f() }` (`sys.DITSupported` is
   false on amd64; DIT is ARM-FEAT_DIT-only and not differentially
   observable — record re-test trigger for arm64+FEAT_DIT hosts).
6. `crypto/hmac` — rewrite of public `hmac.go` + internal `hmac.go` as one
   non-generic file against the synthetic digest types. Keep: the
   recover-probed `unique` check in `New` (panics
   `"crypto/hmac: hash generation function does not produce unique values"`);
   the `marshaled` fast path (Reset→MarshalBinary, Sum→UnmarshalBinary);
   `Clone` with `errCloneUnsupported` wrapping `errors.ErrUnsupported`.
   Drop: `fips140hash.UnwrapNew` (no wrapper indirection), `MarkAsUsedInKDF`
   (unexported `*HMAC` has no synthetic caller — but keep it? It's exported
   in the internal package only; public `hmac` has no MarkAsUsedInKDF.
   Drop), key-length FIPS checks.
7. `crypto/cipher` (CBC/CTR/CFB/OFB + `io.go` + GCM) — public files trimmed
   (`fips140only`→false, `internal/byteorder`→helpers, `subtle`→synthetic).
   AES fast-path dispatch: the public modes type-assert `*aes.Block`;
   against the synthetic `crypto/aes` this keeps working if synthetic aes
   provides the same method set (`NewCBCEncrypter` etc. ported from
   fips140/aes, trimmed). **GCM simplification (verified): route ALL GCM
   through `newGCMFallback`** — under purego the internal `gcm.GCM` uses the
   identical generic seal/open (the fallback is documented as "a copy of the
   generic implementation"); constructor errors are byte-identical across both
   (`"cipher: incorrect tag size given to GCM"`, `"cipher: the nonce can't
   have zero length"`, `"cipher: NewGCM requires 128-bit block cipher"`);
   both `clear(out)` on tag-mismatch. Only port needed from
   `fips140/aes/gcm`: `GHASH`/`ghash` (~180 lines, `RecordNonApproved` trimmed;
   `gcm_nonces.go`/`cmac.go`/`ctrkdf.go` not needed).
8. `crypto/aes` — wrapper trimmed + fips140/aes core (`aes.go`,
   `aes_generic.go`, `cbc.go`, `ctr.go`, `const.go`) minus `Record*`/`alias`
   (rewritten) — the `Block` type plus `Encrypt/Decrypt/EncryptBlockInternal`.
9. `crypto/des`, `crypto/rc4` — fully pure Go already (0 asm in go1.27.1);
   trims are `fips140only` + `alias` only. des is needed later by x509/pkcs;
   rc4 is ~100 lines (keep for `cipher.Stream` completeness; note upstream
   `Reset` deprecation is preserved as-is).
10. `crypto/rand` — **conditionally IN**: `Reader`/`Read`/`Text` real,
    backed by the syscall track's getrandom (or a runtime entropy intrinsic;
    re-test trigger on whichever lands). With FIPS off the DRBG is dead code
    (verified: `drbg.Read` → `sysrand.Read` directly), so no DRBG port is
    needed. `Int`/`Prime` (need `math/big`) ship as fail-fast with re-test
    trigger (M7 `errors.As` precedent). The pre-3.17 `urandom` fallback and
    the `warnBlocked` timer may be dropped (modern host kernel; document).
    `fatal` gets a real body (`panic`, closest available to `runtime.fatal`;
    divergence only on the untestable entropy-failure path).

### DEFERRED to M9+ (with re-test triggers)

1. `crypto/ecdh`, `crypto/ecdsa`, `crypto/rsa` (+ `dsa`, `ed25519`,
   `elliptic`, `hkdf`, `pbkdf2`, `mldsa`, `mlkem`, `hpke`) — asymmetric
   crypto defers as one coherent M9 milestone: ecdsa/rsa need `math/big`
   (not in M8's 5 tracks); ecdh needs `crypto/rand` keygen + the large
   fips140 nistec fiat tree; all need the M8 crypto core beneath them.
   Re-test triggers: `math/big` lands → rsa/ecdsa; `crypto/rand` Reader
   lands → keygen paths; M8 corpus re-runs green beneath them.
2. `crypto/x509`, `crypto/tls` — **confirm: wave 3 (P3-M9, net)** per PLAN.md.
   Independent reasons beyond schedule: x509 needs `net`/`net/netip`/
   `net/url`/`os`/`time`; tls needs `net` + `crypto/hpke|mldsa|mlkem|hkdf`
   (post-quantum!) + the full asymmetric set. Neither can precede its
   dependencies; both are 14–21-file state machines.
3. `crypto/internal/*` as importable packages — never; they are
   inline/trim source donors only (importing them would drag the fips140
   module `init` — `godebug` + self-tests — into the synthetic scope).

## 4. Extraction obstacles per IN package (and how the synthetic avoids them)

| package | obstacle | avoidance |
|---|---|---|
| md5/sha1 | `fips140only.Enforced()` (4 sites); dead `boring` branches; `boring.Unreachable()` no-op | vendored-copy-and-trim: replace with `false`, delete branches |
| sha256/sha512 | wrapper imports fips140 digest (module-coupled); `RecordApproved`; `cast.go` self-test `init` | copy digest+block files, drop `cast.go`, delete `Record*`; wrapper keeps exact public API |
| sha3 | `//go:linkname fips140hash_sha3Unwrap`; `unsafe` in keccakf (LE fast path); fips140 coupling | drop linkname (no wrapper indirection in synthetic); keccakf always-copy rewrite |
| subtle | linkname'd `setDITEnabled/Disabled` (runtime-provided); generics+`unsafe` in `xor_generic.go` | `dit.go`: `f()` directly; `XORBytes`: plain byte-loop rewrite |
| hmac | internal `New` is generic; `fips140hash.UnwrapNew`; digest types live in fips140 | rewrite non-generic against synthetic digests; drop Unwrap |
| cipher/aes | AES fast-path dispatch on `*aes.Block` (internal type); `alias` overlap checks (`unsafe`); GCM split across public+internal | synthetic aes exposes same method set; GCM routed via `newGCMFallback` + ported `GHASH`; `alias` → §5.2 |
| des/rc4 | `alias.InexactOverlap` only | same `alias` rewrite |
| rand | `//go:linkname fatal`; `math/big` in util.go; entropy via `internal/syscall/unix`+`syscall` | real body for `fatal`; `Int`/`Prime` fail-fast; entropy from M8 syscall track / runtime intrinsic |
| `crypto` root | needs `hash`, `io` (M8 track), `strconv` (M7) | dependency note for the coordinator |

## 5. The 3 hardest behavioral risks

**R1. HMAC's `marshaled` fast path + digest marshal formats (highest).**
`HMAC.Reset` marshals inner/outer digest state (`MarshalBinary`) when the
digest supports it and later `Sum` restores via `UnmarshalBinary`; the
`marshaled` flag flips the entire Reset/Sum code path. The wire format is
magic-prefixed (`"sha\x02"`/`"sha\x03"`…), and `UnmarshalBinary` error
strings (`"crypto/sha256: invalid hash state"` etc.) are part of the
observable surface. A 1-byte divergence here is invisible to all KATs and
only fires on the Reset-after-Sum sequence
(`h.Sum(nil); h.Reset(); h.Write(x); h.Sum(nil)`). Mitigation: the corpus
must hammer this path for every (hmac × {sha256, sha512, sha3-256, md5,
sha1}) combination — md5/sha1 take the *slow* path (no BinaryMarshaler),
which is itself a branch-parity check — plus tampered-marshal error cases.

**R2. Buffer-overlap panic parity needs `unsafe` (hard dependency).**
15+ call sites panic on inexact overlap (`"crypto/cipher: invalid buffer
overlap"`, `"crypto/des: invalid buffer overlap"`, `"crypto/rc4: ..."`,
`"crypto/aes: ..."`, GCM's two overlap panics); the check is
`alias.InexactOverlap`/`AnyOverlap`, i.e. raw pointer comparison. Without
`unsafe` there is no correct conservative approximation: always-panic
breaks normal disjoint dst/src use; never-panic turns gc's fail-fast misuse
detection into silent wrong output (violates "fail fast, NEVER silent").
Exact parity **requires** the M8 unsafe track (the check is 6 lines with
`unsafe.Pointer`; `&x[0]==&y[0]` alone is insufficient — it misses the
disjoint case). If unsafe slips: ship with the divergence documented and
mark all overlap-panic corpus programs `.scopelimited` (`retest: unsafe`),
re-test trigger armed. Note `subtle.XORBytes` is the exception — overlapping
dst/src is *legal* there (plain loop handles it).

**R3. GCM fallback-routing edge parity (medium, verifiable).**
Routing AES-GCM through `newGCMFallback` instead of porting internal
`gcm.GCM` is output-identical (verified: same constructor errors, same
generic algorithm, same `clear(out)`-on-mismatch), but three edges need
explicit differential proof during implementation: (a) exact-overlap
`Seal`/`Open` (dst==src / dst==ciphertext — the internal path documents a
memmove for the random-nonce wrapper; the fallback's block-at-a-time loop
must be shown equivalent); (b) `NewGCMWithRandomNonce` (needs the DRBG
entropy decision from §3.10 — port `SealWithRandomNonce`'s overlap checks +
memmove faithfully); (c) non-standard nonce/tag sizes via
`NewGCMWithNonceSize`/`NewGCMWithTagSize` (`deriveCounter`'s GHASH-of-nonce
path for nonce ≠ 12). Corpus programs 10–12 below target exactly these.

## 6. Differential testing — corpus sketch (wave2 `crypto/<pkg>/`)

Crypto is deterministic given inputs (except `rand`) — ideal for
byte-identical differential testing. Hex-encode all digests/tags; print
fixed strings only. Numbering follows wave2 `<nn>_<name>.go` convention.

1. `00_hash_kat_empty` — md5/sha1/sha256/sha512/sha3-224/256/384/512 of
   empty input (FIPS KATs).
2. `01_hash_kat_abc` — `"abc"` (+ `"abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq"`
   for the 2-block SHA-256/512 vectors).
3. `02_hash_chunking` — 100 003-byte input written as one-shot vs chunks of
   1/63/64/65/129 bytes; all digests must agree (buffering logic).
4. `03_hash_state` — `Sum` doesn't mutate (write, Sum, write more, Sum);
   `Reset` reuse; `Size`/`BlockSize` table.
5. `04_hash_marshal` — sha256/sha512/sha3 mid-stream `MarshalBinary` →
   continue both original and `UnmarshalBinary` copy → equal digests;
   tampered/short/truncated inputs → printed error strings (byte-parity).
6. `05_hmac_rfc4231` — RFC 4231 cases 1–4 (HMAC-SHA-256) + RFC 2202 MD5/SHA-1
   vectors.
7. `06_hmac_edge` — empty key, 1-byte key, key == blocksize, key > blocksize
   (key-hashing path), empty message; `hmac.Equal` true/false/length-mismatch.
8. `07_hmac_state_clone` — `Sum`→`Reset`→`Write`→`Sum` vs fresh (R1!);
   `Clone()` mid-stream for sha256 (ok) and md5 (`errCloneUnsupported`
   wrapping `errors.ErrUnsupported` — print `errors.Is(err,
   errors.ErrUnsupported)`); non-unique hash func → `.panic`
   `"crypto/hmac: hash generation function does not produce unique values"`.
9. `08_aes_kat` — FIPS-197 AES-128/192/256 encrypt+decrypt KATs; bad key
   sizes 15/17/31/33 → `KeySizeError` string parity.
10. `09_modes_roundtrip` — CBC/CTR/CFB/OFB × {AES-128, TripleDES} × lengths
    {0,1,15,16,17,31,32,33}: encrypt→decrypt round-trip; in-place
    (dst==src); IV tamper → mismatch; CBC non-multiple-of-block → `.panic`;
    bad IV length → `.panic`.
11. `10_gcm_kat` — NIST GCM test vectors (empty pt, 16-byte pt, with AAD);
    tampered tag → `"cipher: message authentication failed"`; tampered AAD
    → same; `NewGCMWithTagSize` 12/16, tag size 11/17 → `.panic`; wrong
    nonce size → `.panic`; exact-overlap Seal/Open (R3a).
12. `11_gcm_fallback` — GCM over a non-AES 16-byte test cipher defined in
    the program (exercises `newGCMFallback`); `NewGCM(desCipher)` →
    `"cipher: NewGCM requires 128-bit block cipher"`.
13. `12_overlap_panics` — inexact-overlap dst/src for CBC/CTR/CFB/OFB/DES/
    RC4/AES-Encrypt → `.panic` with exact first stderr lines. **Canary for
    R2**: `.scopelimited` (`retest: unsafe`) if the unsafe track slips.
14. `13_subtle_rand_registry` — `ConstantTimeCompare`/`Equal`/`Select`/
    `LessOrEq`/`Copy`/`XORBytes` vectors incl. empty inputs;
    `WithDataIndependentTiming` runs f (and propagates panics);
    `rand.Read` lengths {0,1,1000} all succeed, `rand.Text()` is 26 chars
    from the base32 alphabet; `crypto.SHA256.Available()` true after import,
    `Hash.Size()` table, custom `RegisterHash` round-trip.

`rand` programs assert fixed properties only (lengths, alphabets, no-error),
never values. `NewGCMWithRandomNonce` programs assert structural properties
(nonce prefix length 12, round-trip) unless entropy is unavailable.

## 7. `math/big` recommendation: defer asymmetric crypto to M9, do NOT vendor a minimal big

`math/big` is not among M8's 5 tracks, and `crypto/ecdsa` + `crypto/rsa`
need it pervasively (`rsa.go`, `pkcs1v15.go`, `pss` use `big.Int` as the
core representation — there is no "small" subset that preserves API).
Vendoring a minimal big for M8 would be a throwaway fork: it must then be
deleted when real `math/big` lands, it doubles the differential surface,
and it violates the no-placeholders bar (a "minimal big" is a stub by
another name). The M6 inventory already prices `math/big` as `jvm-math`
(mostly-Go + asm speedups) — a clean M9 bring-up. **Recommend: defer
`ecdh`/`ecdsa`/`rsa`/`ed25519`/`elliptic`/`dsa`/`hkdf`/`pbkdf2`/`mldsa`/
`mlkem`/`hpke` as one M9 asymmetric milestone** ("remaining" packages),
after M8's hash+cipher+rand core is green beneath them. Note `ecdh` is the
only asymmetric package not needing `math/big` (fips140 nistec fiat code),
but it still needs `crypto/rand` keygen and is large — keep it in the M9
batch for coherence. Re-test triggers: `math/big` lands → rsa/ecdsa;
M8 crypto corpus stays green as the regression floor.

## 8. Cross-track dependencies (for the coordinator)

1. **`io` track (M8)**: `hash.Hash` embeds `io.Writer`; `cipher/io.go`
   (`StreamReader`/`StreamWriter`) needs `io.Reader/Writer`; `crypto.Signer`
   uses `io.Reader`; `rand.Reader` is an `io.Reader`. Crypto cannot type-check
   without synthetic `io`.
2. **syscall track (M8)**: `crypto/rand` entropy = getrandom(2) loop
   (`internal/syscall/unix` + `syscall`); alternative: a runtime entropy
   intrinsic — either way, record which one the implementation uses.
3. **unsafe track (M8)**: exact `alias` overlap checks (R2). Preferred:
   crypto waits for / depends on unsafe; fallback documented in §5 R2.
4. **`reflect` track**: not needed for the IN-scope set (no reflect in any
   pure-Go file selected).
5. **`os`/`time`**: only via sysrand's droppable `warnBlocked` timer and the
   pre-3.17 urandom fallback — no hard dependency.

## 9. Sizing

~6 500 lines of stdlib source feed the synthetic authoring; the synthetic
output is smaller (boring/FIPS/`cast.go`/dispatch files drop out). Largest
single ports: sha3 (~1 100 lines incl. keccak), aes core (~700), cipher+gcm
(~1 100), hmac rewrite (~300). md5/sha1/des/rc4/subtle are small. Estimated
13 new `synth_crypto_*.go` files + `synth_hash.go` + `synth_crypto.go`
(root), registered in `syntheticSources` at rebase time (pipelining
constraint: no shared-file edits until M7 remediation lands).
