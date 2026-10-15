// 10_args: os.Args from the launcher (R6). The runner passes the
// fixture args after "--". Only args[1:] are printed — argv[0] is the
// absolute input-file path under graalgo and the built binary under gc.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println(len(os.Args) - 1)
	for _, a := range os.Args[1:] {
		fmt.Println(a)
	}
}
