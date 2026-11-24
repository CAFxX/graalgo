#!/usr/bin/env python3
"""Self-test of test/lib/gcoracle.sh (run by `make check`): a replayed gc
leg must be byte-identical to the live one, and every input the leg
depends on must miss the cache. A fake `go` counts its builds."""
import os
import subprocess
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))

FAKE_GO = """#!/bin/bash
# fake toolchain: `go version`, and `go build ... -o OUT .` writes a script
# that prints main.go's first line (exit status: its second line)
case "$1" in
version) echo "go version go1.99 fake"; exit 0 ;;
build) echo x >> "$GO_COUNT"; out=""; while [ $# -gt 0 ]; do [ "$1" = -o ] && out="$2"; shift; done
       grep -q REJECT main.go && { echo "main.go:1: rejected"; exit 1; }
       printf '#!/bin/bash\\necho "%s $MARK"\\necho err >&2\\nexit %s\\n' "$(head -1 main.go)" "$(sed -n 2p main.go)" > "$out"
       chmod +x "$out" ;;
esac
"""

DRIVER = """
set -uo pipefail
. "$LIB/infra.sh"
. "$LIB/gcoracle.sh"
run_leg() { local st=0; ( cd "$(dirname "$1")" && "$1" >"$2" 2>"$3" ) || st=$?; return "$st"; }
gc_live() { gc_leg_build_run "$w/mod" "$w" run_leg; }
w="$TMP/w"; rm -rf "$w"; mkdir -p "$w/mod"; printf '%s\\n%s\\n' "$LINE" "$STATUS" > "$w/mod/main.go"
gc_oracle "$w" "$w/mod" gc_live gate "$MATERIAL"
echo "status=$(cat "$w/gc.status")"; cat "$w/gc.out" "$w/gc.err" 2>/dev/null; cat "$w/gc.build" 2>/dev/null
"""


class GcOracleTest(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp()
        self.go = os.path.join(self.d, "go")
        with open(self.go, "w") as f:
            f.write(FAKE_GO)
        os.chmod(self.go, 0o755)
        self.count = os.path.join(self.d, "count")
        self.tmp = os.path.join(self.d, "scratch")
        os.mkdir(self.tmp)

    def tearDown(self):
        subprocess.call(["rm", "-rf", self.d])

    def leg(self, line="hello", status="0", material="m", mark="", cache=None):
        env = dict(os.environ, GO=self.go, GO_COUNT=self.count, LIB=HERE, TMP=self.tmp,
                   LINE=line, STATUS=status, MATERIAL=material, MARK=mark,
                   GC_ORACLE_CACHE=cache or os.path.join(self.d, "cache"))
        out = subprocess.run(["bash", "-c", DRIVER], env=env, capture_output=True, text=True)
        self.assertEqual(out.returncode, 0, out.stderr)
        return out.stdout

    def builds(self):
        if not os.path.exists(self.count):
            return 0
        with open(self.count) as f:
            return len(f.read().split())

    def test_replay_is_identical(self):
        live = self.leg(status="3")
        self.assertEqual(live, "status=3\nhello \nerr\n")
        self.assertEqual(self.leg(status="3"), live)
        self.assertEqual(self.builds(), 1)

    def test_every_input_is_in_the_key(self):
        self.leg()
        self.leg(line="changed source")
        self.leg(material="other flags")
        self.assertEqual(self.builds(), 3)
        env_go = dict(os.environ)
        try:
            os.environ["GODEBUG"] = "x=1"
            self.leg()
        finally:
            os.environ.clear()
            os.environ.update(env_go)
        self.assertEqual(self.builds(), 4)

    def test_rejected_build_is_replayed(self):
        out = self.leg(line="REJECT")
        self.assertIn("status=build", out)
        self.assertIn("rejected", out)
        self.assertEqual(self.leg(line="REJECT"), out)
        self.assertEqual(self.builds(), 1)

    def test_not_stored(self):
        self.leg(status="124")  # timed out: host speed, never stored
        self.leg(status="124")
        self.assertEqual(self.builds(), 2)
        self.leg(mark=self.tmp)  # output mentions the scratch path
        self.leg(mark=self.tmp)
        self.assertEqual(self.builds(), 4)
        self.leg(cache="off")
        self.leg(cache="off")
        self.assertEqual(self.builds(), 6)


if __name__ == "__main__":
    unittest.main(verbosity=1)
