"""pe_report.py on planted traces: a failed compilation fails it, so does a root
deoptimized more than --max-churn times; a clean trace passes (run by
`make pe-check-m1a`, < 1 s)."""
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
DONE = ('[engine] opt done   engine=1  id=7     pkg.f                 |Tier 1|Time 3(2+1)ms|AST 80'
        '|Inlined 0Y 0N|IR 142/162|CodeSize 674|Addr 0x1|CompId 1|UTC 2026-11-20T20:56:52.636|Src n/a\n')
FAILED = ('[engine] opt failed engine=1  id=9     pkg.g                 |Tier 1|Time 20(20+0)ms|Reason: '
          'jdk.graal.compiler.code.SourceStackTraceBailoutException$1: Partial evaluation did not reduce value '
          'to a constant|UTC 2026-11-20T20:51:40.460|Src n/a\n')
DEOPT = '[engine] opt deopt  engine=1  id=7     pkg.f                 |   |UTC 2026-11-20T20:56:52.815|Src n/a\n'


def run(text, *extra):
    with tempfile.NamedTemporaryFile('w', suffix='.trace', delete=False) as f:
        f.write(text)
    try:
        r = subprocess.run([sys.executable, os.path.join(HERE, 'pe_report.py'), 'p', f.name] + list(extra),
                           capture_output=True, text=True)
        return r.returncode, r.stdout
    finally:
        os.unlink(f.name)


def main():
    rc, out = run(DONE * 3 + DEOPT * 2)
    assert rc == 0 and 'done=3' in out and 'deopt=2' in out, out
    rc, out = run(DONE + FAILED)
    assert rc == 1 and 'FAILED  pkg.g' in out and 'did not reduce' in out, out
    rc, out = run(DONE + DEOPT * 5, '4')
    assert rc == 1 and 'CHURN   pkg.f: 5' in out, out
    rc, out = run(DONE + DEOPT * 4, '4')
    assert rc == 0, out
    print('test_pe_report: ok')


if __name__ == '__main__':
    main()
