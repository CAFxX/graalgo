#!/usr/bin/env bash
# Layer-3 per-release gate (docs/arch/phases/phase3/PLAN.md §5 layer 3, GG-0029/GG-0032):
# stdlib differential via the wave1/wave2/wave3 corpora.
#
# The wave corpora are the operational form of the "stdlib + scripts" layer:
# curated differential programs per stdlib package (wave1: strings/bytes/
# strconv/..., wave2: io/os/syscall/reflect/..., wave3: net/http/compress/...),
# each run under stock gc 1.27.1 vs graalgo P3SyncMain (gmp=1 and gmp=4),
# comparing stdout bytes, exit codes, and panic lines per the differential
# oracle (GG-0030).
#
# NOTE (honest scope): literal `go test ./...` execution under graalgo awaits
# the package-mode launcher (PLAN §5's `graalgo-run --gmp=N ./pkg/...`;
# P3SyncMain is single-file today). The wave corpora are the runnable
# differential net over the stdlib until that lands; tracked in LAYERS.md.
#
# Four-way triage per red (PLAN §5): every FAIL line is classified as
#   graalgo-bug / upstream-change / backend-specific+reason-code /
#   nondeterminism-normalization
# and recorded in the JSON report. A red gate means: triage first, then fix
# or exclusion-list with a reason code (GG-0033).
#
# Exit status: 0 = all waves green; 1 = any wave red (report has the triage).
#
# Environment:
#   GR           repo root (default: this repo's root)
#   GO_BIN_DIR   Go toolchain bin dir (default: $HOME/tools/go/bin)
#   JAVA_BIN     java binary (default: $HOME/tools/graalvm-jdk-25/bin/java)
#   OUTDIR       report dir (default: $GR/.out/stdlib-gate/layer3-<ts>)
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
GR="${GR:-$REPO_ROOT}"
export GR
export GO_BIN_DIR="${GO_BIN_DIR:-$HOME/tools/go/bin}"
export JAVA_BIN="${JAVA_BIN:-$HOME/tools/graalvm-jdk-25/bin/java}"
# The wave runners invoke internal/backend/graalgo-sync-run, which resolves
# java via GRAALVM_HOME (not JAVA_BIN); derive it when unset.
export GRAALVM_HOME="${GRAALVM_HOME:-${JAVA_BIN%/bin/java}}"
export PATH="$GO_BIN_DIR:$PATH"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
OUTDIR="${OUTDIR:-$GR/.out/stdlib-gate/layer3-$TS}"
mkdir -p "$OUTDIR"

# Tmp discipline: never write to /tmp (512M tmpfs on the VM).
export TMPDIR="${TMPDIR:-$OUTDIR/tmp}"
mkdir -p "$TMPDIR"
export GOCACHE="${GOCACHE:-$HOME/workspace/.gocache-stdlib-gate}"
mkdir -p "$GOCACHE"

echo "== [0/3] build =="
make -C "$GR" build > "$OUTDIR/build.log" 2>&1 || {
  echo "GATE RED: build failed (see $OUTDIR/build.log)"
  exit 1
}

run_wave() {
  local wave="$1" runner="$2" log="$3"
  echo "== [wave] $wave =="
  # The wave runners print FAIL lines and exit non-zero on any failure.
  set +e
  "$runner" all > "$log" 2>&1
  local rc=$?
  set -e
  tail -3 "$log"
  return $rc
}

overall=0
run_wave "wave1" "$GR/test/corpus/wave1/run_wave.sh" "$OUTDIR/wave1.log" || overall=1
run_wave "wave2" "$GR/test/corpus/wave2/run_wave.sh" "$OUTDIR/wave2.log" || overall=1
run_wave "wave3" "$GR/test/corpus/wave3/run_wave3.sh" "$OUTDIR/wave3.log" || overall=1

echo "== [3/3] triage report =="
python3 - "$OUTDIR" "$overall" <<'EOF'
import json, re, sys, datetime
outdir, overall = sys.argv[1], sys.argv[2]
report = {
    "layer": 3,
    "name": "stdlib + scripts (wave corpora differential)",
    "timestamp": datetime.datetime.utcnow().isoformat() + "Z",
    "go_version": "1.27.1",
    "waves": {},
    "failures": [],
    "verdict": "GREEN" if overall == "0" else "RED",
}
for wave in ("wave1", "wave2", "wave3"):
    try:
        log = open(f"{outdir}/{wave}.log").read()
    except OSError:
        report["waves"][wave] = {"status": "no-log"}
        continue
    m = re.search(r"== differential: (\d+) passed, (\d+) scope-limited, (\d+) failed ==", log)
    fails = [l for l in log.splitlines() if l.startswith("FAIL ")]
    report["waves"][wave] = {
        "status": "RED" if fails else "GREEN",
        "passed": int(m.group(1)) if m else -1,
        "scope_limited": int(m.group(2)) if m else -1,
        "failed": int(m.group(3)) if m else len(fails),
    }
    for f in fails:
        # Four-way triage placeholder: the gate runner records the raw
        # failure; classification (graalgo-bug / upstream-change /
        # backend-specific / nondeterminism) happens at triage time and is
        # written back into this report's "triage" field.
        report["failures"].append({"wave": wave, "line": f, "triage": "UNTRIAGED"})
json.dump(report, open(f"{outdir}/report.json", "w"), indent=2)
print(f"wrote {outdir}/report.json: verdict={report['verdict']}, "
      f"failures={len(report['failures'])}")
EOF

if [ "$overall" -ne 0 ]; then
  echo "GATE RED: failures above (triage: $OUTDIR/report.json)"
  exit 1
fi
echo "GATE GREEN: all three wave corpora differential-clean vs gc 1.27.1"
