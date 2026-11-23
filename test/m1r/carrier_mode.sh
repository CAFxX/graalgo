#!/usr/bin/env bash
# test/m1r/carrier_mode.sh — carrier mode (ADR-0018) through the -toolexec path,
# run by `make test-m1r` after run.sh: the programs of test/m1r/carrier/ under
# -Dgraalgo.sched.goroutines=carrier, against gc like any m1r program.
# spawn_flat.go is the scaling check: goroutine creation must cost the same
# with 1,000 and with 19,000 goroutines alive, and 20,000 spawns must take
# under 2 s (one polyglot thread per goroutine fails both).
#
# The programs are not in test/m1r/programs/: the current default goroutine
# mode (one thread per goroutine; GG-0073's fallback, the default until the
# carrier default flip lands) is quadratic in the live count and fails
# spawn_flat by design. The scheduler's semantics suites also run in
# carrier mode, at the Java level (graalgo.sched.CarriersTest).
#
# Usage: test/m1r/carrier_mode.sh [-v]
# Env:   as test/m1r/run.sh. Exit: run.sh's.
set -uo pipefail
HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
GRAALGO_JAVA_OPTS="${GRAALGO_JAVA_OPTS:-} -Dgraalgo.sched.goroutines=carrier" \
    exec "$HERE/run.sh" "$@" "$HERE"/carrier/*.go
