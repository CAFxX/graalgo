package main

func main() {
	// forward goto
	a := 0
	goto done
	a = 999
done:
	a += 1
	// backward goto as a loop
	b := 0
	c := 0
loop:
	b++
	c += b
	if b < 10 {
		goto loop
	}
	// goto out of a nested block
	d := 0
	for i := 0; i < 5; i++ {
		if i == 3 {
			goto out
		}
		d += i
	}
	d += 100
out:
	d += 1
	_ = a
	_ = b
	_ = c
	_ = d
}

// observe: a b c d
