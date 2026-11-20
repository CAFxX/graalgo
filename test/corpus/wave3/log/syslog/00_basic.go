package main

// L11 (REVIEW-2026-11-18-05): log/syslog — zero corpus imports
// before this program. Writing to the system logger: New with a
// valid priority, a Write, Close, and the error path on a bad
// priority. Only booleans are printed (the daemon's queue is not
// observable from here).

import (
	"fmt"
	"log/syslog"
)

func main() {
	w, err := syslog.New(syslog.LOG_INFO|syslog.LOG_USER, "graalgo-1805")
	fmt.Printf("new-err=%v\n", err != nil)
	if err == nil {
		if err := w.Info("differential corpus probe"); err != nil {
			fmt.Printf("write-err=%v\n", err != nil)
		} else {
			fmt.Printf("write-ok=true\n")
		}
		if err := w.Warning("second probe"); err != nil {
			fmt.Printf("warning-err=%v\n", err != nil)
		} else {
			fmt.Printf("warning-ok=true\n")
		}
		w.Close()
		fmt.Printf("close-ok=true\n")
	}
	// Bad priority: New rejects it.
	_, err = syslog.New(syslog.Priority(-1), "bad")
	fmt.Printf("bad-priority-err=%v\n", err != nil)
	// Dial with an empty network/address defaults to the local syslog.
	_, err = syslog.Dial("", "", syslog.LOG_INFO, "demo")
	fmt.Printf("dial-empty-err=%v\n", err != nil)
}
