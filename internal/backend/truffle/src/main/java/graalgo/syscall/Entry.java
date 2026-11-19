package graalgo.syscall;

/** One system call of the role table: number, name, the may-block bit and the argument roles. */
final class Entry {
    final int nr;
    final String name;
    /** May block indefinitely: the executor releases the scheduler slot and leaves the carrier pool. */
    final boolean mayBlock;
    final Role[] roles;

    Entry(int nr, String name, boolean mayBlock, Role[] roles) {
        this.nr = nr;
        this.name = name;
        this.mayBlock = mayBlock;
        this.roles = roles;
    }
}
