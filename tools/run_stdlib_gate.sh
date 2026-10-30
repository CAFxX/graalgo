#!/usr/bin/env bash
# Layer-3 per-release gate (phases/phase3/PLAN.md §5 layer 3, GG-0029/GG-0032):
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
# and recorded in the JSON report. Until a classifier assigns a category,
# a failure is recorded with triage=null ("pending-classification") — the
# report never claims triage that has not happened. A red gate means:
# triage first, then fix or exclusion-list with a reason code (GG-0033).
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
  # Record the runner exit code for the triage report: a wave whose runner
  # failed is RED even if its log contains no FAIL lines, and a wave with
  # no summary line is UNKNOWN, never GREEN-by-absence.
  echo "$rc" > "${log%.log}.rc"
  tail -3 "$log"
  return $rc
}

overall=0
run_wave "wave1" "$GR/test/corpus/wave1/run_wave.sh" "$OUTDIR/wave1.log" || overall=1
run_wave "wave2" "$GR/test/corpus/wave2/run_wave.sh" "$OUTDIR/wave2.log" || overall=1
run_wave "wave3" "$GR/test/corpus/wave3/run_wave3.sh" "$OUTDIR/wave3.log" || overall=1

echo "== [3/3] triage report =="
python3 - "$OUTDIR" "$overall" <<'EOF' || overall=1
import json, re, sys, datetime
outdir, overall = sys.argv[1], sys.argv[2]
report = {
    "layer": 3,
    "name": "stdlib + scripts (wave corpora differential)",
    "timestamp": datetime.datetime.utcnow().isoformat() + "Z",
    "go_version": "1.27.1",
    "waves": {},
    "failures": [],
    "verdict": "GREEN",
}
for wave in ("wave1", "wave2", "wave3"):
    try:
        log = open(f"{outdir}/{wave}.log").read()
    except OSError:
        # No log = no evidence of a pass: UNKNOWN, never GREEN-by-absence.
        report["waves"][wave] = {"status": "UNKNOWN", "passed": -1,
                                 "scope_limited": -1, "failed": -1}
        report["verdict"] = "RED"
        continue
    try:
        rc = int(open(f"{outdir}/{wave}.rc").read().strip())
    except (OSError, ValueError):
        rc = -1
    m = re.search(r"== differential: (\d+) passed, (\d+) scope-limited, (\d+) failed ==", log)
    fails = [l for l in log.splitlines() if l.startswith("FAIL ")]
    if m is None:
        # The summary line is the positive evidence of a completed wave.
        # Without it, a runner that exited 0 without running anything
        # (or crashed before summarising) must not be reported GREEN.
        status = "UNKNOWN"
    elif rc != 0 or fails:
        status = "RED"
    else:
        status = "GREEN"
    report["waves"][wave] = {
        "status": status,
        "passed": int(m.group(1)) if m else -1,
        "scope_limited": int(m.group(2)) if m else -1,
        "failed": int(m.group(3)) if m else len(fails),
    }
    if status != "GREEN":
        report["verdict"] = "RED"
    for f in fails:
        # No classifier runs in this script, so no triage category is
        # claimed: triage=null until a classifier assigns one of the
        # four categories (graalgo-bug / upstream-change /
        # backend-specific / nondeterminism) and writes it back.
        report["failures"].append({"wave": wave, "line": f, "triage": None,
                                   "triage_status": "pending-classification"})
if overall != "0":
    report["verdict"] = "RED"
report["triage_complete"] = report["verdict"] == "GREEN" or all(
    f["triage"] is not None for f in report["failures"])
json.dump(report, open(f"{outdir}/report.json", "w"), indent=2)
print(f"wrote {outdir}/report.json: verdict={report['verdict']}, "
      f"failures={len(report['failures'])}")
sys.exit(0 if report["verdict"] == "GREEN" else 1)
EOF

if [ "$overall" -ne 0 ]; then
  echo "GATE RED: failures above (triage: $OUTDIR/report.json)"
  exit 1
fi
echo "GATE GREEN: all three wave corpora differential-clean vs gc 1.27.1"
