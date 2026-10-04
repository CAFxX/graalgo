// bitsgo: prints math/bits results for each hex uint64 on stdin, in the
// same field order as BitsForm.java.
package main

import (
	"bufio"
	"fmt"
	"math/bits"
	"os"
	"strconv"
)

func main() {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		x, err := strconv.ParseUint(sc.Text(), 16, 64)
		if err != nil {
			panic(err)
		}
		fmt.Printf("%d %d %d %016x %016x %016x %d\n",
			bits.LeadingZeros64(x), bits.TrailingZeros64(x), bits.OnesCount64(x),
			bits.Reverse64(x), bits.ReverseBytes64(x), bits.RotateLeft64(x, 17),
			bits.Len64(x))
	}
}
