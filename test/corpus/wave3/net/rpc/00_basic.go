package main

// L11 (REVIEW-2026-11-18-05): net/rpc — zero corpus imports before
// this program. An RPC service over net.Pipe: method calls with
// reply values, an error-returning method, and the unknown-method
// error path.

import (
	"fmt"
	"net"
	"net/rpc"
)

type Args struct{ A, B int }

type Arith int

func (a *Arith) Add(args *Args, reply *int) error {
	*reply = args.A + args.B
	return nil
}

func (a *Arith) Mul(args *Args, reply *int) error {
	*reply = args.A * args.B
	return nil
}

func (a *Arith) Fail(args *Args, reply *int) error {
	return fmt.Errorf("arith always fails")
}

func main() {
	srv := rpc.NewServer()
	if err := srv.Register(new(Arith)); err != nil {
		fmt.Printf("register-err=%v\n", err != nil)
		return
	}
	c1, c2 := net.Pipe()
	go srv.ServeConn(c1)
	client := rpc.NewClient(c2)
	var reply int
	if err := client.Call("Arith.Add", &Args{3, 4}, &reply); err != nil {
		fmt.Printf("add-err=%v\n", err != nil)
		return
	}
	fmt.Printf("add=%d\n", reply)
	if err := client.Call("Arith.Mul", &Args{3, 4}, &reply); err != nil {
		fmt.Printf("mul-err=%v\n", err != nil)
		return
	}
	fmt.Printf("mul=%d\n", reply)
	err := client.Call("Arith.Fail", &Args{1, 2}, &reply)
	fmt.Printf("fail-err=%v\n", err != nil)
	err = client.Call("Arith.Nosuch", &Args{1, 2}, &reply)
	fmt.Printf("nosuch-err=%v\n", err != nil)
	err = client.Call("Nosuch.Add", &Args{1, 2}, &reply)
	fmt.Printf("noservice-err=%v\n", err != nil)
	// Async call.
	call := client.Go("Arith.Add", &Args{10, 20}, &reply, nil)
	<-call.Done
	fmt.Printf("async-add=%d err=%v\n", reply, call.Error != nil)
	client.Close()
}
