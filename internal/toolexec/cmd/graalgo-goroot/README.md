# graalgo-goroot — apply the stdlib fork (status: **new**)

Command over package [../../fork/](../../fork/): applies graalgo's
standard-library fork ([stdlib/](../../../../stdlib/README.md)) to an
upstream Go distribution. Modes: `-overlay` (a `go build -overlay` file, what
the tests use), `-o` (assemble the GOROOT a distribution ships), `-audit`
(fork-size report). `make goroot` runs it.
Design: [toolexec-design.md](../../../../docs/arch/toolexec-design.md) D8, §5.1.

**Test:** `cd internal/toolexec && go test ./fork`.
