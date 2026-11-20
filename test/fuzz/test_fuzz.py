#!/usr/bin/env python3
"""Unit tests of the fuzz runner's pure parts (run by `make fuzz-newpath`
before the run): the generator is seeded, the minimizer keeps the failure
and drops the rest, the known-divergence register refuses an id that
docs/dev/bugs.md does not have, and signatures collapse run-specific numbers.
"""
import contextlib
import importlib.util
import io
import os
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))


def load(name):
    """Imports test/fuzz/<name>.py by path (test/fuzz is no package)."""
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, name + '.py'))
    assert spec is not None and spec.loader is not None, name
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


gen = load('gen')
run = load('run')


class FakeToolchain:
    """evaluate: 'diverge' while main.go still has the marker line and parses
    (balanced braces) — a stand-in for gc plus graalgo."""

    def __init__(self):
        self.calls = 0

    def evaluate(self, d):
        self.calls += 1
        with open(os.path.join(d, 'main.go')) as f:
            text = f.read()
        if text.count('{') != text.count('}'):
            return 'bad-program', 'gc build', ''
        if 'MARK()' in text:
            return 'diverge', 'stdout: x', {}
        return 'match', '', {}


class FuzzTest(unittest.TestCase):
    def test_generator_seeded(self):
        self.assertEqual(gen.generate(7), gen.generate(7))
        self.assertNotEqual(gen.generate(7), gen.generate(8))
        self.assertIn('package main', gen.generate(1))

    def test_minimize_keeps_failure(self):
        src = '\n'.join(['package main', '', 'func a() {', '\tx := 1', '\t_ = x', '}', '',
                         'func main() {', '\tfor i := 0; i < 3; i++ {', '\t\tprintln(i)', '\t}',
                         '\tMARK()', '\tprintln("tail")', '}', ''])
        tc = FakeToolchain()
        with tempfile.TemporaryDirectory() as w:
            out = run.minimize(tc, src, ('diverge', 'stdout'), w, lambda *_: None)
        self.assertIn('MARK()', out)
        self.assertNotIn('println(i)', out)
        self.assertNotIn('func a()', out)
        self.assertLess(len(out.split('\n')), 6)

    def test_blocks(self):
        src = ['func f() {', '\tif x {', '\t\ta()', '\t} else {', '\t\tb()', '\t}', '}', 'var y = 1']
        self.assertEqual(run.blocks(src), [(0, 6), (1, 5)])

    def test_known_requires_bug_row(self):
        with tempfile.NamedTemporaryFile('w', suffix='.txt', delete=False) as f:
            f.write('test/fuzz/x.go BUG-9999 diverge stdout: x\n')
        try:
            with self.assertRaises(SystemExit), contextlib.redirect_stderr(io.StringIO()):
                run.load_known(f.name)
        finally:
            os.unlink(f.name)

    def test_known_parses(self):
        known = run.load_known(os.path.join(run.HERE, 'known.txt'))
        for prog, (_bug, cls, _sig) in known.items():
            self.assertTrue(os.path.exists(os.path.join(run.ROOT, prog)), prog)
            self.assertIn(cls, ('diverge', 'gg-crash', 'gg-build', 'timeout'))

    def test_sig_text(self):
        self.assertEqual(run.sig_text('main.go:12:3: x 0x1f'), run.sig_text('main.go:9:1: x 0xab'))
        self.assertEqual(run.sig_key('diverge', 'stdout: gc=1 gg=2'), ('diverge', 'stdout'))


if __name__ == '__main__':
    unittest.main()
