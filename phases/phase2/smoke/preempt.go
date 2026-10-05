package main

var done1 = false
var done2 = false
var count1 = 0
var count2 = 0

func spin1() {
	for i := 0; i < 2000000; i++ {
		count1++
	}
	done1 = true
}

func spin2() {
	for i := 0; i < 2000000; i++ {
		count2++
	}
	done2 = true
}

func main() {
	go spin1()
	go spin2()
	for !(done1 && done2) {
	}
	println(count1, count2)
}
