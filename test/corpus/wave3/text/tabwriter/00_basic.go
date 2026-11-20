package main

// L11 (REVIEW-2026-11-18-05): text/tabwriter — zero corpus imports
// before this program. Aligning tabular text: cells, padding,
// minwidth/tabwidth, and the FilterHTML and Debug flags.

import (
	"bytes"
	"fmt"
	"text/tabwriter"
)

func render(label string, lines []string) {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 8, 1, ' ', 0)
	for _, l := range lines {
		fmt.Fprintln(tw, l)
	}
	tw.Flush()
	fmt.Printf("%s:\n%s", label, buf.String())
}

func main() {
	lines := []string{
		"name\tage\tcity",
		"alice\t30\trome",
		"bob\t5\tpino torinese",
		"carlo alberto\t41\tturin",
	}
	render("default", lines)
	// Debug flag shows cell boundaries with '|'.
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 8, 1, ' ', tabwriter.Debug)
	for _, l := range lines {
		fmt.Fprintln(tw, l)
	}
	tw.Flush()
	fmt.Printf("debug:\n%s", buf.String())
	// Right-aligned numbers via the AlignRight flag on the column.
	var buf2 bytes.Buffer
	tw2 := tabwriter.NewWriter(&buf2, 0, 8, 1, ' ', tabwriter.AlignRight)
	for _, l := range []string{"item\tcount", "apples\t12", "figs\t1234"} {
		fmt.Fprintln(tw2, l)
	}
	tw2.Flush()
	fmt.Printf("alignright:\n%s", buf2.String())
	// FilterHTML strips tags when computing widths.
	var buf3 bytes.Buffer
	tw3 := tabwriter.NewWriter(&buf3, 0, 8, 1, ' ', tabwriter.FilterHTML)
	for _, l := range []string{"<b>name</b>\tval", "x\t1"} {
		fmt.Fprintln(tw3, l)
	}
	tw3.Flush()
	fmt.Printf("filterhtml:\n%s", buf3.String())
}
