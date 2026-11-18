package main

import "errors"

func main() {
	panic(errors.New("an error value"))
}
