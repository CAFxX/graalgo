package graalgo.syscall;

/** One system call of the role table: number, name, the may-block bit and the argument roles. */
final class Entry {
    final int nr;
    final String name;
    /** May block indefinitely: the executor releases the scheduler slot and leaves the carrier pool. */
    final boolean mayBlock;
    /**
     * For a may-block transfer on the descriptor a1: 'r' or 'w', the readiness with which the
     * call returns without waiting (the executor polls for it and then runs inline); 0: none.
     */
    final int ready;
    /** With {@link #ready}: the index of the transfer length argument; -1 otherwise. */
    final int lenArg;
    final Role[] roles;

    Entry(int nr, String name, boolean mayBlock, int ready, Role[] roles) {
        this.nr = nr;
        this.name = name;
        this.mayBlock = mayBlock;
        this.ready = ready;
        int len = -1;
        for (Role r : roles) {
            if (r.kind == Role.K_BYTES) {
                len = r.lenArg;
            }
        }
        this.lenArg = ready != 0 ? len : -1;
        this.roles = roles;
    }
}
