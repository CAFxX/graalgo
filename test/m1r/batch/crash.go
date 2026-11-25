// Its process dies under it (test/m1r/batch.sh): it writes its pid and
// waits; the test SIGKILLs that pid — in the batch, the batch JVM, as a
// JVM crash or the OOM killer would — and the batch reruns it and every
// later program in their own processes. Rerun (the test left a "killed"
// file), it ends at once.
package main

import (
	"os"
	"strconv"
	"time"
)

func main() {
	if _, err := os.Stat("killed"); err == nil {
		println("rerun in its own process")
		return
	}
	os.WriteFile("pid.tmp", []byte(strconv.Itoa(os.Getpid())), 0o644)
	os.Rename("pid.tmp", "pid")
	println("waiting to be killed")
	time.Sleep(time.Minute)
	println("not killed")
}
