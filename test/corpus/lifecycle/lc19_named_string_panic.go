package main

type MyStr string

func main() {
	panic(MyStr("hi\nthere"))
}
