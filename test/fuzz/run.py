#!/usr/bin/env python3
"""test/fuzz/run.py — differential fuzz runner of the -toolexec path
(`make fuzz-newpath`).

Every program is built twice from the same module: by gc (the oracle,
$GO) and by the stock `go build -toolexec graalgo-tool` with graalgo's
standard-library fork (test/lib/toolexec.sh, via test/fuzz/setup.sh), whose
executable runs on the node package (internal/backend/graalgo-bundle-run).
Both legs run with GODEBUG=cpu.all=off (docs/arch/limitations.md). Compared:
stdout (bytes), stderr (normalized as test/m1a/run.sh does) and the exit
status. Each program is classified:

  match         same observable behavior
  diverge       stdout, stderr or exit status differ (kind in the signature)
  gg-crash      the graalgo leg died of a JVM/Truffle-level error (a Java
                exception or an internal error on stderr)
  gg-build      graalgo-tool rejected a program gc builds
  timeout       the graalgo leg timed out where gc finished
  bad-program   gc rejects the program or itself times out (a generator or
                corpus bug, never a graalgo finding)

Programs: hand-crafted ones (a `package main` .go file, or a directory with
a go.mod), and/or N programs of test/fuzz/gen.py (--gen N --seed S).

Known divergences: test/fuzz/known.txt lists `<program> <BUG-id> <class>
<signature>` lines; a listed program that diverges with that class and
signature is KNOWN (not a failure), one that diverges otherwise is DRIFT,
one that matches is STALE (move it to a gate). Exit status: 0 when every
outcome is match/KNOWN, 1 on a NEW/DRIFT/STALE outcome, 3 on infra failure.

--minimize OUT_DIR delta-debugs every new failing program (line ddmin with
balanced-brace block removal) down to a program that still fails with the
same class and signature key, written to OUT_DIR.

Usage:
  run.py [--gen N --seed S] [--corpus] [--minimize DIR] [--report FILE]
         [--keep DIR] [-v] [PROGRAM.go | MODULE_DIR]...
Env: GO, GRAALGO_JAVA_OPTS, FUZZ_TIMEOUT (seconds, default 60),
     GRAALGO_TMP (scratch root, default <repo>/.tmp).
"""
import argparse
import difflib
import importlib.util
import io
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, '..', '..'))
INFRA_EXIT = 3

JAVA_CRASH = re.compile(
    r'(Exception in thread|^\s+at (graalgo|com\.oracle|java|jdk|org\.graalvm)\.|'
    r'java\.lang\.\w+(Error|Exception)|graalgo internal error|'
    r'INTERNAL ERROR|fatal internal error|^# A fatal error has been detected by the Java Runtime)',
    re.M)
INFRA = re.compile(
    r'Could not find or load main class|ClassNotFoundException|'
    r'NoClassDefFoundError|Could not create the Java Virtual Machine|'
    r'Error occurred during initialization of VM|OutOfMemoryError: unable to create native thread')


class Toolchain:
    def __init__(self, scratch):
        self.scratch = scratch
        out = subprocess.run([os.path.join(HERE, 'setup.sh'), os.path.join(scratch, 'tc')],
                             capture_output=True, text=True)
        if out.returncode != 0:
            sys.stderr.write(out.stderr)
            print('INFRA: test/fuzz/setup.sh failed', file=sys.stderr)
            sys.exit(INFRA_EXIT)
        self.env = dict(line.split('=', 1) for line in out.stdout.splitlines() if '=' in line)
        self.go = self.env['GO']
        if not os.path.exists(os.path.join(ROOT, 'internal/backend/truffle/out/classes/graalgo/launcher/BundleMain.class')):
            print('INFRA: backend not built — run `make build`', file=sys.stderr)
            sys.exit(INFRA_EXIT)
        base = dict(os.environ)
        base.update(GOFLAGS='', CGO_ENABLED='0', GOTOOLCHAIN='local',
                    GOTMPDIR=self.env['GOTMPDIR'])
        base['PATH'] = os.path.dirname(self.go) + os.pathsep + base.get('PATH', '')
        self.build_env = base
        self.gg_build_env = dict(base, GRAALGO_STUB=self.env['TOOLEXEC_STUB'],
                                 GRAALGO_RUNNER=self.env['RUNNER'])
        run_env = dict(base, GODEBUG='cpu.all=off', TMPDIR=scratch)
        run_env.setdefault('GRAALGO_JAVA_OPTS',
                           '-Xmx2g -XX:ActiveProcessorCount=4 -XX:ErrorFile=%s/hs_err_pid%%p.log' % scratch)
        self.run_env = run_env
        self.timeout = int(os.environ.get('FUZZ_TIMEOUT', '60'))

    def normalize(self, text, gg):
        if gg:
            text = '\n'.join(l for l in text.split('\n')
                             if not l.startswith('WARNING: ') and not l.startswith('NOTE: Picked up'))
        r = subprocess.run([sys.executable, self.env['NORMALIZE']], input=text,
                           capture_output=True, text=True)
        return r.stdout

    def _run(self, exe, gg):
        cmd = ['nice', '-n', '19', exe] if gg else [exe]
        try:
            p = subprocess.run(cmd, cwd=os.path.dirname(exe), env=self.run_env,
                               stdin=subprocess.DEVNULL, capture_output=True,
                               timeout=self.timeout)
            return p.returncode, p.stdout, p.stderr.decode('utf-8', 'replace')
        except subprocess.TimeoutExpired as e:
            return 'timeout', e.stdout or b'', (e.stderr or b'').decode('utf-8', 'replace')

    def evaluate(self, moddir):
        """Build and run moddir under both legs -> (class, signature, detail)."""
        gc_bin = os.path.join(moddir, 'gc.bin')
        gg_bin = os.path.join(moddir, 'gg.bin')
        b = subprocess.run([self.go, 'build', '-o', gc_bin, '.'], cwd=moddir,
                           env=self.build_env, capture_output=True, text=True)
        if b.returncode != 0:
            return 'bad-program', 'gc build', first_line(b.stderr)
        b = subprocess.run(['nice', '-n', '10', self.go, 'build', '-overlay',
                            self.env['TOOLEXEC_OVERLAY'], '-toolexec',
                            self.env['TOOLEXEC_TOOL'], '-o', gg_bin, '.'],
                           cwd=moddir, env=self.gg_build_env, capture_output=True, text=True)
        if b.returncode != 0:
            msg = [l for l in b.stderr.splitlines() if not l.startswith('#')]
            return 'gg-build', sig_text(msg[0] if msg else 'no output'), b.stderr[:4000]
        gc_rc, gc_out, gc_err = self._run(gc_bin, False)
        if gc_rc == 'timeout':
            return 'bad-program', 'gc timeout', ''
        gg_rc, gg_out, gg_err = self._run(gg_bin, True)
        if gg_rc != 'timeout' and gg_rc != 0 and INFRA.search(gg_err):
            print('INFRA: graalgo leg failed to start:\n' + gg_err[:2000], file=sys.stderr)
            sys.exit(INFRA_EXIT)
        detail = {'gc_rc': gc_rc, 'gg_rc': gg_rc, 'gc_out': gc_out, 'gg_out': gg_out,
                  'gc_err': gc_err, 'gg_err': gg_err}
        if gg_rc == 'timeout':
            return 'timeout', 'timeout', detail
        m = JAVA_CRASH.search(gg_err)
        gc_nerr = self.normalize(gc_err, False)
        gg_nerr = self.normalize(gg_err, True)
        same = gc_rc == gg_rc and gc_out == gg_out and gc_nerr == gg_nerr
        if same:
            return 'match', '', detail
        if m:
            fie = re.search(r'fatal internal error[^:]*: (.*)', gg_err)
            if fie:
                return 'gg-crash', sig_text(fie.group(1)), detail
            exc = re.search(r'((?:java|com|org|graalgo)\.[\w.$]+(?:Error|Exception))(?::\s*([^\n]*))?', gg_err)
            return 'gg-crash', sig_text(exc.group(0) if exc else m.group(0)), detail
        if gc_rc != gg_rc:
            return 'diverge', 'exit %s vs gc %s' % (gg_rc, gc_rc), detail
        if gc_out != gg_out:
            return 'diverge', 'stdout: ' + first_diff(gc_out.decode('utf-8', 'replace'),
                                                     gg_out.decode('utf-8', 'replace')), detail
        return 'diverge', 'stderr: ' + first_diff(gc_nerr, gg_nerr), detail


def load_sibling(name):
    """Imports test/fuzz/<name>.py by path (test/fuzz is no package)."""
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, name + '.py'))
    if spec is None or spec.loader is None:
        raise ImportError(name)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def first_line(s):
    for l in s.splitlines():
        if l.strip() and not l.startswith('#'):
            return l.strip()[:200]
    return ''


def sig_text(s):
    """A signature: digits and hex collapsed, so runs of one cause compare equal."""
    s = re.sub(r'0x[0-9a-fA-F]+', 'X', s.strip())
    s = re.sub(r'\d+', 'N', s)
    return s[:160]


def first_diff(a, b):
    al, bl = a.split('\n'), b.split('\n')
    for i in range(max(len(al), len(bl))):
        x = al[i] if i < len(al) else '<EOF>'
        y = bl[i] if i < len(bl) else '<EOF>'
        if x != y:
            return ('gc=%r gg=%r' % (x[:70], y[:70]))[:170]
    return 'same'


def sig_key(cls, sig):
    """What minimization preserves: the class and the kind of divergence."""
    if cls == 'diverge':
        return cls, sig.split(':')[0].split(' ')[0]
    if cls in ('gg-crash', 'gg-build'):
        return cls, re.sub(r':.*', '', sig)[:80]
    return cls, ''


def materialize(src, dst):
    os.makedirs(dst, exist_ok=True)
    if os.path.isdir(src):
        shutil.copytree(src, dst, dirs_exist_ok=True)
    else:
        shutil.copy(src, os.path.join(dst, 'main.go'))
        with open(os.path.join(dst, 'go.mod'), 'w') as f:
            f.write('module fuzzprog\n\ngo 1.27\n')


# --- minimization -----------------------------------------------------------

def blocks(lines):
    """Spans [i, j] of brace-balanced blocks: a line ending in '{' through its
    matching closing line (largest first)."""
    spans, stack = [], []
    for i, l in enumerate(lines):
        s = l.rstrip()
        opens, closes = s.count('{'), s.count('}')
        if s.endswith('{'):
            stack.append(i)
            if closes:
                pass
        elif closes > opens and stack:
            spans.append((stack.pop(), i))
    spans.sort(key=lambda p: p[0] - p[1])
    return spans


def minimize(tc, src_text, key, workdir, log):
    lines = src_text.split('\n')
    tries = [0]

    def test(cand):
        tries[0] += 1
        d = os.path.join(workdir, 'm%d' % tries[0])
        os.makedirs(d)
        with open(os.path.join(d, 'main.go'), 'w') as f:
            f.write('\n'.join(cand))
        with open(os.path.join(d, 'go.mod'), 'w') as f:
            f.write('module fuzzprog\n\ngo 1.27\n')
        cls, sig, _ = tc.evaluate(d)
        shutil.rmtree(d, ignore_errors=True)
        return sig_key(cls, sig) == key

    # Pass 1: drop whole blocks, largest first, until no block can go.
    changed = True
    while changed:
        changed = False
        for (i, j) in blocks(lines):
            cand = lines[:i] + lines[j + 1:]
            if test(cand):
                lines, changed = cand, True
                log('  block %d-%d removed (%d lines left)' % (i, j, len(lines)))
                break
    # Pass 2: line ddmin.
    n = 2
    while len(lines) >= 2:
        chunk = max(1, len(lines) // n)
        reduced = False
        for start in range(0, len(lines), chunk):
            cand = lines[:start] + lines[start + chunk:]
            if cand and test(cand):
                lines, reduced = cand, True
                n = max(n - 1, 2)
                break
        if not reduced:
            if chunk == 1:
                break
            n = min(len(lines), n * 2)
    log('  minimized to %d lines in %d tries' % (len(lines), tries[0]))
    return '\n'.join(lines)


# --- known divergences --------------------------------------------------------

def load_known(path):
    known = {}
    if not os.path.exists(path):
        return known
    with open(path) as f:
        for n, line in enumerate(f, 1):
            line = line.strip()
            if not line or line.startswith('#'):
                continue
            parts = line.split(None, 3)
            if len(parts) != 4 or not re.match(r'BUG-\d{4}$', parts[1]):
                print('known.txt:%d: want `<program> <BUG-NNNN> <class> <signature>`' % n,
                      file=sys.stderr)
                sys.exit(2)
            known[parts[0]] = (parts[1], parts[2], parts[3])
    with open(os.path.join(ROOT, 'docs', 'dev', 'bugs.md')) as f:
        ids = set(re.findall(r'^\| (BUG-\d{4}) \|', f.read(), re.M))
    missing = sorted({v[0] for v in known.values()} - ids)
    if missing:
        print('known.txt: no row in docs/dev/bugs.md for %s' % ', '.join(missing), file=sys.stderr)
        sys.exit(2)
    return known


def main():
    if isinstance(sys.stdout, io.TextIOWrapper):
        sys.stdout.reconfigure(line_buffering=True)
    ap = argparse.ArgumentParser(description=(__doc__ or '').split('\n')[0])
    ap.add_argument('programs', nargs='*')
    ap.add_argument('--gen', type=int, default=0, help='generate N programs')
    ap.add_argument('--seed', type=int, default=1)
    ap.add_argument('--corpus', action='store_true',
                    help='run test/fuzz/corpus/**/*.go and test/fuzz/repro/*.go')
    ap.add_argument('--minimize', metavar='DIR')
    ap.add_argument('--report', metavar='FILE')
    ap.add_argument('--keep', metavar='DIR', help='copy every failing program here')
    ap.add_argument('-v', action='store_true')
    args = ap.parse_args()

    root = os.environ.get('GRAALGO_TMP') or os.path.join(ROOT, '.tmp')
    os.makedirs(root, exist_ok=True)
    scratch = tempfile.mkdtemp(prefix='fuzz.', dir=root)
    try:
        return run(args, scratch)
    finally:
        shutil.rmtree(scratch, ignore_errors=True)


def run(args, scratch):
    tc = Toolchain(scratch)
    progs = []  # (name, source path)
    if args.corpus:
        for sub in ('corpus', 'repro'):
            base = os.path.join(HERE, sub)
            for dp, _, fs in sorted(os.walk(base)):
                for f in sorted(fs):
                    if f.endswith('.go'):
                        p = os.path.join(dp, f)
                        progs.append((os.path.relpath(p, ROOT), p))
    for p in args.programs:
        progs.append((os.path.relpath(os.path.abspath(p), ROOT), os.path.abspath(p)))
    if args.gen:
        gdir = os.path.join(scratch, 'gen')
        os.makedirs(gdir)
        gen = load_sibling('gen')
        for i in range(args.gen):
            seed = args.seed + i
            p = os.path.join(gdir, 'gen_%d.go' % seed)
            with open(p, 'w') as f:
                f.write(gen.generate(seed))
            progs.append(('gen:%d' % seed, p))
    if not progs:
        print('fuzz: no program (zero cases is not a pass)', file=sys.stderr)
        return INFRA_EXIT

    known = load_known(os.path.join(HERE, 'known.txt'))
    counts, outcomes = {}, []
    report = open(args.report, 'w', buffering=1) if args.report else None
    bad = 0
    t0 = time.time()
    for k, (name, src) in enumerate(progs):
        d = os.path.join(scratch, 'w%d' % k)
        materialize(src, d)
        cls, sig, detail = tc.evaluate(d)
        counts[cls] = counts.get(cls, 0) + 1
        verdict = 'PASS'
        kn = known.get(name)
        if kn:
            if cls == 'match':
                verdict = 'STALE'
            elif (cls, sig) == (kn[1], kn[2]):
                verdict = 'KNOWN ' + kn[0]
            else:
                verdict = 'DRIFT ' + kn[0]
        elif cls == 'bad-program':
            verdict = 'BAD'
        elif cls != 'match':
            verdict = 'NEW'
        failing = verdict.split()[0] in ('NEW', 'DRIFT', 'STALE')
        if failing or verdict == 'BAD':
            bad += failing
            print('%s %s [%s] %s' % (verdict, name, cls, sig))
            if args.v and isinstance(detail, dict):
                print('    exit: gc %s, graalgo %s' % (detail['gc_rc'], detail['gg_rc']))
                if detail['gc_rc'] != detail['gg_rc']:
                    for l in tc.normalize(detail['gg_err'], True).strip().split('\n')[-8:]:
                        print('    graalgo tail | ' + l[:300])
                for what, x, y in (
                        ('out', detail['gc_out'].decode('utf-8', 'replace'),
                         detail['gg_out'].decode('utf-8', 'replace')),
                        ('err', tc.normalize(detail['gc_err'], False),
                         tc.normalize(detail['gg_err'], True))):
                    dl = list(difflib.unified_diff(x.split('\n'), y.split('\n'), 'gc', 'graalgo',
                                                  n=1, lineterm=''))
                    for l in dl[2:42]:
                        print('    %s | %s' % (what, l[:300]))
            elif args.v and detail:
                print('    ' + str(detail)[:1200])
        if report:
            report.write('%s\t%s\t%s\t%s\n' % (verdict, name, cls, sig))
        outcomes.append((name, cls, sig, verdict, src))
        if failing and args.keep:
            os.makedirs(args.keep, exist_ok=True)
            dst = os.path.join(args.keep, name.replace('/', '_').replace(':', '_'))
            shutil.copy(os.path.join(d, 'main.go'), dst if dst.endswith('.go') else dst + '.go')
        shutil.rmtree(d, ignore_errors=True)
    if args.minimize:
        os.makedirs(args.minimize, exist_ok=True)
        seen = set()
        for name, cls, sig, verdict, src in outcomes:
            if not verdict.startswith('NEW') or os.path.isdir(src):
                continue
            key = sig_key(cls, sig)
            if (key, sig) in seen:
                continue
            seen.add((key, sig))
            print('minimizing %s %s' % (name, key))
            with open(src) as f:
                text = f.read()
            w = os.path.join(scratch, 'min')
            os.makedirs(w, exist_ok=True)
            small = minimize(tc, text, key, w, print)
            out = os.path.join(args.minimize, name.replace('/', '_').replace(':', '_'))
            if not out.endswith('.go'):
                out += '.go'
            with open(out, 'w') as f:
                f.write(small)
    if report:
        report.close()
    total = len(progs)
    print('fuzz: %d programs in %.0fs: %s' % (total, time.time() - t0, ', '.join(
        '%s %d' % (c, counts[c]) for c in sorted(counts))))
    sigs = {}
    for name, cls, sig, verdict, _ in outcomes:
        if cls not in ('match', 'bad-program'):
            sigs.setdefault((cls, sig), []).append(name)
    if sigs:
        print('fuzz: distinct failure signatures:')
        for (cls, sig), names in sorted(sigs.items(), key=lambda kv: -len(kv[1])):
            print('  %3d  [%s] %s  e.g. %s' % (len(names), cls, sig, names[0]))
    return 1 if bad else 0


if __name__ == '__main__':
    sys.exit(main())
