#!/usr/bin/env python3
"""test/lib/gates.py — run the per-push gates timed, optionally in parallel,
and ratchet their cost against test/duration-baseline.json.

    gates.py run [-j N] [-k] [--json FILE] [--baseline FILE]
                 [--ratchet off|warn|fail] [--cache warm|cold]
                 [--format auto|plain|github] [--shard I/N] [TARGET...]
    gates.py shards N [--baseline FILE] [--cache warm|cold] [TARGET...]
    gates.py record RUN_JSON... [--baseline FILE] [--reason TEXT]

`run` with no TARGET runs the prerequisites of the Makefile's `all:` rule
(the per-push set, so the two cannot drift apart); a TARGET named `all`
stands for that list too. The SETUP targets
(generate, build, graalgo, java-test-classes) always run first, one at a
time (unless --no-setup): they write the build outputs every gate reads. Each gate then runs as
`make -o SETUP... <gate>` (SETUP marked done, never rebuilt), up to N at a
time; a gate's output is captured and printed whole when it finishes, so
parallel output never interleaves. Wall time and CPU time (user+sys of the
gate's whole process tree, from wait4) are measured per target.

Ratchet: each target's CPU time is compared with the committed baseline.
  warn  (local default) prints SLOWER when cpu > 1.2 x base + 5 s
  fail  (CI) also fails when cpu > FAIL_RATIO x base + 30 s (default 2.0):
        only a gross regression (an accidental doubling) fails; host noise
        and a slower runner stay warnings
CPU time, not wall: on a shared host wall time tracks the load, CPU time
much less. A target missing from the baseline is reported (NEW), never
failed; `record` rewrites the baseline from a run's JSON.

Like with like. A baseline file belongs to one environment (the dev host:
test/duration-baseline.json; the GitHub runner, 4 vCPU:
test/duration-baseline-ci.json — CI passes it), and inside it the numbers
of a warm-cache run ("cpu") are kept apart from those of a cold-cache run
("cpu_cold": the Go build and gc-oracle caches were not restored).
--cache says which this run is: a warm run compares with "cpu", falling
back to "cpu_cold" (a cold number is never lower than the warm one, so the
fallback can only be lax, never a false failure); a cold run compares only
with "cpu_cold" — a target with no cold number is NEW, never failed.
`record` files a run under the section of the run's --cache.

Forward-only baseline. A re-record could otherwise launder a regression
(record the slower run, and the ratchet compares with the slower number).
So a target whose "cpu"/"cpu_cold" number is NEW, or rose more than
RISE_RATIO (25%) + RISE_SLACK (1 s: sub-second targets jitter) over the
committed one ("cpu" falls back to "cpu_cold" like the ratchet), needs a
`reason` entry for that target ("reason": {target: text}) added or changed
in the same change: check_ratchets.py (make check-repo) compares each
baseline file with the git base and fails otherwise (`unreasoned_rises`).
`record --reason TEXT` writes TEXT for exactly the targets that need one
(and lists them); without it, record lists them and check-repo will fail.

--format: `github` wraps each gate's output in ::group:: (GitHub Actions
log folding), `plain` in `== target` lines; `auto` (default) picks github
iff GITHUB_ACTIONS=true.

Shards (--shard I/N, CI's parallel jobs): the gate list (after the docs
tier) is split into N disjoint parts whose union is the whole list, by
longest-processing-time-first over the baseline's wall times (a gate
without one weighs SHARD_DEFAULT_WALL): each part goes to the lightest
shard so far. Every shard runs the SETUP targets itself (a shard with no
gate runs nothing). A shard's gates are started longest first. `shards N`
prints the partition. The assignment depends only on the target list and
the baseline file, so N jobs of one commit agree on it; a gate can never
fall between shards (test_gates.py checks union and disjointness).

Exit: 0 all gates passed (and no ratchet failure), 1 a gate failed or the
ratchet failed, 2 usage.
"""
import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
import threading
import time

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
SETUP = ["generate", "build", "graalgo", "java-test-classes"]
WARN_RATIO, WARN_SLACK = 1.2, 5.0
FAIL_SLACK = 30.0
RISE_RATIO, RISE_SLACK = 1.25, 1.0
RATCHETED = ("cpu", "cpu_cold")
DEFAULT_BASELINE = os.path.join(ROOT, "test", "duration-baseline.json")
# Gates that share a scratch directory with fixed file names never run
# concurrently: the truffle module's JVM tests (test-pin-smoke, `check`'s
# module gates, the smoke tests, the fast Java classes) use truffle/out/test-tmp.
SERIAL_GROUPS = [{"check", "test-pin-smoke", "test-ci", "test-java-fast"}]
# Gates that are network-bound with ~0 CPU time (check-links-external: 21 s
# wall, 0.5 CPU s): they run beside the -j N others without taking a job
# slot, so a local -j 1 pre-land does not wait on the network.
LIGHT = {"check-links-external"}
# The weight of a gate the baseline has no wall time for (a new gate).
SHARD_DEFAULT_WALL = 30.0


# The docs tier (--tier-base): a change that touches only Markdown outside
# the code trees runs `check` (doc paths, links, GG ids, registers, req
# lint read the docs) plus the gates known to read a doc. Anything else —
# or an unknown base — runs the full list. Never on master: CI passes
# --tier-base only for branches and PRs, so the full tier stays the last
# word. A new doc reader belongs in DOC_READERS.
CODE_TREES = ("internal/", "cmd/", "stdlib/", "test/", "tools/", ".github/")
DOCS_TIER = ["check"]
DOC_READERS = {
    "docs/dev/bugs.md": ["test-toolexec"],  # intrinsics/temporary_test.go
    "docs/arch/RECOVERY.md": ["test-java-fast"],  # test/java/check_lists.py
}


def tier_targets(changed, full):
    """The targets for a change touching `changed` (paths), or `full`."""
    if not changed or any(not f.endswith(".md") or f.startswith(CODE_TREES) for f in changed):
        return full
    picked = list(DOCS_TIER)
    for f in changed:
        picked += [t for t in DOC_READERS.get(f, []) if t not in picked]
    return [t for t in full if t in picked] + [t for t in picked if t not in full]


def changed_files(base, cwd):
    """Paths changed between base and HEAD, or None when unknown."""
    try:
        out = subprocess.run(["git", "diff", "--name-only", base, "HEAD"], cwd=cwd,
                             capture_output=True, text=True, timeout=60)
    except (OSError, subprocess.TimeoutExpired):
        return None
    if out.returncode != 0:
        return None
    return [l for l in out.stdout.splitlines() if l]


def conflicts(target, running):
    return any(target in g and g & set(running) for g in SERIAL_GROUPS)


def all_targets(makefile):
    """The prerequisites of the `all:` rule, in order."""
    with open(makefile) as f:
        text = f.read().replace("\\\n", " ")
    m = re.search(r"^all:([^\n]*)$", text, re.M)
    if not m:
        raise SystemExit("gates: no `all:` rule in %s" % makefile)
    return m.group(1).split()


def load_walls(path, cache="warm"):
    """target -> wall seconds of the baseline (like load_baseline)."""
    if not path or not os.path.exists(path):
        return {}
    with open(path) as f:
        b = json.load(f)
    out = dict(b.get("wall_cold", {}))
    if cache == "warm":
        out.update(b.get("wall", {}))
    return out


def shard_targets(gates, n, walls):
    """Split `gates` into n disjoint lists covering all of them: longest
    first, each to the lightest shard (ties: the lowest index). Each list
    keeps the longest-first order."""
    weight = {t: walls.get(t, SHARD_DEFAULT_WALL) for t in gates}
    shards = [[] for _ in range(n)]
    load = [0.0] * n
    for t in sorted(gates, key=lambda t: (-weight[t], gates.index(t))):
        i = min(range(n), key=lambda i: (load[i], i))
        shards[i].append(t)
        load[i] += weight[t]
    return shards, load


def parse_shard(v):
    m = re.fullmatch(r"(\d+)/(\d+)", v or "")
    if not m or not 1 <= int(m.group(1)) <= int(m.group(2)):
        raise ValueError("--shard must be I/N with 1 <= I <= N, not %r" % v)
    return int(m.group(1)), int(m.group(2))


def parse_shard_arg(v):
    try:
        return parse_shard(v)
    except ValueError as e:
        raise argparse.ArgumentTypeError(str(e))


def run_target(target, make, cwd, setup_done, log_path):
    """Run one make target; return (rc, wall, cpu)."""
    cmd = [make, "--no-print-directory"]
    for s in setup_done:
        cmd += ["-o", s]
    cmd.append(target)
    t0 = time.monotonic()
    with open(log_path, "wb") as log:
        p = subprocess.Popen(cmd, cwd=cwd, stdout=log, stderr=subprocess.STDOUT,
                             stdin=subprocess.DEVNULL)
        _, status, ru = os.wait4(p.pid, 0)
        p.returncode = os.waitstatus_to_exitcode(status)
    wall = time.monotonic() - t0
    return p.returncode, wall, ru.ru_utime + ru.ru_stime


def verdict(cpu, base, mode, fail_ratio):
    """Ratchet verdict of one target: OK, NEW, SLOWER (warning) or FAIL."""
    if base is None:
        return "NEW"
    if mode == "fail" and cpu > fail_ratio * base + FAIL_SLACK:
        return "FAIL"
    if cpu > WARN_RATIO * base + WARN_SLACK:
        return "SLOWER"
    return "OK"


def emit(lock, text):
    with lock:
        sys.stdout.write(text)
        sys.stdout.flush()


def expand(a):
    targets = []
    for t in a.targets or ["all"]:  # "all" stands for the `all:` rule's list
        targets += all_targets(os.path.join(a.dir, "Makefile")) if t == "all" else [t]
    return targets


def cmd_shards(a):
    gates = [t for t in expand(a) if t not in SETUP]
    shards, load = shard_targets(gates, a.n, load_walls(a.baseline, a.cache))
    for i, (s, w) in enumerate(zip(shards, load), 1):
        print("shard %d/%d (~%.0f s): %s" % (i, a.n, w, " ".join(s)))
    return 0


def cmd_run(a):
    targets = expand(a)
    if a.tier_base:
        changed = changed_files(a.tier_base, a.dir)
        tiered = tier_targets(changed, targets)
        if tiered != targets:
            print("gates: docs tier: %d changed file(s) since %s are all docs: running %s"
                  % (len(changed or []), a.tier_base, " ".join(tiered)))
        else:
            print("gates: full tier (changes since %s are not docs-only, or unknown)" % a.tier_base)
        targets = tiered
    setup = [] if a.no_setup else list(SETUP)
    gates = [t for t in targets if t not in SETUP]
    if a.shard:
        i, n = a.shard
        shards, load = shard_targets(gates, n, load_walls(a.baseline, a.cache))
        gates = shards[i - 1]
        print("gates: shard %d/%d (~%.0f s by the baseline): %s" % (i, n, load[i - 1], " ".join(gates) or "-"))
        if not gates:
            setup = []
    gha = a.format == "github" or (a.format == "auto" and os.environ.get("GITHUB_ACTIONS") == "true")
    os.makedirs(a.logdir, exist_ok=True)
    logdir = tempfile.mkdtemp(prefix="gates-", dir=a.logdir)
    results = {}
    lock = threading.Lock()
    failed = []

    def one(t, done):
        log = os.path.join(logdir, t + ".log")
        rc, wall, cpu = run_target(t, a.make, a.dir, done, log)
        results[t] = {"rc": rc, "wall": round(wall, 1), "cpu": round(cpu, 1)}
        with open(log, "rb") as f:
            body = f.read().decode("utf-8", "replace")
        head = "%s %s (wall %.0fs, cpu %.0fs)" % ("PASS" if rc == 0 else "FAIL", t, wall, cpu)
        if gha:
            emit(lock, "::group::%s\n%s::endgroup::\n%s\n" % (head, body, head))
        else:
            emit(lock, "== %s\n%s== %s\n" % (t, body, head))
        if rc != 0:
            failed.append(t)

    done = []
    for t in setup:
        one(t, done)
        if failed:
            break
        done.append(t)
    if not failed:
        pending = list(gates)
        running = {}  # target -> thread
        while pending or running:
            running = {t: th for t, th in running.items() if th.is_alive()}
            stop = failed and not a.keep_going
            # a LIGHT gate (network-bound, ~0 CPU) never waits for a job
            # slot and never takes one: one at a time, beside the others
            while not stop:
                nxt = next((t for t in pending if t in LIGHT and not LIGHT & set(running)), None)
                if nxt is None:
                    heavy = [t for t in running if t not in LIGHT]
                    if len(heavy) >= a.jobs:
                        break
                    nxt = next((t for t in pending if t not in LIGHT and not conflicts(t, running)), None)
                if nxt is None:
                    break
                pending.remove(nxt)
                th = threading.Thread(target=one, args=(nxt, done))
                th.start()
                running[nxt] = th
            if stop and not running:
                break
            if running:
                next(iter(running.values())).join(0.2)
    skipped = [t for t in setup + gates if t not in results]

    base = load_baseline(a.baseline, a.cache)
    print("\ngates: %s caches; baseline %s" % (a.cache, a.baseline or "-"))
    ratchet_fail = report(results, base, a.ratchet, a.fail_ratio, skipped)
    if a.json:
        with open(a.json, "w") as f:
            json.dump({"cache": a.cache, "jobs": a.jobs, "targets": results}, f,
                      indent=1, sort_keys=True)
            f.write("\n")
    if failed:
        print("gates: FAILED: %s (logs in %s)" % (" ".join(failed), logdir))
        return 1
    for t in results:
        os.unlink(os.path.join(logdir, t + ".log"))
    os.rmdir(logdir)
    return 1 if ratchet_fail else 0


def section(cache, kind="cpu"):
    """The baseline section a run with this cache state is filed under."""
    return kind if cache == "warm" else kind + "_cold"


def load_baseline(path, cache="warm"):
    """target -> (cpu seconds, from_cold) to compare a run of this cache
    state with: like with like (module docstring)."""
    if not path or not os.path.exists(path):
        return {}
    with open(path) as f:
        b = json.load(f)
    out = {t: (v, True) for t, v in b.get("cpu_cold", {}).items()}
    if cache == "warm":
        out.update({t: (v, False) for t, v in b.get("cpu", {}).items()})
    return out


def report(results, base, mode, fail_ratio, skipped):
    """Print the timing table; return True on a ratchet failure."""
    print("\n%-22s %4s %8s %8s %8s  %s" % ("target", "rc", "wall s", "cpu s", "base s", "ratchet"))
    tw = tc = 0.0
    bad = False
    for t, r in results.items():
        b, cold = base.get(t, (None, False))
        v = verdict(r["cpu"], b, mode, fail_ratio) if mode != "off" else "-"
        bad |= v == "FAIL"
        tw += r["wall"]
        tc += r["cpu"]
        print("%-22s %4d %8.1f %8.1f %8s  %s" % (t, r["rc"], r["wall"], r["cpu"],
                                                 "-" if b is None else "%.1f%s" % (b, "c" if cold else ""), v))
    print("%-22s %4s %8.1f %8.1f   (sum of targets; parallel wall is less; base `c`: a cold-cache number)"
          % ("total", "", tw, tc))
    if skipped:
        print("not run (an earlier failure): %s" % " ".join(skipped))
    if bad:
        print("gates: RATCHET FAILED: a target's CPU time exceeds %.1fx its baseline + %.0fs "
              "(re-record with `gates.py record` only for a reviewed, intended cost increase; "
              "docs/dev/test-profile.md)" % (fail_ratio, FAIL_SLACK))
    return bad


def unreasoned_rises(was, now):
    """[(section, target, old, new)] of baseline `now` (a parsed baseline
    file) whose cpu/cpu_cold number is new or rose past RISE_RATIO x old +
    RISE_SLACK against baseline `was`, without a reason that `now` added or
    changed (module docstring: forward-only baseline)."""
    out = []
    reasons, old_reasons = now.get("reason", {}), was.get("reason", {})
    for sec in RATCHETED:
        for t, v in sorted(now.get(sec, {}).items()):
            old = was.get(sec, {}).get(t)
            if old is None and sec == "cpu":
                old = was.get("cpu_cold", {}).get(t)
            if old is not None and v <= old * RISE_RATIO + RISE_SLACK:
                continue
            if reasons.get(t) and reasons[t] != old_reasons.get(t):
                continue
            out.append((sec, t, old, v))
    return out


def cmd_record(a):
    prev = {}
    if os.path.exists(a.baseline):
        with open(a.baseline) as f:
            prev = json.load(f)
    # several runs (CI's shards, each also running the SETUP targets):
    # the max per target and section, so a normal run does not flap
    got = {}
    for path in a.run_json:
        with open(path) as f:
            run = json.load(f)
        cache = run.get("cache", "warm")
        for t, r in run["targets"].items():
            if r["rc"] != 0:
                continue
            for kind in ("cpu", "wall"):
                sec = got.setdefault(section(cache, kind), {})
                sec[t] = max(sec.get(t, r[kind]), r[kind])
    new = json.loads(json.dumps(prev))
    for sec, vals in got.items():
        new.setdefault(sec, {}).update(vals)
    cpu = {t for sec, vals in got.items() if sec.startswith("cpu") for t in vals}
    rises = unreasoned_rises(prev, new)
    if rises and a.reason:
        new.setdefault("reason", {}).update({t: a.reason for _, t, _, _ in rises})
    with open(a.baseline, "w") as f:
        json.dump(new, f, indent=1, sort_keys=True)
        f.write("\n")
    print("gates: recorded %d targets (%s) from %d run(s) into %s"
          % (len(cpu), " ".join(sorted(got)) or "-", len(a.run_json), a.baseline))
    for sec, t, o, v in rises:
        print("gates: %s %s: %s -> %.1f s %s" % (sec, t, "NEW" if o is None else "%.1f" % o, v,
                                                "(reason recorded)" if a.reason else "NEEDS A REASON"))
    if rises and not a.reason:
        print("gates: a new or >25%% higher baseline needs a reason (re-run with --reason TEXT); "
              "make check-repo fails without one")
    return 0


def main(argv=None):
    reconfigure = getattr(sys.stdout, "reconfigure", None)
    if reconfigure:
        reconfigure(line_buffering=True)  # in order with the gates' output
    ap = argparse.ArgumentParser(description=(__doc__ or "").split("\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)
    r = sub.add_parser("run")
    r.add_argument("-j", "--jobs", type=int, default=int(os.environ.get("GATE_JOBS", "1") or 1))
    r.add_argument("-k", "--keep-going", action="store_true")
    r.add_argument("--json")
    r.add_argument("--baseline", default=DEFAULT_BASELINE)
    r.add_argument("--ratchet", choices=["off", "warn", "fail"], default="warn")
    r.add_argument("--fail-ratio", type=float, default=2.0)
    r.add_argument("--cache", choices=["warm", "cold"], default=os.environ.get("GATE_CACHE") or "warm",
                   help="whether the build/oracle caches were warm: picks the baseline section")
    r.add_argument("--format", choices=["auto", "plain", "github"], default="auto",
                   help="per-gate output framing (auto: github iff GITHUB_ACTIONS=true)")
    r.add_argument("--make", default=os.environ.get("MAKE", "make"))
    r.add_argument("--dir", default=ROOT)
    r.add_argument("--logdir", default=os.path.join(ROOT, ".tmp"))
    r.add_argument("--tier-base", default=os.environ.get("GATE_TIER_BASE") or None,
                   help="pick the docs tier when every change since this commit is a doc")
    r.add_argument("--no-setup", action="store_true",
                   help="do not run the SETUP targets first (a gate builds its own prerequisites)")
    r.add_argument("--shard", type=parse_shard_arg, default=os.environ.get("GATE_SHARD") or None,
                   help="I/N: run only shard I of N (CI's parallel jobs; see the module doc)")
    r.add_argument("targets", nargs="*")
    sh = sub.add_parser("shards")
    sh.add_argument("n", type=int)
    sh.add_argument("--baseline", default=DEFAULT_BASELINE)
    sh.add_argument("--cache", choices=["warm", "cold"], default=os.environ.get("GATE_CACHE") or "warm")
    sh.add_argument("--dir", default=ROOT)
    sh.add_argument("targets", nargs="*")
    rec = sub.add_parser("record")
    rec.add_argument("run_json", nargs="+")
    rec.add_argument("--baseline", default=DEFAULT_BASELINE)
    rec.add_argument("--reason", help="why a new or >25%% higher number is intended "
                     "(written for exactly the targets that need one)")
    a = ap.parse_args(argv)
    if a.cmd == "run":
        if a.jobs < 1:
            ap.error("-j must be >= 1")
        if a.cache not in ("warm", "cold"):
            ap.error("--cache / GATE_CACHE must be warm or cold, not %r" % a.cache)
        if isinstance(a.shard, str):  # from GATE_SHARD: argparse types only the flag
            try:
                a.shard = parse_shard(a.shard)
            except ValueError as e:
                ap.error(str(e))
        return cmd_run(a)
    if a.cmd == "shards":
        if a.n < 1:
            ap.error("N must be >= 1")
        return cmd_shards(a)
    return cmd_record(a)


if __name__ == "__main__":
    sys.exit(main())
