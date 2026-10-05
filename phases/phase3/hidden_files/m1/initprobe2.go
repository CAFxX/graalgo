package main

var inited = false

func init() {
	inited = true
}

func main() {
	if inited {
		println("INIT RAN")
	} else {
		println("INIT DID NOT RUN")
	}
}
