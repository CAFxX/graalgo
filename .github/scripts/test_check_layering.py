#!/usr/bin/env python3
"""Tests for check_layering.py: planted v1 dependencies on the new path
(direct, transitive, same-package, Go import, go.mod replace) must fail;
grandfathered edges pass; stale or uncommented allow-list lines fail;
comments, strings and graalgo.go.values do not count.

Run: python3 .github/scripts/test_check_layering.py
"""

import atexit
import contextlib
import io
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import check_layering as g  # noqa: E402

J = "internal/backend/truffle/src/main/java/"


def repo(files, allow=None):
    d = Path(tempfile.mkdtemp())
    atexit.register(shutil.rmtree, d, True)
    for rel, content in files.items():
        p = d / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(content)
    if allow is not None:
        p = d / g.ALLOWLIST
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(allow)
    return d


def java(pkg, name, body="", imports=()):
    imp = "".join(f"import {i};\n" for i in imports)
    return {f"{J}{pkg.replace('.', '/')}/{name}.java":
            f"package {pkg};\n{imp}\npublic final class {name} {{\n{body}\n}}\n"}


def run(d):
    buf = io.StringIO()
    with contextlib.redirect_stdout(buf):
        rc = g.main(["check_layering.py", str(d)])
    return rc, buf.getvalue()


class LayeringTest(unittest.TestCase):
    def test_clean_tree_passes(self):
        d = repo({**java("graalgo.go.nodes", "N", "graalgo.go.values.Slice s;"),
                  **java("graalgo.go.values", "Slice")})
        self.assertEqual(run(d)[0], 0)

    def test_direct_import_fails(self):
        d = repo(java("graalgo.go.nodes", "N", imports=["graalgo.frontend.NodeFactory"]))
        rc, out = run(d)
        self.assertEqual(rc, 1)
        self.assertIn("graalgo.go.nodes.N -> graalgo.frontend.NodeFactory", out)

    def test_transitive_through_shared_code_fails(self):
        d = repo({**java("graalgo.go.nodes", "N", "graalgo.language.GoLanguage l;"),
                  **java("graalgo.language", "GoLanguage", imports=["graalgo.values.GoMap"])})
        rc, out = run(d)
        self.assertEqual(rc, 1)
        self.assertIn("graalgo.language.GoLanguage -> graalgo.values.GoMap", out)

    def test_same_package_simple_name_is_followed(self):
        d = repo({**java("graalgo.go.nodes", "N", imports=["graalgo.runtime.A"]),
                  **java("graalgo.runtime", "A", "B b;"),
                  **java("graalgo.runtime", "B", "graalgo.p3reflect.ReflectTypes t;")})
        rc, out = run(d)
        self.assertEqual(rc, 1)
        self.assertIn("graalgo.runtime.B -> graalgo.p3reflect.ReflectTypes", out)

    def test_unreachable_v1_use_is_not_a_violation(self):
        # Shared code that the new path never reaches may use v1 freely.
        d = repo({**java("graalgo.go.nodes", "N"),
                  **java("graalgo.runtime", "Old", imports=["graalgo.values.GoMap"])})
        self.assertEqual(run(d)[0], 0)

    def test_comments_and_strings_do_not_count(self):
        body = ('/** {@link graalgo.frontend.NodeFactory} */\n'
                '// graalgo.iface.X\n'
                'String s = "graalgo.rtstack.RtStack";')
        d = repo(java("graalgo.go.nodes", "N", body))
        self.assertEqual(run(d)[0], 0)

    def test_allowlisted_edge_passes_and_stale_fails(self):
        files = java("graalgo.go.nodes", "N", imports=["graalgo.rtmemstats.RtMemStats"])
        edge = "graalgo.go.nodes.N -> graalgo.rtmemstats.RtMemStats"
        self.assertEqual(run(repo(files, f"{edge}  # owner: plan\n"))[0], 0)
        rc, out = run(repo(java("graalgo.go.nodes", "N"), f"{edge}  # owner: plan\n"))
        self.assertEqual(rc, 1)
        self.assertIn("stale entry", out)

    def test_allowlist_line_needs_a_comment(self):
        files = java("graalgo.go.nodes", "N", imports=["graalgo.rtmemstats.RtMemStats"])
        rc, out = run(repo(files, "graalgo.go.nodes.N -> graalgo.rtmemstats.RtMemStats\n"))
        self.assertEqual(rc, 1)
        self.assertIn("needs a '# owner: split plan' comment", out)

    def test_toolexec_go_import_fails(self):
        src = ('package emit\n\nimport (\n\t"fmt"\n\t"github.com/CAFxX/graalgo/internal/vet/x"\n)\n')
        rc, out = run(repo({"internal/toolexec/emit/a.go": src}))
        self.assertEqual(rc, 1)
        self.assertIn("internal/toolexec/emit/a.go -> github.com/CAFxX/graalgo/internal/vet/x", out)
        single = 'package emit\n\nimport fe "graalgo-frontend/wire"\n'
        self.assertEqual(run(repo({"internal/toolexec/b.go": single}))[0], 1)

    def test_toolexec_gomod_replace_fails(self):
        mod = "module graalgo-toolexec\n\nreplace x => ../driver\n"
        rc, out = run(repo({"internal/toolexec/go.mod": mod}))
        self.assertEqual(rc, 1)
        self.assertIn("../driver", out)

    def test_toolexec_unrelated_import_passes(self):
        src = 'package emit\n\nimport "graalgo-toolexec/ir"\n'
        self.assertEqual(run(repo({"internal/toolexec/c.go": src}))[0], 0)


if __name__ == "__main__":
    unittest.main()
