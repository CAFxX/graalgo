package main

func main() {
	var i any = "x"
	_ = i.(int)
}

// panic: panic: interface conversion: interface {} is string, not int
