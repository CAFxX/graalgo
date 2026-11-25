#!/usr/bin/env python3
"""Double-fork launcher for run_matrix.sh: reparents the runner to init so
it survives the spawning exec session being reaped."""
import os
import sys

SPIKE = os.path.expanduser("~/workspace/graalgo/spikes/05-cmdgo")

os.chdir(SPIKE)
pid = os.fork()
if pid != 0:
    # Parent: exit immediately; child continues.
    os._exit(0)

# Child: new session, then fork again so the grandchild is reparented to init.
os.setsid()
pid2 = os.fork()
if pid2 != 0:
    os._exit(0)

# Grandchild: fully detached. Redirect stdio and exec the runner.
os.chdir(SPIKE)
log = open(os.path.join(SPIKE, "logs", "runner.out"), "ab", buffering=0)
os.dup2(log.fileno(), 1)
os.dup2(log.fileno(), 2)
devnull = os.open(os.devnull, os.O_RDONLY)
os.dup2(devnull, 0)
os.execv(os.path.join(SPIKE, "run_matrix.sh"), ["run_matrix.sh"])
