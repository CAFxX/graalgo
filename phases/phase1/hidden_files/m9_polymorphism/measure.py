#!/usr/bin/env python3
"""M9 call-site polymorphism measurement.

Runs every program in the three corpora under
`graalgo-run --dump-polymorphism`, aggregates the per-site identity
histograms, and counts generic instantiations per origin from the
frontend/v1 documents.

Output: report.json + SUMMARY.md in this directory.

Methodology notes (M9 review #2):
- Per-program timeout: 30s for the probed run, 60s for the frontend pass
  (disclosed here and in SUMMARY.md; programs killed by the timeout are
  counted in `programs_no_snapshot`, not silently dropped).
- Programs are bucketed as: executed (snapshot produced), rejected at build
  (frontend non-zero exit / bad JSON), frontend timeout, no snapshot
  (probed run produced none — timeout kill, crash, or launcher input error),
  panic-truncated (snapshot present but the program panicked: partial trace,
  flagged per program).
- Site names are `<funcdecl-objId>@<file>:<line>:<col>`: each monomorphized
  instantiation is a separate funcdecl, so per-instantiation sites do not
  merge (M9 review #2 P1-1).
- Deferred function-value calls are probed (M9 review #2 P1-2).
"""
import json
import os
import subprocess
import sys
from pathlib import Path

GRAALGO = Path.home() / "workspace" / "graalgo"
P1 = GRAALGO / "phases" / "phase1"
RUN = P1 / "graalgo-run"
OUT = Path(__file__).parent

# M9 review #2 P2-10: fail fast if the binary is absent.
if not RUN.exists() or not os.access(RUN, os.X_OK):
    sys.exit(f"graalgo-run not found or not executable: {RUN} (build it first)")

PROGRAMS = []
for d in sorted((GRAALGO / "tools" / "diffharness" / "corpus").iterdir()):
    m = d / "main.go"
    if m.exists():
        # M9 review #2 P2-7: include the parent dir so main.go files are distinct.
        PROGRAMS.append(("diffharness", str(m), f"diffharness:{d.name}/main.go"))
for g in sorted((P1 / "corpus").rglob("*.go")):
    PROGRAMS.append(("corpus", str(g), f"corpus:{g.relative_to(P1 / 'corpus')}"))
for g in sorted((GRAALGO / "tools" / "fuzz" / "work" / "regressions").glob("*.go")):
    PROGRAMS.append(("fuzz", str(g), f"fuzz:{g.name}"))


def run_gofrontend(prog):
    """Returns (status, doc). status: 'ok' | 'rejected' | 'timeout'."""
    gf = P1 / "frontend" / "gofrontend"
    try:
        r = subprocess.run([str(gf), prog], capture_output=True, text=True,
                           errors="replace", timeout=60)
    except subprocess.TimeoutExpired:
        return "timeout", None
    if r.returncode != 0:
        return "rejected", None
    return "ok", r.stdout


def main():
    sites = []  # (program, site, calls, targets, receiverTypes, truncated)
    insts = {}  # origin name -> set of typearg tuples
    ran = 0
    rejected = 0
    frontend_timeouts = 0
    no_snapshot = []  # (label, reason)
    truncated = []  # labels of programs whose snapshot is panic-truncated
    snap = OUT / "tmp_snap.json"

    for tag, prog, label in PROGRAMS:
        status, doc = run_gofrontend(prog)
        if status == "timeout":
            frontend_timeouts += 1
            continue
        if status == "rejected":
            rejected += 1
            continue
        try:
            d = json.loads(doc)
        except json.JSONDecodeError:
            rejected += 1
            continue
        # Static: generic instantiations per origin.
        objs = d.get("objects", [])
        names = {i: o.get("name", "?") for i, o in enumerate(objs)}
        for o in objs:
            if o.get("kind") == "inst":
                origin = names.get(o.get("instof", -1), "?")
                key = tuple(o.get("typeargs", []))
                insts.setdefault(origin, set()).add(key)
        # Dynamic: run with polymorphism probing.
        # M9 review #2 P1-4: the stale snapshot is removed BEFORE the run, and
        # removal is in a finally so a corrupt snapshot can never leak into
        # the next program's row.
        snap.unlink(missing_ok=True)
        try:
            r = subprocess.run(
                ["timeout", "30", str(RUN), "--dump-polymorphism=" + str(snap), prog],
                capture_output=True, text=True, errors="replace", timeout=60)
            rc = r.returncode
        except subprocess.TimeoutExpired:
            # The outer 60s guard fired (the inner `timeout 30` should have
            # fired first); treat as a kill.
            no_snapshot.append((label, "outer-timeout"))
            continue
        # M9 review #2 P1-3: a missing snapshot is a counted bucket, not a
        # silent skip. M9 review #2 P1-5: record the exit status; a snapshot
        # from a panicking program is a partial (truncated) trace.
        if not snap.exists():
            reason = "timeout-kill" if rc == 124 else f"exit-{rc}"
            no_snapshot.append((label, reason))
            continue
        ran += 1
        is_truncated = rc != 0
        if is_truncated:
            truncated.append((label, rc))
        try:
            s = json.loads(snap.read_text(encoding="utf-8", errors="replace"))
        except json.JSONDecodeError:
            no_snapshot.append((label, "corrupt-snapshot"))
            continue
        finally:
            snap.unlink(missing_ok=True)
        for site in s.get("sites", []):
            sites.append((label, site["site"], site["calls"],
                          site["targets"], site["receiverTypes"], is_truncated))

    # Aggregate target-cardinality histogram (full traces only for the headline;
    # truncated traces are reported separately).
    hist = {}
    hist_trunc = {}
    for _, site, calls, tgts, _, is_trunc in sites:
        h = hist_trunc if is_trunc else hist
        h[tgts] = h.get(tgts, 0) + 1
    recv_hist = {}
    for _, site, calls, tgts, recv, _ in sites:
        if recv > 0:
            recv_hist[recv] = recv_hist.get(recv, 0) + 1

    report = {
        "methodology": {
            "probed_run_timeout_s": 30,
            "frontend_timeout_s": 60,
            "site_name_format": "<funcdecl-objId>@<file>:<line>:<col>",
            "truncated_definition": "snapshot present but probed run exited non-zero",
        },
        "programs_total": len(PROGRAMS),
        "programs_rejected_at_build": rejected,
        "programs_frontend_timeout": frontend_timeouts,
        "programs_executed": ran,
        "programs_no_snapshot": [{"program": p, "reason": r} for p, r in no_snapshot],
        "programs_panic_truncated": [{"program": p, "exit": rc} for p, rc in truncated],
        "call_sites": len(sites),
        "call_sites_full_trace": sum(hist.values()),
        "call_sites_truncated_trace": sum(hist_trunc.values()),
        "target_cardinality_histogram": {str(k): v for k, v in sorted(hist.items())},
        "target_cardinality_histogram_truncated": {str(k): v for k, v in sorted(hist_trunc.items())},
        "receiver_cardinality_histogram": {str(k): v for k, v in sorted(recv_hist.items())},
        "method_sites_measured": sum(1 for s in sites if s[4] > 0),
        "generic_origins": len(insts),
        "generic_instantiations_total": sum(len(v) for v in insts.values()),
        "generic_instantiation_histogram": {},
        "sites": [
            {"program": p, "site": s, "calls": c, "targets": t,
             "receiverTypes": r, "truncated": tr}
            for p, s, c, t, r, tr in sites
        ],
    }
    ih = {}
    for origin, keys in insts.items():
        ih[len(keys)] = ih.get(len(keys), 0) + 1
    report["generic_instantiation_histogram"] = {str(k): v for k, v in sorted(ih.items())}
    report["generic_origins_detail"] = {o: len(ks) for o, ks in sorted(insts.items())}

    (OUT / "report.json").write_text(json.dumps(report, indent=2))

    n = sum(hist.values())
    mega = sum(v for k, v in hist.items() if k > 5)
    lines = [
        "# M9 polymorphism measurement — summary",
        "",
        f"Programs: {len(PROGRAMS)} total; {rejected} rejected at build "
        f"(out of WS scope); {frontend_timeouts} frontend timeouts; "
        f"{ran} executed with probing; {len(no_snapshot)} produced no snapshot "
        f"({', '.join(sorted(set(r for _, r in no_snapshot))) or 'none'}); "
        f"{len(truncated)} panic-truncated (partial traces, excluded from the "
        f"headline histogram).",
        f"Methodology: 30s per-program probed-run timeout; site names are "
        f"`<funcdecl-objId>@<file>:<line>:<col>` (per-instantiation sites do "
        f"not merge); deferred calls probed.",
        f"Call sites measured (full traces): {n} (method sites with receiver "
        f"data: {report['method_sites_measured']}).",
        "",
        "## Call-site target cardinality — full traces only",
        "  (distinct compiled targets per site)",
        "",
        "| targets | sites | % |",
        "|---|---|---|",
    ]
    for k in sorted(hist):
        v = hist[k]
        pct = 100.0 * v / n if n else 0
        label = {1: "monomorphic", 2: "bimorphic"}.get(k, "megamorphic" if k > 5 else f"{k}-morphic")
        lines.append(f"| {k} ({label}) | {v} | {pct:.1f}% |")
    lines += [
        "",
        f"Monomorphic: {hist.get(1, 0)} / {n} "
        f"({100.0 * hist.get(1, 0) / n if n else 0:.1f}%).",
        f"Megamorphic (>5 targets): {mega} / {n} "
        f"({100.0 * mega / n if n else 0:.1f}%).",
    ]
    if hist_trunc:
        lines += [
            "",
            f"Truncated traces (excluded above): "
            f"{sum(hist_trunc.values())} sites, histogram "
            f"{dict(sorted(hist_trunc.items()))}.",
        ]
    lines += [
        "",
        "## Method receiver-type cardinality",
        "",
        "In Phase 1, method calls resolve statically (`methodref`), so the",
        "receiver's static type is fixed per site: receiver cardinality is 1",
        "for every executed method site. This histogram is scaffolding for",
        "WS D (interface dispatch), not signal yet.",
        f"Method sites with receiver data: {report['method_sites_measured']}; "
        f"histogram: {report['receiver_cardinality_histogram'] or 'n/a'}.",
        "",
        "## Generic instantiations (static, from frontend/v1 documents)",
        "",
        f"Generic origins: {len(insts)}; total instantiations: "
        f"{sum(len(v) for v in insts.values())}.",
        f"Per-origin instantiation-count histogram: "
        f"{report['generic_instantiation_histogram'] or 'n/a'}.",
        "",
        "Full per-site data: `report.json`.",
    ]
    (OUT / "SUMMARY.md").write_text("\n".join(lines) + "\n")
    print("\n".join(lines))


if __name__ == "__main__":
    main()
