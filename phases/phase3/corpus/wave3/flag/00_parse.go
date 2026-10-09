// 00_parse: all flag types, -f=v / -f v / --f forms, -- terminator,
// NFlag/NArg/Args/Visit/VisitAll/Lookup/Set. Values are read back via
// Lookup(name).Value.String() (pointer dereference *p is not in
// frontend/v1 scope).
package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func val(fs *flag.FlagSet, name string) string {
	return fs.Lookup(name).Value.String()
}

func main() {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Bool("b", false, "bool flag")
	fs.Int("i", 0, "int flag")
	fs.Int64("i64", 0, "int64 flag")
	fs.Uint("u", 0, "uint flag")
	fs.Uint64("u64", 0, "uint64 flag")
	fs.String("s", "", "string flag")
	fs.Float64("f", 0, "float flag")
	fs.Duration("d", 0, "duration flag")
	var custom string
	fs.Func("c", "custom flag", func(v string) error { custom = "C=" + v; return nil })
	var bfVal string
	fs.BoolFunc("bf", "boolfunc flag", func(v string) error { bfVal = "BF=" + v; return nil })

	args := []string{
		"-b",
		"-i=42",
		"--i64", "-7",
		"-u", "8",
		"-u64=9",
		"-s=hello",
		"-f=1.5",
		"-d=2h45m",
		"-c", "x",
		"-bf=false",
		"--",
		"-not-a-flag",
		"pos1",
	}
	if err := fs.Parse(args); err != nil {
		fmt.Println("parse err:", err)
		os.Exit(1)
	}
	fmt.Println("b:", val(fs, "b"))
	fmt.Println("i:", val(fs, "i"))
	fmt.Println("i64:", val(fs, "i64"))
	fmt.Println("u:", val(fs, "u"))
	fmt.Println("u64:", val(fs, "u64"))
	fmt.Println("s:", val(fs, "s"))
	fmt.Println("f:", val(fs, "f"))
	fmt.Println("d:", val(fs, "d"), "==", (2*time.Hour+45*time.Minute).String())
	fmt.Println("custom:", custom)
	fmt.Println("bf:", bfVal)
	fmt.Println("NFlag:", fs.NFlag())
	fmt.Println("NArg:", fs.NArg())
	fmt.Println("Arg0:", fs.Arg(0), "Arg1:", fs.Arg(1), "Arg99:", fs.Arg(99))
	fmt.Println("Args:", fs.Args())
	visited := []string{}
	fs.Visit(func(fl *flag.Flag) { visited = append(visited, fl.Name) })
	fmt.Println("Visit:", visited)
	all := []string{}
	fs.VisitAll(func(fl *flag.Flag) { all = append(all, fl.Name) })
	fmt.Println("VisitAll:", all)
	fmt.Println("Lookup i:", fs.Lookup("i").DefValue)
	fmt.Println("Lookup missing:", fs.Lookup("nope") == nil)
	if err := fs.Set("i", "100"); err != nil {
		fmt.Println("set err:", err)
		os.Exit(1)
	}
	fmt.Println("i after Set:", val(fs, "i"))
	fmt.Println("Parsed:", fs.Parsed())
}
