package main

// L11 (REVIEW-2026-11-18-05): net/rpc/jsonrpc — zero corpus imports
// before this program. The same RPC service as net/rpc, but over the
// JSON codec: calls, replies, and the unknown-method error path.

import (
	"fmt"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
)

type Args struct{ A, B int }

type Arith int

func (a *Arith) Add(args *Args, reply *int) error {
	*reply = args.A + args.B
	return nil
}

func (a *Arith) Div(args *Args, reply *int) error {
	if args.B == 0 {
		return fmt.Errorf("division by zero")
	}
	*reply = args.A / args.B
	return nil
}

func main() {
	srv := rpc.NewServer()
	if err := srv.Register(new(Arith)); err != nil {
		fmt.Printf("register-err=%v\n", err != nil)
		return
	}
	c1, c2 := net.Pipe()
	go srv.ServeCodec(jsonrpc.NewServerCodec(c1))
	client := rpc.NewClientWithCodec(jsonrpc.NewClientCodec(c2))
	var reply int
	if err := client.Call("Arith.Add", &Args{10, 5}, &reply); err != nil {
		fmt.Printf("add-err=%v\n", err != nil)
		return
	}
	fmt.Printf("add=%d\n", reply)
	if err := client.Call("Arith.Div", &Args{10, 2}, &reply); err != nil {
		fmt.Printf("div-err=%v\n", err != nil)
		return
	}
	fmt.Printf("div=%d\n", reply)
	err := client.Call("Arith.Div", &Args{10, 0}, &reply)
	fmt.Printf("divzero-err=%v\n", err != nil)
	err = client.Call("Arith.Nosuch", &Args{1, 2}, &reply)
	fmt.Printf("nosuch-err=%v\n", err != nil)
	client.Close()
}
