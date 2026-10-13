// 04_count: -test.count=2 runs each test twice (see 04_count.args).
package main

import (
	"fmt"
	"testing"
)

var runs int

func TestCounted(t *testing.T) {
	runs++
	fmt.Println("COUNTED-RUN", runs)
}

func main() {
	tests := []testing.InternalTest{
		{"TestCounted", TestCounted},
	}
	// Note: testing.Main calls os.Exit; nothing runs after it.
	testing.Main(nil, tests, nil, nil)
}

