package main

import (
	"fmt"
	"syscall"
)

func main() {
	gids, err := syscall.Getgroups()
	fmt.Printf("err=%v gids=%v len=%d\n", err, gids, len(gids))
}
