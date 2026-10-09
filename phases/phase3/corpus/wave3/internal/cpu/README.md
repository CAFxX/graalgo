# wave-3 `internal/cpu` corpus

Scope-limited corpus for `internal/cpu`. There is no synthetic adapter:
CPU feature detection needs cpuid/hwcap intrinsics, and no honest
synthetic values exist (the backend runs on the JVM; Go-visible feature
bits would be fiction). There is no in-tree consumer.

- `00_cpu.go` is `.scopelimited` + `nogc` (external programs cannot import
  `internal/*` at all):
  `reason: external programs cannot import internal/* (go tool
  restriction); no synthetic adapter — CPU feature detection needs
  cpuid/hwcap intrinsics with no honest synthetic values, and there is no
  in-tree consumer`,
  `retest: internal/cpu backend support lands (feature-detection
  intrinsics)`.
- The program is an API-surface pin (`CacheLineSize`, the `X86`/`ARM64`
  records); values are host-dependent and are not pinned. Build-checked
  against the real package in the tier1d harness.
