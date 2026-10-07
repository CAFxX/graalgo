// wordfreq: word-frequency analyzer CLI (P3-M7 wave-1 end-to-end demo).
//
// Reads deterministic embedded CSV-ish input (rows "term,doc,count,score"),
// normalizes terms case-insensitively, aggregates counts and scores,
// computes length/score statistics, prints a text summary, and emits a
// JSON report. A realistic small CLI: malformed rows are skipped with a
// count, single-token terms are enforced, output is fully deterministic.
//
// Wave-1 packages only, through the real import machinery:
// strings (Split/Fields/ToLower/TrimSpace/HasPrefix), bytes (Join),
// strconv (Atoi/ParseFloat/FormatFloat/Itoa), sort (Strings/Ints/Sort with
// a custom sort.Interface), math (Sqrt), encoding/json (MarshalIndent),
// fmt (Println).
//
// Deliberate scope-avoidance (frontend/v1 + M7 scope, see corpus README):
//   - sort.Slice needs reflect (fail-fast) -> custom byCount sort.Interface
//     (the sort/08_custom_iface.go pattern), no funclits.
//   - multi-result synthetic calls in 2-target position only where the
//     mrdesugar whitelist covers them (strconv.Atoi/ParseFloat,
//     json.MarshalIndent); bytes.Buffer.Write* is multi-result -> the text
//     summary is assembled with single-result bytes.Join instead.
//   - no package-level vars (global inits), no pointer-receiver calls on
//     addressable values and no * deref expressions (WS D — *ast.StarExpr
//     is a scope error), no multi-result declarations of my own.
//   - map values are value structs with read-modify-write (stats[term]=st),
//     not pointers, so no deref is ever needed.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Embedded deterministic input: rows of "term,doc,count,score".
// Includes comment/blank lines, a zero-count row, a bad row, a row with
// a non-numeric count, a row with an empty term, and a unicode term — the
// CLI skips what it cannot parse, deterministically.
const input = `# wordfreq demo input: term,doc,count,score
go,doc1,3,0.90
Go,doc2,2,0.75
concurrency,doc1,1,0.99
Compiler,doc3,4,0.50
go,doc3,1,0.60
channel,doc2,2,0.80
compiler,doc1,2,0.70
Channel,doc4,1,0.85
goroutine,doc5,5,0.95
runtime,doc2,2,0.40
scheduler,doc6,0,0.20
unicode-hello,doc7,2,0.66
badrow
also,bad,notanumber,0.5
,doc9,3,0.1
empty,,,
multi word term,doc10,1,0.5
`

// termStat aggregates one normalized term's total count and its
// count-weighted score sum.
type termStat struct {
	term  string
	count int
	score float64
}

// byCount sorts by descending count, ties broken by ascending term.
type byCount []termStat

func (s byCount) Len() int { return len(s) }
func (s byCount) Less(i, j int) bool {
	if s[i].count != s[j].count {
		return s[i].count > s[j].count
	}
	return s[i].term < s[j].term
}
func (s byCount) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func main() {
	stats := map[string]termStat{}
	var lengths []int
	total := 0
	skipped := 0
	var scoreSum float64
	var scoreSq float64

	for _, line := range strings.Split(input, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) != 4 {
			skipped++
			continue
		}
		term := strings.ToLower(strings.TrimSpace(fields[0]))
		n, nerr := strconv.Atoi(strings.TrimSpace(fields[2]))
		score, serr := strconv.ParseFloat(strings.TrimSpace(fields[3]), 64)
		// Single-token terms only; non-negative counts; valid numbers.
		if term == "" || len(strings.Fields(term)) != 1 || nerr != nil || serr != nil || n < 0 {
			skipped++
			continue
		}
		st, ok := stats[term]
		if !ok {
			st = termStat{term: term}
		}
		st.count += n
		st.score += score * float64(n)
		stats[term] = st
		for i := 0; i < n; i++ {
			lengths = append(lengths, len(term))
		}
		total += n
		scoreSum += score * float64(n)
		scoreSq += score * score * float64(n)
	}

	// Deterministic term order: collect keys, sort, then rank by count.
	terms := make([]string, 0, len(stats))
	for t := range stats {
		terms = append(terms, t)
	}
	sort.Strings(terms)
	ranked := make(byCount, 0, len(stats))
	for _, t := range terms {
		ranked = append(ranked, stats[t])
	}
	sort.Sort(ranked)

	// Length statistics over all term occurrences.
	sort.Ints(lengths)
	meanLen := 0.0
	medianLen := 0.0
	if len(lengths) > 0 {
		sum := 0
		for _, l := range lengths {
			sum += l
		}
		meanLen = float64(sum) / float64(len(lengths))
		mid := len(lengths) / 2
		if len(lengths)%2 == 1 {
			medianLen = float64(lengths[mid])
		} else {
			medianLen = float64(lengths[mid-1]+lengths[mid]) / 2
		}
	}

	// Score statistics: mean and population stddev over all occurrences.
	meanScore := 0.0
	stdScore := 0.0
	if total > 0 {
		meanScore = scoreSum / float64(total)
		variance := scoreSq/float64(total) - meanScore*meanScore
		if variance < 0 {
			variance = 0 // guard float noise
		}
		stdScore = math.Sqrt(variance)
	}

	// Text summary assembled with bytes.Join.
	top := "n/a"
	if len(ranked) > 0 {
		top = ranked[0].term + "=" + strconv.Itoa(ranked[0].count)
	}
	summary := [][]byte{
		[]byte("total=" + strconv.Itoa(total)),
		[]byte("unique=" + strconv.Itoa(len(stats))),
		[]byte("skipped=" + strconv.Itoa(skipped)),
		[]byte("mean_len=" + strconv.FormatFloat(meanLen, 'f', 2, 64)),
		[]byte("median_len=" + strconv.FormatFloat(medianLen, 'f', 2, 64)),
		[]byte("mean_score=" + strconv.FormatFloat(meanScore, 'f', 4, 64)),
		[]byte("std_score=" + strconv.FormatFloat(stdScore, 'f', 4, 64)),
		[]byte("top=" + top),
	}
	fmt.Println(string(bytes.Join(summary, []byte("\n"))))

	// JSON report via encoding/json; map keys sort deterministically.
	breakdown := map[string]any{}
	for _, t := range terms {
		st := stats[t]
		ms := "n/a"
		if st.count > 0 {
			ms = strconv.FormatFloat(st.score/float64(st.count), 'f', 4, 64)
		}
		breakdown[t] = map[string]any{
			"count":      st.count,
			"mean_score": ms,
		}
	}
	report := map[string]any{
		"total":      total,
		"unique":     len(stats),
		"skipped":    skipped,
		"mean_len":   strconv.FormatFloat(meanLen, 'f', 2, 64),
		"median_len": strconv.FormatFloat(medianLen, 'f', 2, 64),
		"mean_score": strconv.FormatFloat(meanScore, 'f', 4, 64),
		"std_score":  strconv.FormatFloat(stdScore, 'f', 4, 64),
		"top":        top,
		"breakdown":  breakdown,
	}
	out, merr := json.MarshalIndent(report, "", "  ")
	if merr != nil {
		fmt.Println("marshal error:", merr)
		return
	}
	fmt.Println(string(out))
}
