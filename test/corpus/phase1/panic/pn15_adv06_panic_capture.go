package main

// R2 adv06 (review remediation): panic/recover across loop iterations; the
// deferred closure captures the header var — M5 x loopcap interplay. A
// shared-slot bug observes i=5 in the recover path. gc: 91939.
func main() {
	log := 0
	// probe
	for i := 0; i < 5; i++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log = log*10 + i
				}
			}()
			if i%2 == 1 {
				panic("odd")
			}
			log = log*10 + 9
		}()
	}
	_ = log
}

// observe: log
