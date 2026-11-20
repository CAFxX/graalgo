// reqpipe: an HTTP-handler-shaped pipeline without sockets (the T2 workload
// until net links): decode a JSON request, validate it with a regexp, update
// an account map under a mutex, encode the JSON response and a log line
// through bufio.Writer into an io.Writer — the interface-heavy hot path of
// SPEC §2.6.1's devirtualization cascade (io.Writer, json's encoders).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"sync"

	"graalgo/bench/h"
)

type Req struct {
	User   string            `json:"user"`
	Email  string            `json:"email"`
	Amount int               `json:"amount"`
	Items  []string          `json:"items"`
	Meta   map[string]string `json:"meta"`
}

type Resp struct {
	OK      bool   `json:"ok"`
	User    string `json:"user"`
	Balance int    `json:"balance"`
	Count   int    `json:"count"`
	Error   string `json:"error,omitempty"`
}

type account struct {
	balance, count int
}

var (
	reEmail  = regexp.MustCompile(`^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}$`)
	bodies   [][]byte
	mu       sync.Mutex
	accounts map[string]*account
)

// sink is the response writer: an io.Writer that hashes what it is given.
type sink struct{ h, n uint64 }

func (s *sink) Write(p []byte) (int, error) {
	for _, c := range p {
		s.h = (s.h ^ uint64(c)) * 1099511628211
	}
	s.n += uint64(len(p))
	return len(p), nil
}

func init() {
	seed := uint32(9)
	rnd := func(n int) int { seed = seed*1664525 + 1013904223; return int(seed>>8) % n }
	for i := 0; i < 1000; i++ {
		email := "user" + strconv.Itoa(rnd(500)) + "@example.com"
		if rnd(10) == 0 {
			email = "not an email"
		}
		r := Req{User: "u" + strconv.Itoa(rnd(500)), Email: email, Amount: rnd(1000) - 300,
			Meta: map[string]string{"client": "bench", "trace": strconv.Itoa(rnd(1 << 20))}}
		for j := 0; j < rnd(5); j++ {
			r.Items = append(r.Items, "sku-"+strconv.Itoa(rnd(10000)))
		}
		b, err := json.Marshal(r)
		if err != nil {
			panic(err)
		}
		bodies = append(bodies, b)
	}
}

func handle(w io.Writer, body []byte) {
	var req Req
	var resp Resp
	if err := json.Unmarshal(body, &req); err != nil {
		resp.Error = err.Error()
	} else if !reEmail.MatchString(req.Email) {
		resp.User, resp.Error = req.User, "bad email"
	} else {
		mu.Lock()
		a := accounts[req.User]
		if a == nil {
			a = &account{}
			accounts[req.User] = a
		}
		a.balance += req.Amount
		a.count += len(req.Items)
		resp = Resp{OK: true, User: req.User, Balance: a.balance, Count: a.count}
		mu.Unlock()
	}
	if err := json.NewEncoder(w).Encode(&resp); err != nil {
		panic(err)
	}
	fmt.Fprintf(w, "%s %d %v\n", resp.User, resp.Balance, resp.OK)
}

func work() int {
	accounts = map[string]*account{}
	s := &sink{h: 14695981039346656037}
	bw := bufio.NewWriterSize(s, 4096)
	for _, b := range bodies {
		handle(bw, b)
	}
	bw.Flush()
	return int(s.h>>1) ^ int(s.n)
}

func main() { h.Run("reqpipe", 10, work) }
