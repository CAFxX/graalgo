module p3frontend

go 1.27

// The frozen gofrontend service (phases/phase1/frontend) is consumed via its
// frozen CLI contract (INTERFACE_FREEZE.md §2.1): the module is package main
// and therefore unimportable by construction, so p3frontend execs the
// gofrontend binary (see export.go runGofrontend). The require+replace below
// records the dependency for readers and for `go mod graph`.
require graalgo-frontend v0.0.0

replace graalgo-frontend => ../../../phase1/frontend
