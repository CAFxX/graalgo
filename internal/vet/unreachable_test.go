package vet

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUnreachableInfiniteLoopExits pins when an infinite for loop counts
// as exiting (so the statement after it is reachable) and when it counts
// as terminating (so the statement after it is flagged unreachable).
// Each case is a function body; want is the number of "unreachable code"
// diagnostics the analyzer must report for the whole snippet.
func TestUnreachableInfiniteLoopExits(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{
			name: "labeled break naming the loop itself",
			body: "L: for {\n\t\tbreak L\n\t}\n\tprintln(\"after\")\n",
			want: 0,
		},
		{
			name: "labeled break naming the loop from inside an if",
			body: "L: for {\n\t\tif true {\n\t\t\tbreak L\n\t\t}\n\t}\n\tprintln(\"after\")\n",
			want: 0,
		},
		{
			name: "goto to a label after the loop",
			body: "for {\n\t\tgoto done\n\t}\ndone:\n\tprintln(\"after\")\n",
			want: 0,
		},
		{
			name: "goto out from inside an if",
			body: "for {\n\t\tif true {\n\t\t\tgoto done\n\t\t}\n\t}\ndone:\n\tprintln(\"after\")\n",
			want: 0,
		},
		{
			name: "plain infinite loop never exits",
			body: "for {\n\t}\n\tprintln(\"after\")\n",
			want: 1,
		},
		{
			name: "unlabeled break exits",
			body: "for {\n\t\tbreak\n\t}\n\tprintln(\"after\")\n",
			want: 0,
		},
		{
			name: "break in the else branch exits",
			body: "for {\n\t\tif true {\n\t\t} else {\n\t\t\tbreak\n\t\t}\n\t}\n\tprintln(\"after\")\n",
			want: 0,
		},
		{
			name: "goto to a label inside the loop does not exit",
			body: "for {\n\t\tif true {\n\t\t\tgoto again\n\t\t}\nagain:\n\t\tprintln(\"x\")\n\t}\n\tprintln(\"after\")\n",
			want: 1,
		},
		{
			name: "break naming a labeled block inside the body does not exit",
			body: "for {\n\t\tL: {\n\t\t\tbreak L\n\t\t}\n\t}\n\tprintln(\"after\")\n",
			want: 1,
		},
		{
			name: "break naming the outer loop skips the inner follow-up only",
			body: "Outer: for {\n\t\tfor {\n\t\t\tbreak Outer\n\t\t}\n\t\tprintln(\"inner-after\")\n\t}\n\tprintln(\"outer-after\")\n",
			want: 1,
		},
		{
			name: "break naming the outer loop from inside a switch exits it",
			body: "Outer: for {\n\t\tswitch 1 {\n\t\tcase 1:\n\t\t\tbreak Outer\n\t\t}\n\t}\n\tprintln(\"after\")\n",
			want: 0,
		},
		{
			name: "unlabeled break inside a switch does not exit the loop",
			body: "for {\n\t\tswitch 1 {\n\t\tcase 1:\n\t\t\tbreak\n\t\t}\n\t}\n\tprintln(\"after\")\n",
			want: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "package main\n\nfunc f() {\n\t" + tc.body + "}\n"
			dir := t.TempDir()
			path := filepath.Join(dir, "case.go")
			if err := os.WriteFile(path, []byte(src), 0644); err != nil {
				t.Fatal(err)
			}
			ds, err := Run([]string{path})
			if err != nil {
				t.Fatal(err)
			}
			var got int
			var msgs []string
			for _, d := range ds {
				if d.Message == "unreachable code" {
					got++
					msgs = append(msgs, d.Pos.String())
				}
			}
			if got != tc.want {
				t.Errorf("got %d unreachable-code diagnostics, want %d (%v)\n%s",
					got, tc.want, msgs, src)
			}
		})
	}
}
