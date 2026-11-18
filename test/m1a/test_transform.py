#!/usr/bin/env python3
"""Tests of test/m1a/transform.py (run by `make test-m1a`): the println probe
goes into main only — before main's bare returns and its closing brace — even
when helper functions follow main or have bare returns of their own."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import transform  # noqa: E402

SRC = '''package main

func helper() {
\tif true {
\t\treturn
\t}
}

func main() {
\tx := 1
\tif x > 0 {
\t\treturn
\t}
\tx++
}

func after() {
\treturn
}

// observe: x
'''


def main():
    out = transform.transform(SRC).split('\n')
    probes = [i for i, l in enumerate(out) if l.strip() == 'println(x)']
    main_at = out.index('func main() {')
    after_at = out.index('func after() {')
    assert len(probes) == 2, out
    assert all(main_at < i < after_at for i in probes), out
    # before main's bare return and immediately before main's closing brace
    assert out[probes[0] + 1].strip() == 'return', out
    assert out[probes[1] + 1] == '}', out
    # unchanged without the trailer
    assert transform.transform('package main\n\nfunc main() {\n}\n') == 'package main\n\nfunc main() {\n}\n'
    try:
        transform.transform('package main\n\nfunc f() {\n}\n\n// observe: x\n')
    except SystemExit:
        pass
    else:
        raise AssertionError('no main: must fail loudly')
    print('test_transform: ok')


if __name__ == '__main__':
    main()
