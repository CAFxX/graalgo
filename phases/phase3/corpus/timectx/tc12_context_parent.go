package main

import "context"

// tc12: parent cancel propagates to child contexts.
func main() {
	parent, parentCancel := context.WithCancel(context.Background())
	child, _ := context.WithCancel(parent)
	grandchild, _ := context.WithCancel(child)
	parentCancel()
	<-child.Done()
	<-grandchild.Done()
	if child.Err() == context.Canceled && grandchild.Err() == context.Canceled {
		println("propagated")
	}
}
