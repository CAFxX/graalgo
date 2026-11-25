#!/usr/bin/env python3
"""Tests of tbnorm.py, the frames-keeping stderr normalizer of test/m1a."""
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import tbnorm as tb  # noqa: E402

GC = """panic: boom [recovered]
\tpanic: again

goroutine 1 [running]:
main.main.func1()
\t/tmp/x/main.go:7 +0x25
panic({0x4b1e20?, 0xc000012345?})
\t/usr/lib/go/src/runtime/panic.go:787 +0x132
main.(*T).m(0xc000010000, 0x2)
\t/tmp/x/main.go:12 +0x1d
main.inner(...)
\t/tmp/x/main.go:4
main.main()
\t/tmp/x/main.go:20 +0x3a
created by main.start in goroutine 1
\t/tmp/x/main.go:30 +0x44
exit status 2
"""

GG = """panic: boom [recovered]
\tpanic: again

goroutine 7 [running]:
main.main.func1(...)
\t/w/m/main.go:7 +0x10
main.(*T).m(...)
\t/w/m/main.go:12 +0x8
main.inner(...)
\t/w/m/main.go:4 +0x4
main.main(...)
\t/w/m/main.go:20 +0x0
exit status 2
"""


class TracebackTest(unittest.TestCase):
    def test_gc_and_graalgo_forms_agree(self):
        self.assertEqual(tb.normalize(GC), tb.normalize(GG))

    def test_lines_and_names_are_compared(self):
        self.assertNotEqual(tb.normalize(GC), tb.normalize(GG.replace('main.go:12', 'main.go:13')))
        self.assertNotEqual(tb.normalize(GC), tb.normalize(GG.replace('main.inner', 'main.outer')))
        self.assertNotEqual(tb.normalize(GC), tb.normalize(GG.replace('main.go:4 ', 'other.go:4 ')))

    def test_shape(self):
        self.assertEqual(tb.normalize(GC).splitlines()[3:6],
                         ['goroutine N [running]:', 'main.main.func1(...)', '\tmain.go:7'])

    def test_signal_pc_and_runtime_frames(self):
        out = tb.normalize('[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x46214e]\n\n'
                           'goroutine 1 [running]:\nruntime.(*Func).Entry(0x4cc462?)\n'
                           '\t/go/src/runtime/symtab.go:824 +0xe\nmain.f(0x0)\n\t/x/main.go:3 +0x1\n')
        self.assertEqual(out, '[signal SIGSEGV: segmentation violation code=0xADDR addr=0xADDR pc=0xADDR]\n\n'
                              'goroutine N [running]:\nmain.f(...)\n\tmain.go:3\n')


if __name__ == '__main__':
    unittest.main()
