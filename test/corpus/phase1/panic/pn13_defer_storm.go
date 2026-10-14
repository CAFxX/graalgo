package main

func main() {
	sum := 0
	count := 0
	// probe: 10k defers, each adding its index.
	// loopcap: per-iteration capture — the deferred closure captures i directly
	// (no param dodge); gc semantics give sum = 0+1+...+9999.
	for i := 0; i < 10000; i++ {
		defer func() {
			sum += i
			count++
		}()
	}
	_ = sum
	_ = count
}

// observe: sum count
