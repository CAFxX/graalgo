// defer and go statements evaluate the function value, receiver and
// arguments at the defer point: value receivers of arrays/structs are
// copied, pointer receivers alias; deferred closures see later writes;
// deferred method values on interfaces; nil func defer panics at exit.
package main

type acct struct {
	id   int
	hist [3]int
}

func (a acct) show(tag string) { println(tag, a.id, a.hist[0], a.hist[2]) }
func (a *acct) showP(tag string) {
	println(tag, a.id, a.hist[0], a.hist[2])
}

type ring [4]byte

func (r ring) str() string   { return string(r[:]) }
func (r *ring) strP() string { return string(r[:]) }
func (r ring) print(tag string) {
	println(tag, string(r[:]))
}

type shower interface{ show(string) }

func try(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			println(name, "->", r.(interface{ Error() string }).Error())
		}
	}()
	f()
}

func values() {
	a := acct{id: 1, hist: [3]int{1, 2, 3}}
	defer a.show("value recv")
	defer a.showP("ptr recv")
	defer func() { a.show("closure") }()
	var s shower = a
	defer s.show("iface")
	m := a.show
	defer m("method value")
	pa := &a
	defer pa.show("deref at defer")
	a.id = 2
	a.hist[2] = 30
	pa.hist[0] = 10
	s = acct{id: 99}
}

func rings() {
	r := ring{'a', 'b', 'c', 'd'}
	defer r.print("ring value")
	defer println("arg eval", r.str())
	defer func(x ring) { println("param copy", string(x[:])) }(r)
	defer func() { println("captured", r.str()) }()
	rp := &r
	defer println("ptr method arg", rp.strP())
	r[0] = 'Z'
	rp[3] = 'Y'
}

func loopDefers() {
	type pt struct{ x, y int }
	ps := []pt{{1, 2}, {3, 4}, {5, 6}}
	for i, p := range ps {
		defer func() { println("loop var", i, p.x) }()
		ps[i].x *= 10
	}
	for i := range ps {
		defer println("elem at defer", ps[i].x)
	}
}

func named() (res [2]int) {
	defer func() { res[1] += 100 }()
	res[0] = 1
	return [2]int{5, 6}
}

func recvEval() {
	calls := 0
	get := func() *acct {
		calls++
		return &acct{id: calls}
	}
	defer get().show("receiver evaluated once")
	defer get().showP("ptr receiver evaluated once")
	println("calls before return", calls)
}

func goStatement() {
	done := make(chan [2]int)
	x := [2]int{1, 2}
	go func(v [2]int) { done <- v }(x)
	x[0] = 100
	v := <-done
	println("go arg copied", v[0], x[0])
	a := acct{id: 5}
	ch := make(chan int)
	go func() { a.show("go value"); ch <- 1 }()
	<-ch
}

func main() {
	values()
	rings()
	loopDefers()
	r := named()
	println("named result", r[0], r[1])
	recvEval()
	goStatement()
	try("defer nil func", func() {
		var f func()
		defer f()
		println("body runs first")
	})
	try("defer nil method value", func() {
		var p *acct
		defer p.show("nope")
		println("body before deref")
	})
	try("defer nil iface", func() {
		var s shower
		defer s.show("nope")
		println("not printed")
	})
	try("panic arg evaluated", func() {
		var arr []int
		defer println("never", arr[1])
	})
}
