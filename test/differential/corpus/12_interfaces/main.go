package main

import "fmt"

type Speaker interface{ Speak() string }

type Dog struct{ Name string }

func (d Dog) Speak() string { return d.Name + " says woof" }

type Robot struct{}

func (r Robot) Speak() string { return "beep" }

func announce(s Speaker) { fmt.Println(s.Speak()) }

func main() {
	announce(Dog{"Rex"})
	announce(Robot{})
	var s Speaker = Dog{"Max"}
	if d, ok := s.(Dog); ok {
		fmt.Println("dog:", d.Name)
	}
	fmt.Println(s == nil)
}
