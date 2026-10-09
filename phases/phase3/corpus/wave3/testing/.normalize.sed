# testing/ output normalization (applied with sed -E to BOTH legs before
# cmp in run_wave3.sh). Each rule is tied to a documented scope-limit in
# the synthetic testing source header (synth_testing.go).
#
# 1. gc prefixes every log line with "file:line: "; the synthetic testing
#    omits it (no runtime.Caller in the frozen runtime).
s/^((    )+)[^ ]+:[0-9]+: /\1/
# 2. Test durations vary run to run: "--- PASS: X (0.00s)", "--- FAIL: X (1.23s)".
s/ \([0-9]+\.[0-9]+s\)//g
# 3. gc names benchmarks "Name-N" (N = runtime.GOMAXPROCS); the frozen
#    runtime has no GOMAXPROCS, so the synthetic testing omits the suffix.
s/^(Benchmark[A-Za-z0-9_\/]*)-[0-9]+/\1/
# 4. Benchmark timing values vary run to run.
s/[0-9][0-9.,]* ns\/op/0 ns\/op/
s/[0-9][0-9.,]* MB\/s/0 MB\/s/
