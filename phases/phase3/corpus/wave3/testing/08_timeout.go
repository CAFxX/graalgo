// 08_timeout: -test.timeout=100ms panics the test binary (see
// 08_timeout.args and 08_timeout.panic). Marked .timing by construction.
package main

import (
	"testing"
	"time"
)

func TestSleeps(t *testing.T) {
	time.Sleep(5 * time.Second)
}

func main() {
	tests := []testing.InternalTest{
		{"TestSleeps", TestSleeps},
	}
	testing.Main(nil, tests, nil, nil)
}

