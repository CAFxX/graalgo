package main

// RV-P1-19: empty select blocks forever — gc prints
// "fatal error: all goroutines are asleep - deadlock!" and exits 2.
func main() { select {} }
