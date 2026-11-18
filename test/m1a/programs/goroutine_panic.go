package main

func main() {
	done := make(chan bool)
	go func() {
		var a []int
		_ = a[5]
		done <- true
	}()
	<-done
}
