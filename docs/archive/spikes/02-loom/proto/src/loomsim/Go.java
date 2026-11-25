package loomsim;

/**
 * go statement: one goroutine = one virtual thread (T5 §3.1).
 * Names follow T7 §2.2: "graalgo-goroutine-<id>" so profilers read cleanly.
 * Each goroutine gets a Goroutine control block (preempt flag etc.) modeled
 * as virtual-thread-scoped state.
 *
 * NOTE on scheduler customization (verified against JDK 25 sources,
 * ~/tools/graalvm-jdk-25/lib/src.zip, java/lang/Thread.java +
 * ThreadBuilders.java): the public Builder.OfVirtual API has NO
 * scheduler(Executor) method; the VirtualThreadBuilder(Executor) constructor
 * is package-private ("invoked by tests"). The carrier pool is per-JVM and
 * configured ONLY via the system properties
 * jdk.virtualThreadScheduler.parallelism (default: #processors) and
 * jdk.virtualThreadScheduler.maxPoolSize (default: 256). So GOMAXPROCS
 * fidelity is a LAUNCHER concern: translate GOMAXPROCS=n into
 * -Djdk.virtualThreadScheduler.parallelism=n before JVM boot. In-process
 * runtime.GOMAXPROCS(n) changes cannot resize the carrier pool via public
 * API — documented deviation (see CARRIER_POOL.md).
 */
public final class Go {
    /** go f() — spawn one virtual thread per goroutine. */
    public static Thread go(Runnable f) {
        Goroutine[] holder = new Goroutine[1];
        Thread t = Thread.ofVirtual()
                .inheritInheritableThreadLocals(false)
                .start(() -> {
                    Goroutine g = Goroutine.enter();
                    holder[0] = g;
                    Thread.currentThread().setName("graalgo-goroutine-" + g.id);
                    try {
                        f.run();
                    } finally {
                        Goroutine.exit(g);
                    }
                });
        return t;
    }

    /** Current carrier-pool parallelism as seen by the runtime. */
    public static int carrierParallelism() {
        return Integer.getInteger("jdk.virtualThreadScheduler.parallelism",
                Runtime.getRuntime().availableProcessors());
    }

    private Go() {}
}
