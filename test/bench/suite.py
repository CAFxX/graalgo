#!/usr/bin/env python3
"""The benchmark suite runner (`make bench-suite`; docs/arch/performance.md).

Runs every program of test/bench/suite/<name>/ (harness: suite/h) under gc,
optionally gc+PGO, and graalgo (built through graalgo-tool, run on the node
package), interleaved --reps times, and checks that every run prints gc's
RESULT line. Reports per benchmark:

  gc / gc+pgo        best round and median round (ms)
  graalgo            best round, steady median (median of the last half of
                     the rounds), the warmup curve (first rounds, ms), the
                     time to peak (cumulative round time until the first
                     round within 1.10x of the steady median) and the process
                     wall time (JVM start-up and compilation included)
  ratios             graalgo/gc on best rounds and on steady medians

Medians are over --reps interleaved repetitions (gc, gc+pgo, graalgo, gc, ...)
so host-load drift hits every leg alike; the load average before and after is
recorded. --trace adds Truffle compilation tracing to the graalgo leg and
counts compilations, failures, deoptimizations and invalidations.

--dir runs the programs of another directory with the same protocol
(test/bench/realistic/, `make bench-realistic`); `STAT <name> <key> <int>`
lines a program prints are collected (median over reps). Every run also
records the peak RSS and the CPU time (user + system, JIT and GC threads
included) of the process. --gc-env sets environment variables of the gc
legs (e.g. GOMAXPROCS=4 to match the JVM's -XX:ActiveProcessorCount=4);
--gc-variant NAME=ENV and --gg-variant NAME=OPTS add further gc legs (other
environment, e.g. GOGC=400) and graalgo legs (JVM flags appended to
GRAALGO_JAVA_OPTS, e.g. a different collector).

Invoked by suite.sh, which builds graalgo-tool and the stdlib fork and exports
TOOLEXEC_TOOL, TOOLEXEC_STUB, TOOLEXEC_OVERLAY, GRAALGO_RUNNER, GO, TMP.
Exit: 0 all ok, 1 any failure (wrong result, crash, timeout).
"""
import argparse
import json
import os
import re
import statistics
import subprocess
import sys
import threading
import time

HERE = os.path.dirname(os.path.abspath(__file__))
SUITE = os.path.join(HERE, 'suite')
LINE = re.compile(r'^(ROUND|RESULT|BENCH|STAT) (\S+) (.*)$')
PEAK_WITHIN = 1.10
WARMUP_SHOWN = 10


def names_all():
    return sorted(d for d in os.listdir(SUITE)
                  if d != 'h' and os.path.isfile(os.path.join(SUITE, d, 'main.go')))


def env_spec(spec):
    """'K=V K2=V2' -> dict."""
    out = {}
    for kv in spec.split():
        k, sep, v = kv.partition('=')
        if not sep:
            sys.exit('suite.py: not K=V: ' + kv)
        out[k] = v
    return out


def variant(spec):
    name, sep, rest = spec.partition('=')
    if not sep or not name:
        sys.exit('suite.py: variant is NAME=SPEC: ' + spec)
    return name, rest


def env_get(k):
    v = os.environ.get(k)
    if not v:
        sys.exit('suite.py: %s not set (run through test/bench/suite.sh)' % k)
    return v


def go_build(out, name, extra=(), env=None):
    cmd = [env_get('GO'), 'build'] + list(extra) + ['-o', out, './' + name]
    r = subprocess.run(cmd, cwd=SUITE, env=env, capture_output=True, text=True)
    if r.returncode != 0:
        raise RuntimeError('build failed: %s\n%s' % (' '.join(cmd), r.stderr[-3000:]))


def parse(name, text):
    rounds, result, best, stats = [], None, None, {}
    for ln in text.splitlines():
        m = LINE.match(ln)
        if not m or m.group(2) != name:
            continue
        kind, rest = m.group(1), m.group(3) or ''
        if kind == 'ROUND':
            rounds.append(int(rest.split()[1]))
        elif kind == 'RESULT':
            result = rest
        elif kind == 'STAT':
            k, _, v = rest.partition(' ')
            stats[k] = int(v)
        else:
            best = int(rest)
    return rounds, result, best, stats


def run(cmd, env, timeout):
    """Runs cmd; returns (status, combined stderr+stdout, wall seconds,
    {'maxrss_mb', 'cpu_s'}). The usage is the child's: the graalgo leg's
    stub, runner script and JVM exec one another, so it is the JVM's."""
    t0 = time.monotonic()
    with open(os.devnull, 'rb') as devnull:
        p = subprocess.Popen(cmd, env=env, stdin=devnull, stdout=subprocess.PIPE,
                             stderr=subprocess.STDOUT)
    chunks = []
    reader = threading.Thread(target=lambda: chunks.append(p.stdout.read()))
    reader.start()
    status = None
    usage = None
    deadline = t0 + timeout
    while status is None:
        pid, st, ru = os.wait4(p.pid, os.WNOHANG)
        if pid:
            status, usage = os.waitstatus_to_exitcode(st), ru
            p.returncode = status
        elif time.monotonic() > deadline:
            p.kill()
            _, _, usage = os.wait4(p.pid, 0)
            status = 'timeout'
            p.returncode = -9
        else:
            time.sleep(0.02)
    reader.join()
    out = b''.join(chunks).decode('utf-8', 'replace')
    ru = {'maxrss_mb': round(usage.ru_maxrss / 1024, 1),
          'cpu_s': round(usage.ru_utime + usage.ru_stime, 3)}
    return status, out, time.monotonic() - t0, ru


def ms(ns):
    return ns / 1e6


def steady(rounds):
    """The median of the last half of the rounds (rounds is never empty)."""
    return statistics.median(rounds[len(rounds) // 2:])


def time_to_peak(rounds):
    med = steady(rounds)
    acc = 0
    for d in rounds:
        acc += d
        if d <= med * PEAK_WITHIN:
            return acc
    return acc


def main():
    global SUITE
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('names', nargs='*')
    ap.add_argument('--dir', help='the program directory (default: test/bench/suite)')
    ap.add_argument('--reps', type=int, default=1)
    ap.add_argument('--pgo', action='store_true', help='add the gc+PGO leg')
    ap.add_argument('--trace', action='store_true', help='Truffle compilation tracing counts')
    ap.add_argument('--min-total', type=int, metavar='SECONDS',
                    help='least time in rounds per leg (default 4; e.g. 20 for long-running steady state)')
    ap.add_argument('--json', help='write the results here')
    ap.add_argument('--timeout', type=int, default=int(os.environ.get('BENCH_TIMEOUT', '600')))
    ap.add_argument('--no-graalgo', action='store_true', help='gc legs only')
    ap.add_argument('--gc-env', default='', help="environment of the gc legs, 'K=V ...'")
    ap.add_argument('--gc-variant', action='append', default=[], metavar='NAME=ENV',
                    help="an extra gc leg with this environment added ('K=V ...')")
    ap.add_argument('--gg-variant', action='append', default=[], metavar='NAME=OPTS',
                    help='an extra graalgo leg with these JVM flags appended to GRAALGO_JAVA_OPTS')
    a = ap.parse_args()
    if a.dir:
        SUITE = os.path.abspath(a.dir)
    names = a.names or names_all()
    for n in names:
        if not os.path.isfile(os.path.join(SUITE, n, 'main.go')):
            sys.exit('suite.py: no such benchmark: ' + n)

    tmp = os.path.join(env_get('TMP'), 'suite')
    os.makedirs(tmp, exist_ok=True)
    base_env = dict(os.environ)
    if a.min_total:
        base_env['BENCH_MIN_TOTAL'] = str(a.min_total)  # the harness's least time in rounds (h.MinTotal)
    gc_env = dict(base_env, **env_spec(a.gc_env))
    java_opts = base_env.get('GRAALGO_JAVA_OPTS', '-Xmx2g -XX:ActiveProcessorCount=4')
    if a.trace:
        java_opts += (' -Dpolyglot.engine.TraceCompilation=true'
                      ' -Dpolyglot.engine.TraceDeoptimizeFrame=false')
    gg_env = dict(base_env, GRAALGO_JAVA_OPTS=java_opts, TMPDIR=tmp,
                  GRAALGO_STUB=env_get('TOOLEXEC_STUB'), GRAALGO_RUNNER=env_get('GRAALGO_RUNNER'))
    # legs: name -> (binary key, environment, nice)
    legs = {'gc': ('gc', gc_env, False)}
    if a.pgo:
        legs['gcpgo'] = ('gcpgo', gc_env, False)
    if not a.no_graalgo:
        legs['graalgo'] = ('graalgo', gg_env, True)
    for v in a.gc_variant:
        name, spec = variant(v)
        legs['gc:' + name] = ('gc', dict(gc_env, **env_spec(spec)), False)
    for v in a.gg_variant:
        name, opts = variant(v)
        legs['graalgo:' + name] = ('graalgo', dict(gg_env, GRAALGO_JAVA_OPTS=java_opts + ' ' + opts), True)

    load0 = os.getloadavg()
    report = {'host': {'nproc': os.cpu_count(), 'loadavg_before': load0}, 'dir': SUITE,
              'java_opts': java_opts, 'gc_env': a.gc_env, 'reps': a.reps, 'benchmarks': {}}
    hdr = '%-13s %9s %9s %9s %9s %7s %7s %9s %8s  %s' % (
        'benchmark', 'gc_ms', 'pgo_ms', 'gg_best', 'gg_med', 'best_x', 'med_x', 'ttp_ms', 'gg_wall', 'result')
    print(hdr, flush=True)
    failed = False
    for name in names:
        b = {leg: [] for leg in legs}
        verdict = 'ok'
        want = None
        try:
            bins = {'gc': os.path.join(tmp, name + '.gc')}
            go_build(bins['gc'], name)
            if a.pgo:
                prof_bin = os.path.join(tmp, name + '.prof')
                prof = os.path.join(tmp, name + '.pprof')
                go_build(prof_bin, name, ['-tags', 'benchpgo'])
                st, out, _, _ = run([prof_bin], dict(gc_env, BENCH_CPUPROFILE=prof), a.timeout)
                if st != 0:
                    raise RuntimeError('profiling run failed: %s\n%s' % (st, out[-2000:]))
                bins['gcpgo'] = os.path.join(tmp, name + '.gcpgo')
                go_build(bins['gcpgo'], name, ['-pgo=' + prof])
            if not a.no_graalgo:
                bins['graalgo'] = os.path.join(tmp, name + '.gg')
                go_build(bins['graalgo'], name,
                         ['-overlay', env_get('TOOLEXEC_OVERLAY'), '-toolexec', env_get('TOOLEXEC_TOOL')],
                         env=gg_env)
            for _ in range(a.reps):
                for leg, (key, env, niced) in legs.items():
                    cmd = [bins[key]]
                    if niced:
                        cmd = ['nice', '-n', '19'] + cmd
                    st, out, wall, ru = run(cmd, env, a.timeout)
                    rounds, result, best, stats = parse(name, out)
                    rec = {'status': st, 'rounds_ns': rounds, 'best_ns': best, 'wall_s': round(wall, 3),
                           'stats': stats}
                    rec.update(ru)
                    if a.trace and key == 'graalgo':
                        rec['trace'] = {k: len(re.findall(r'^\[engine\] opt ' + k, out, re.M))
                                        for k in ('done', 'failed', 'deopt', 'inv')}
                    b[leg].append(rec)
                    if st != 0 or best is None or not rounds:
                        verdict = 'FAIL(%s %s)' % (leg, 'exit %s' % st if st != 0 else 'no BENCH line')
                        sys.stderr.write('\n'.join('    | ' + l for l in out.splitlines()[-15:]) + '\n')
                    elif want is None:
                        want = result
                    elif result != want:
                        verdict = 'FAIL(%s result %s, gc %s)' % (leg, result, want)
        except RuntimeError as e:
            verdict = 'FAIL(build)'
            sys.stderr.write(str(e) + '\n')
        summ = {'verdict': verdict, 'result': want}
        for leg, recs in b.items():
            ok = [r for r in recs if r['status'] == 0 and r['rounds_ns'] and r['best_ns'] is not None]
            if not ok:
                continue
            s = {'best_ms': ms(min(r['best_ns'] for r in ok)),
                 'median_ms': ms(statistics.median(steady(r['rounds_ns']) for r in ok)),
                 'wall_s': statistics.median(r['wall_s'] for r in ok),
                 'maxrss_mb': statistics.median(r['maxrss_mb'] for r in ok),
                 'cpu_s': statistics.median(r['cpu_s'] for r in ok),
                 'stats': {k: statistics.median(r['stats'][k] for r in ok if k in r['stats'])
                           for k in ok[0]['stats']}}
            if legs[leg][0] == 'graalgo':
                first = ok[0]['rounds_ns']
                s['warmup_ms'] = [round(ms(d), 3) for d in first[:WARMUP_SHOWN]]
                s['time_to_peak_ms'] = ms(statistics.median(time_to_peak(r['rounds_ns']) for r in ok))
                if 'trace' in ok[0]:
                    s['trace'] = ok[0]['trace']
            summ[leg] = s
        summ['runs'] = b
        report['benchmarks'][name] = summ
        failed |= verdict != 'ok'
        gc, pgo, gg = summ.get('gc'), summ.get('gcpgo'), summ.get('graalgo')

        def f(x, fmt='%9.3f'):
            return fmt % x if x is not None else '%9s' % '-'
        print('%-13s %s %s %s %s %s %s %s %s  %s' % (
            name,
            f(gc and gc['best_ms']), f(pgo and pgo['best_ms']),
            f(gg and gg['best_ms']), f(gg and gg['median_ms']),
            f(gg and gc and gg['best_ms'] / gc['best_ms'], '%6.1fx'),
            f(gg and gc and gg['median_ms'] / gc['median_ms'], '%6.1fx'),
            f(gg and gg['time_to_peak_ms'], '%9.0f'), f(gg and gg['wall_s'], '%7.1fs'),
            verdict + ('' if not (gg and 'trace' in gg) else ' trace=%s' % gg['trace'])), flush=True)
        for leg in legs:
            s = summ.get(leg)
            if not s:
                continue
            extra = ''
            if leg not in ('gc', 'gcpgo', 'graalgo'):
                extra = ' best %.3f ms (%.2fx gc) median %.3f ms (%.2fx gc)' % (
                    s['best_ms'], s['best_ms'] / gc['best_ms'] if gc else 0,
                    s['median_ms'], s['median_ms'] / gc['median_ms'] if gc else 0)
            print('  %-20s rss %7.1f MB cpu %7.2f s%s%s' % (
                leg, s['maxrss_mb'], s['cpu_s'], extra,
                ''.join(' %s=%g' % kv for kv in sorted(s['stats'].items()))), flush=True)
    report['host']['loadavg_after'] = os.getloadavg()
    print('load average before %s after %s' % (
        ' '.join('%.1f' % x for x in load0), ' '.join('%.1f' % x for x in report['host']['loadavg_after'])))
    if a.json:
        for s in report['benchmarks'].values():
            for recs in s['runs'].values():
                for r in recs:
                    r['rounds_ns'] = r['rounds_ns'][:200]
        with open(a.json, 'w', encoding='utf-8') as fh:
            json.dump(report, fh, indent=1)
    return 1 if failed else 0


if __name__ == '__main__':
    sys.exit(main())
