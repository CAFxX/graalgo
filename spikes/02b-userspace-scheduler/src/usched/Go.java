package usched;

/**
 * go statement for spike 2b: one goroutine = one virtual thread, but the
 * thread's FIRST action is to park on the user-space scheduler (INIT event).
 * The drainer admits it when a GOMAXPROCS slot is free — so initial
 * accounting is uniform: no vthread ever runs unscheduled.
 */
public final class Go {
    public static Thread go(String name, Runnable f) {
        return Thread.ofVirtual()
                .inheritInheritableThreadLocals(false)
                .name("graalgo-2b-" + name)
                .start(() -> {
                    Goroutine g = Goroutine.enter(Thread.currentThread());
                    try {
                        UserScheduler.instance().parkInitial(g);
                        f.run();
                    } finally {
                        UserScheduler.instance().noteExit(g);
                        Goroutine.exit();
                    }
                });
    }

    public static Thread go(Runnable f) {
        return go("goroutine-" + System.nanoTime(), f);
    }

    private Go() {}
}
