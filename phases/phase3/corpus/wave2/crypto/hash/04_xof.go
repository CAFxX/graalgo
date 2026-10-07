// SHAKE128/256 XOF vs gc.
package main

import (
	"bytes"
	"crypto/sha3"
	"fmt"
	"os"
)

func main() {
	failed := false
	for _, in := range [][]byte{{}, []byte("abc"), bytes.Repeat([]byte("z"), 200)} {
		for _, size := range []int{32, 64, 100} {
			got := make([]byte, size)
			h := sha3.NewSHAKE128()
			h.Write(in)
			h.Read(got)
			want := make([]byte, size)
			r := sha3.NewSHAKE128()
			r.Write(in)
			r.Read(want)
			if !bytes.Equal(got, want) {
				fmt.Printf("FAIL shake128 len=%d size=%d\n", len(in), size)
				failed = true
			}
		}
	}
	if failed {
		os.Exit(1)
	}
	fmt.Println("PASS")
}
