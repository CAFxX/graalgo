package main

type E struct{ msg string }

func (e *E) Error() string { return "E: " + e.msg }

func main() {
	defer println("main deferred")
	defer func() {
		r := recover()
		println("recovered:", r.(error).Error())
		panic(&E{"second"})
	}()
	var p *E
	println(p.msg)
}
