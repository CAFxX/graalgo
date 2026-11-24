#!/usr/bin/env python3
"""Self-test of test/lib/jvmcds.sh (run by `make check`): the archive is
keyed on the classes (an edited class is a new key, never a stale jar),
made once per key, and every failure degrades to no CDS. A fake `jar` and a
fake toolexec build stand in for the JDK and graalgo-tool."""
import os
import subprocess
import tempfile
import time
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))

FAKE_JAR = """#!/bin/bash
# jar --create --file F -C DIR .
while [ $# -gt 0 ]; do [ "$1" = --file ] && out="$2"; shift; done
echo jar >> "$COUNT"; echo jar > "$out"
"""

DRIVER = """
set -uo pipefail
. "$LIB/jvmcds.sh"
toolexec_build() {  # the fake program dumps the archive ArchiveClassesAtExit names
    printf '#!/bin/bash\\nf=$(printf "%%s" "$GRAALGO_JAVA_OPTS" | sed -n "s/.*ArchiveClassesAtExit=\\\\([^ ]*\\\\).*/\\\\1/p")\\n[ -n "$f" ] && [ -z "$FAIL_DUMP" ] && echo jsa > "$f"\\nexit 0\\n' > "$1"
    chmod +x "$1"
}
GRAALGO_JAVA_OPTS="-Xmx1g"
jvm_cds_setup ${KIND:-bundle} /runner
echo "classes=${GRAALGO_TRUFFLE_CLASSES:-none}"
echo "interop=${GRAALGO_INTEROP_CLASSES:-none}"
echo "opts=$GRAALGO_JAVA_OPTS"
"""


class JvmCdsTest(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.mkdtemp()
        self.root = os.path.join(self.d, "repo")
        self.classes = os.path.join(self.root, "internal/backend/truffle/out/classes")
        os.makedirs(self.classes)
        os.makedirs(os.path.join(self.root, "internal/backend/truffle/lib"))
        self.klass = os.path.join(self.classes, "A.class")
        with open(self.klass, "w") as f:
            f.write("v1")
        self.jdk = os.path.join(self.d, "jdk")
        os.makedirs(os.path.join(self.jdk, "bin"))
        with open(os.path.join(self.jdk, "bin/jar"), "w") as f:
            f.write(FAKE_JAR)
        os.chmod(os.path.join(self.jdk, "bin/jar"), 0o755)
        self.count = os.path.join(self.d, "count")
        os.makedirs(os.path.join(self.root, "internal/backend/interop/out/classes"))
        sync = os.path.join(self.root, "internal/backend/graalgo-sync-run")
        with open(sync, "w") as f:  # dumps the archive like the fake program
            f.write('#!/bin/bash\nf=$(printf "%s" "$GRAALGO_JAVA_OPTS" | '
                    'sed -n "s/.*ArchiveClassesAtExit=\\([^ ]*\\).*/\\1/p")\n'
                    '[ -n "$f" ] && [ -n "$GRAALGO_INTEROP_CLASSES" ] && echo jsa > "$f"\nexit 0\n')
        os.chmod(sync, 0o755)

    def tearDown(self):
        subprocess.call(["rm", "-rf", self.d])

    def setup_cds(self, **env):
        e = dict(os.environ, LIB=HERE, GRAALGO_ROOT=self.root, GRAALVM_HOME=self.jdk,
                 COUNT=self.count, FAIL_DUMP="")
        e.pop("GRAALGO_CDS", None)
        e.update(env)
        out = subprocess.run(["bash", "-c", DRIVER], env=e, capture_output=True, text=True)
        self.assertEqual(out.returncode, 0, out.stderr)
        lines = dict(l.split("=", 1) for l in out.stdout.splitlines())
        self.interop = lines["interop"]
        return lines["classes"], lines["opts"]

    def jars(self):
        if not os.path.exists(self.count):
            return 0
        with open(self.count) as f:
            return len(f.read().split())

    def test_made_once_and_used(self):
        classes, opts = self.setup_cds()
        self.assertTrue(classes.endswith("/truffle.jar") and os.path.exists(classes), classes)
        self.assertIn("-Xmx1g -XX:SharedArchiveFile=", opts)
        self.assertIn("-Xlog:cds=off", opts)  # CDS notices would land on stdout
        self.assertEqual(self.setup_cds(), (classes, opts))
        self.assertEqual(self.jars(), 1)

    def test_edited_class_is_a_new_key(self):
        classes, _ = self.setup_cds()
        time.sleep(0.01)
        with open(self.klass, "w") as f:
            f.write("v2 longer")
        classes2, _ = self.setup_cds()
        self.assertNotEqual(classes, classes2)
        self.assertEqual(self.jars(), 2)

    def test_sync_kind(self):
        classes, opts = self.setup_cds(KIND="sync")
        self.assertTrue(self.interop.endswith("/interop.jar") and os.path.exists(self.interop))
        self.assertIn("/sync-", classes)
        self.assertIn("sync.jsa", opts)
        b_classes, _ = self.setup_cds()
        self.assertNotEqual(os.path.dirname(b_classes), os.path.dirname(classes))  # kinds never share
        env = dict(os.environ, GRAALGO_ROOT=self.root, GRAALVM_HOME=self.jdk, COUNT=self.count)
        out = subprocess.run(["bash", os.path.join(HERE, "jvmcds.sh"), "sync-env"], env=env,
                             capture_output=True, text=True)
        self.assertIn("export GRAALGO_INTEROP_CLASSES=", out.stdout)
        self.assertIn("export GRAALGO_JAVA_OPTS=", out.stdout)

    def test_failures_mean_no_cds(self):
        self.assertEqual(self.setup_cds(GRAALGO_CDS="off"), ("none", "-Xmx1g"))
        self.assertEqual(self.setup_cds(FAIL_DUMP="1"), ("none", "-Xmx1g"))
        self.assertEqual(os.listdir(os.path.join(self.root, ".tmp/cds")), [])  # cleaned up


if __name__ == "__main__":
    unittest.main(verbosity=1)
