// WaitGroup misuse: a negative counter panics with gc's text.
package main

import "sync"

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Done()
	func() {
		defer func() { println("recovered:", recover().(string)) }()
		wg.Done()
	}()
	var wg2 sync.WaitGroup
	wg2.Add(-1)
}
