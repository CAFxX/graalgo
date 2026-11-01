module p3frontend

go 1.27

// Test-only module (lane D, review P1-4): a drift harness pinning v1's
// synthetic stdlib (internal/frontend/synth_*.go) against real gc
// 1.27.1. It contains no frontend code and no dependencies — the
// former importer/loader/flattener/exporter and their synthetic
// mirrors were deleted with the p3frontend product pretence. Tests
// read the v1 synth files from ../ and GOROOT from the toolchain.
