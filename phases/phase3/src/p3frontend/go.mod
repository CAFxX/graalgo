module p3frontend

go 1.27

// The frozen gofrontend service (phases/phase1/frontend) is consumed via its
// frozen CLI contract (INTERFACE_FREEZE.md §2.1): the module is package main
// and therefore unimportable by construction, so p3frontend execs the
// gofrontend binary (see export.go runGofrontend). The require+replace below
// records the dependency for readers and for `go mod graph`.
// NOTE: `go mod tidy` removes the require (nothing imports a main-only
// module) while keeping the replace — that is expected; the durable record
// of the binary dependency is DESIGN.md §8, not this file.
require graalgo-frontend v0.0.0

replace graalgo-frontend => ../../../phase1/frontend
