#!/bin/bash
# Targeted regression for S-01 (select/channel/scheduler blast radius).
# Full SchedTestRunner suite is owed in a worktree on quota recovery (/tmp is
# 100% full; gofrontend-dependent tests need temp space). Runs the in-memory
# select/channel/scheduler tests under the $1 overlay (red|green).
set -u
S=/tmp/rev1107-22-s01
R=~/workspace/graalgo
GV=~/tools/graalvm-jdk-25/bin
JARS=$(ls $R/internal/backend/truffle/lib/*.jar | paste -sd: -)
MAIN_CP="$R/internal/backend/runtime/out/classes:$R/internal/backend/truffle/out/classes:$R/internal/backend/interop/out/classes:$JARS"
STAGED_TEST="$S/build/graalgo/internal/backend/runtime/out/test-classes"
MODE="$1"; OUT="$S/$MODE"
nice -n 10 "$GV/java" --enable-native-access=ALL-UNNAMED -Xss32m -XX:-UsePerfData \
  -cp "$S/build/runner-classes:$OUT/test-classes:$OUT/classes:$STAGED_TEST:$MAIN_CP" \
  SingleTestRunner \
  graalgo.runtime.SelectSweepPoisonTest \
  graalgo.runtime.SelectSelfClaimTest \
  graalgo.sched.ChannelTest \
  graalgo.sched.ChannelDifferentialTest \
  graalgo.sched.SchedulerCoreTest \
  graalgo.sched.IoSlotsTest \
  graalgo.runtime.TimerTest
