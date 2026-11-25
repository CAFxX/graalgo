package main

// Loop-capture adversarial: goto out of the loop; closures created before
// the jump keep their per-iteration cells.
func main() {
	var fns [3]func() int
	k := 0
	for i := 0; i < 5; i++ {
		x := i
		fns[k] = func() int { return x }
		k++
		if i == 2 {
			goto done
		}
	}
done:
	sum := 0
	for _, f := range fns {
		sum += f()
	}
	_ = sum
	_ = k
}

// observe: sum k
