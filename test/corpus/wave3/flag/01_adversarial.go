// 01_adversarial: parse twice, unknown flag, bad values, -h -> ErrHelp,
// duration edge cases, TextVar, invalid bool, missing arg. Values are
// read back via Lookup(name).Value.String() (pointer dereference *p is
// not in frontend/v1 scope).
package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

// NOTE: TextVar is not exercised: it requires a user-defined
// MarshalText() ([]byte, error) method, and defining multi-result methods
// is not in frontend v1 scope (WS D). The synthetic flag.TextVar path is
// gc-verified in the package source.

func val(fs *flag.FlagSet, name string) string {
	return fs.Lookup(name).Value.String()
}

func main() {
	// Parse twice: second parse sees only its own args.
	fs := flag.NewFlagSet("t2", flag.ContinueOnError)
	fs.Int("n", 1, "n")
	if err := fs.Parse([]string{"-n=5", "a"}); err != nil {
		fmt.Println("parse1 err:", err)
		os.Exit(1)
	}
	fmt.Println("n:", val(fs, "n"), "args:", fs.Args())
	if err := fs.Parse([]string{"-n=6"}); err != nil {
		fmt.Println("parse2 err:", err)
		os.Exit(1)
	}
	fmt.Println("n after reparse:", val(fs, "n"), "args:", fs.Args(), "NFlag:", fs.NFlag())

	// Unknown flag.
	fs2 := flag.NewFlagSet("t3", flag.ContinueOnError)
	fs2.Int("x", 0, "x")
	err := fs2.Parse([]string{"-nope=1"})
	fmt.Println("unknown flag err:", err != nil, "x:", val(fs2, "x"))

	// Bad int value.
	fs3 := flag.NewFlagSet("t4", flag.ContinueOnError)
	fs3.Int("y", 0, "y")
	err = fs3.Parse([]string{"-y=abc"})
	fmt.Println("bad int err:", err)

	// -h with ContinueOnError -> ErrHelp (usage goes to stderr, ignored).
	fs4 := flag.NewFlagSet("t5", flag.ContinueOnError)
	fs4.Int("z", 0, "z")
	err = fs4.Parse([]string{"-h"})
	fmt.Println("help err is ErrHelp:", err == flag.ErrHelp)

	// Duration edge cases via Set (Set returns the raw value error).
	fs5 := flag.NewFlagSet("t6", flag.ContinueOnError)
	fs5.Duration("d", 0, "d")
	for _, tc := range []struct {
		in      string
		want    time.Duration
		wantErr string
	}{
		{"300ms", 300 * time.Millisecond, ""},
		{"-1.5h", -90 * time.Minute, ""},
		{"2h45m", 2*time.Hour + 45*time.Minute, ""},
		{"1.5s", 1500 * time.Millisecond, ""},
		{"0", 0, ""},
		{"10", 0, "parse error"},
		{"1x", 0, "parse error"},
		{"", 0, "parse error"},
		{"1.2.3s", 0, "parse error"},
	} {
		err := fs5.Set("d", tc.in)
		got := val(fs5, "d")
		if tc.wantErr == "" {
			if err != nil || got != tc.want.String() {
				fmt.Printf("duration %q: got (%v, %v), want (%v, nil)\n", tc.in, got, err, tc.want)
				os.Exit(1)
			}
		} else {
			if err == nil || err.Error() != tc.wantErr {
				fmt.Printf("duration %q: got err %v, want %q\n", tc.in, err, tc.wantErr)
				os.Exit(1)
			}
		}
	}
	fmt.Println("durations ok")

	// Parse-path wraps the value error (unlike Set).
	fs5b := flag.NewFlagSet("t6b", flag.ContinueOnError)
	fs5b.Duration("d", 0, "d")
	err = fs5b.Parse([]string{"-d=10"})
	fmt.Println("parse-wrapped err:", err)

	// Invalid bool via = form.
	fs7 := flag.NewFlagSet("t8", flag.ContinueOnError)
	fs7.Bool("b", false, "b")
	err = fs7.Parse([]string{"-b=maybe"})
	fmt.Println("bad bool err:", err)

	// Missing argument.
	fs8 := flag.NewFlagSet("t9", flag.ContinueOnError)
	fs8.String("s", "", "s")
	err = fs8.Parse([]string{"-s"})
	fmt.Println("missing arg err:", err)

	// Lone dash and empty args.
	fs9 := flag.NewFlagSet("t10", flag.ContinueOnError)
	if err := fs9.Parse([]string{"-", ""}); err != nil {
		fmt.Println("dash err:", err)
		os.Exit(1)
	}
	fmt.Println("dash args:", fs9.Args())
}
