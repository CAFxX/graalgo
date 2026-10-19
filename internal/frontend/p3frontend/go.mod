module p3frontend

go 1.27

// The frozen gofrontend service (internal/frontend) is consumed via its
// frozen CLI contract (INTERFACE_FREEZE.md §2.1): the module root is
// package main and therefore unimportable by construction, so p3frontend
// execs the gofrontend binary (see export.go runGofrontend). The shared
// assembly-strategy mechanism, however, is a real library import:
// graalgo-frontend/asmstrategy (a non-main subpackage of the same
// module) is imported directly (see asm.go), keeping a single generated
// inventory artifact (ADR-0007 addendum D1/D7).
require graalgo-frontend v0.0.0

replace graalgo-frontend => ../../../internal/frontend
