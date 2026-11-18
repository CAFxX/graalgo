package main

// Goroutines, channels and select on the existing scheduler.

func worker(id int, jobs <-chan int, results chan<- int) {
	for j := range jobs {
		results <- j * j
	}
	_ = id
}

func gen(n int) <-chan int {
	ch := make(chan int)
	go func() {
		defer close(ch)
		for i := 0; i < n; i++ {
			ch <- i
		}
	}()
	return ch
}

type msg struct {
	id   int
	text string
}

func main() {
	jobs := make(chan int, 4)
	results := make(chan int, 4)
	for w := 0; w < 3; w++ {
		go worker(w, jobs, results)
	}
	go func() {
		for i := 1; i <= 20; i++ {
			jobs <- i
		}
		close(jobs)
	}()
	sum := 0
	for i := 0; i < 20; i++ {
		sum += <-results
	}
	println("sum", sum)

	total := 0
	for v := range gen(10) {
		total += v
	}
	println("total", total)

	done := make(chan struct{})
	data := make(chan msg)
	go func() {
		for i := 0; i < 3; i++ {
			data <- msg{i, "m"}
		}
		close(done)
	}()
	got := 0
	for running := true; running; {
		select {
		case m := <-data:
			got += m.id + len(m.text)
		case <-done:
			running = false
		}
	}
	println("got", got)

	buf := make(chan int, 2)
	sent := 0
loop:
	for {
		select {
		case buf <- sent:
			sent++
		default:
			break loop
		}
	}
	v, ok := <-buf
	close(buf)
	w, ok2 := <-buf
	x, ok3 := <-buf
	println(sent, len(buf), cap(buf), v, ok, w, ok2, x, ok3)

	var nilch chan int
	select {
	case <-nilch:
		println("unreachable")
	default:
		println("nil channel never ready")
	}

	// A mutex and a wait group built from channels.
	lock := make(chan struct{}, 1)
	wait := make(chan struct{})
	counter := 0
	const n = 50
	for i := 0; i < n; i++ {
		go func() {
			lock <- struct{}{}
			counter++
			<-lock
			wait <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-wait
	}
	println("counter", counter)

	res := make(chan string)
	go func() {
		defer func() { res <- recover().(error).Error() }()
		var c chan int
		close(c)
	}()
	println(<-res)
}
