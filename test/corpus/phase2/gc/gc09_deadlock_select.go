package main

// R1-22: `select {}` blocks forever with no other goroutine, timer,
// I/O, or external waiter outstanding. gc exits 2 with "fatal error:
// all goroutines are asleep - deadlock!"; graalgo must do the same
// (via SchedulerCore.checkDeadlock) instead of hanging until an
// external timeout. No .expected file: stdout is empty and the
// assertion is on exit code 2 + the fatal line on stderr (see
// GcDifferentialTest.testGc09DeadlockSelect).
func main() {
	select {}
}
