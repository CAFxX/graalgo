package graalgo.p3os;

import java.nio.charset.StandardCharsets;

import graalgo.values.GoSlice;
import graalgo.values.GoString;

/**
 * Unit tests for the {@code graalgo.os.spawn/proc*} intrinsics
 * (REVIEW-2026-11-03-16 S8): the ProcessBuilder-backed process family
 * behind synthetic os's StartProcess/FindProcess/Wait/Kill/Signal.
 *
 * <p>Each test calls {@code GoOs.init} first to reset process-wide
 * state. Expected values follow gc, probed 2026-11-03: second Wait
 * is -ECHILD, Kill after Wait is -ECHILD (Go maps it to
 * ErrProcessDone), Wait on a foreign record is -ECHILD, a missing
 * binary is -ENOENT. Stdio is /dev/null throughout (kind 0); pipe
 * bridging is covered by the sync-run e2e, which exercises it
 * through the real Go os.Pipe path.
 */
public final class GoOsProcTest {
    private static Object gs(String s) {
        return GoString.fromUtf8(s);
    }

    private static GoSlice strSlice(String... ss) {
        Object[] arr = new Object[ss.length];
        for (int i = 0; i < ss.length; i++) {
            arr[i] = GoString.fromUtf8(ss[i]);
        }
        return new GoSlice(arr, 0, arr.length, arr.length);
    }

    private static long spawn(String name, GoSlice argv, GoSlice env) {
        // Absolute dir: the Go side always passes a resolved dir.
        return GoOs.spawn(gs(name), argv, env,
                gs(System.getProperty("user.dir")),
                Long.valueOf(0), Long.valueOf(0),
                Long.valueOf(0), Long.valueOf(0),
                Long.valueOf(0), Long.valueOf(0));
    }

    private static void check(boolean cond, String msg) {
        if (!cond) {
            throw new AssertionError(msg);
        }
    }

    private static void checkEq(long want, long got, String msg) {
        if (want != got) {
            throw new AssertionError(msg + ": want " + want + ", got " + got);
        }
    }

    public void testExecutable() {
        GoOs.init(new String[] {"entrypoint.go", "a"});
        Object exe = GoOs.executable();
        checkEq(0, "entrypoint.go".compareTo(
                new String(((GoString) exe).bytes(), StandardCharsets.UTF_8)),
                "executable == argv[0]");
        GoOs.init(new String[0]);
        Object empty = GoOs.executable();
        checkEq(0, ((GoString) empty).bytes().length,
                "executable empty without argv");
    }

    public void testSpawnTrue() {
        GoOs.init(new String[] {"t"});
        long cookie = spawn("/bin/true", strSlice("true"), null);
        check(cookie > 0, "spawn true cookie: " + cookie);
        check(GoOs.procPid(Long.valueOf(cookie)) > 0, "pid > 0");
        checkEq(0, GoOs.procWait(Long.valueOf(cookie)), "wait true");
        checkEq(0, GoOs.procExitCode(Long.valueOf(cookie)), "exit 0");
        checkEq(0, GoOs.procSignal(Long.valueOf(cookie)), "no signal");
        check(GoOs.procCpuNanos(Long.valueOf(cookie)) >= 0, "cpu nanos >= 0");
        checkEq(0, GoOs.procRelease(Long.valueOf(cookie)), "release");
        checkEq(-GoOs.EBADF, GoOs.procPid(Long.valueOf(cookie)),
                "pid after release");
    }

    public void testSpawnExitCode() {
        GoOs.init(new String[] {"t"});
        long cookie = spawn("/bin/sh", strSlice("sh", "-c", "exit 3"), null);
        check(cookie > 0, "spawn sh cookie: " + cookie);
        checkEq(0, GoOs.procWait(Long.valueOf(cookie)), "wait sh");
        checkEq(3, GoOs.procExitCode(Long.valueOf(cookie)), "exit 3");
    }

    public void testSpawnMissing() {
        GoOs.init(new String[] {"t"});
        long r = spawn("/nonexistent-binary-xyz", strSlice("x"), null);
        checkEq(-GoOs.ENOENT, r, "missing binary");
    }

    public void testSecondWait() {
        GoOs.init(new String[] {"t"});
        long cookie = spawn("/bin/true", strSlice("true"), null);
        checkEq(0, GoOs.procWait(Long.valueOf(cookie)), "first wait");
        checkEq(-GoOs.ECHILD, GoOs.procWait(Long.valueOf(cookie)),
                "second wait");
    }

    public void testKillAfterWait() {
        GoOs.init(new String[] {"t"});
        long cookie = spawn("/bin/true", strSlice("true"), null);
        checkEq(0, GoOs.procWait(Long.valueOf(cookie)), "wait");
        checkEq(-GoOs.ECHILD, GoOs.procKill(Long.valueOf(cookie),
                Long.valueOf(9)), "kill after wait");
    }

    public void testKillLive() {
        GoOs.init(new String[] {"t"});
        long cookie = spawn("/bin/sleep", strSlice("sleep", "30"), null);
        check(cookie > 0, "spawn sleep cookie: " + cookie);
        checkEq(0, GoOs.procKill(Long.valueOf(cookie), Long.valueOf(9)),
                "kill live");
        checkEq(0, GoOs.procWait(Long.valueOf(cookie)), "wait killed");
        checkEq(9, GoOs.procSignal(Long.valueOf(cookie)), "signal 9");
    }

    public void testFindProcess() {
        GoOs.init(new String[] {"t"});
        long cookie = spawn("/bin/true", strSlice("true"), null);
        long pid = GoOs.procPid(Long.valueOf(cookie));
        checkEq(cookie, GoOs.findProcess(Long.valueOf(pid)),
                "find own pid");
        long foreign = GoOs.findProcess(Long.valueOf(1));
        check(foreign > 0 && foreign != cookie, "foreign cookie: " + foreign);
        checkEq(-GoOs.ECHILD, GoOs.procWait(Long.valueOf(foreign)),
                "wait foreign");
        checkEq(0, GoOs.procRelease(Long.valueOf(foreign)),
                "release foreign");
        checkEq(0, GoOs.procWait(Long.valueOf(cookie)), "wait own after");
    }

    public void testSpawnEnv() {
        GoOs.init(new String[] {"t"});
        // Non-nil env replaces the environment exactly.
        long cookie = spawn("/bin/sh",
                strSlice("sh", "-c", "exit $GRAALGO_TEST_SPAWNENV"),
                strSlice("GRAALGO_TEST_SPAWNENV=7"));
        checkEq(0, GoOs.procWait(Long.valueOf(cookie)), "wait env");
        checkEq(7, GoOs.procExitCode(Long.valueOf(cookie)), "env exit 7");
        // Nil env inherits the merged GoOs view, including the overlay.
        GoOs.setenv(gs("GRAALGO_TEST_SPAWNENV"), gs("5"));
        long cookie2 = spawn("/bin/sh",
                strSlice("sh", "-c", "exit $GRAALGO_TEST_SPAWNENV"), null);
        checkEq(0, GoOs.procWait(Long.valueOf(cookie2)), "wait env2");
        checkEq(5, GoOs.procExitCode(Long.valueOf(cookie2)),
                "overlay env exit 5");
    }
}
