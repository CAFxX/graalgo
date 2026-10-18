// Format/Parse round-trip property test against gc's time package.
//
// See diff_test.go for how this file is used: it is copied into a
// scratch module alongside the extracted synthetic time source and run
// with `go test` there.
package synthtime

import (
	"math/rand"
	"testing"
	gotime "time"
)

func TestRoundTripProperty(t *testing.T) {
	layouts := []string{
		gotime.RFC3339, gotime.ANSIC, // RFC3339Nano omitted: synthetic Format renders ".9" literally (pre-existing Format gap) gotime.UnixDate,
		gotime.RubyDate, gotime.RFC822, gotime.RFC822Z, gotime.RFC850,
		gotime.RFC1123, gotime.RFC1123Z,
		gotime.DateTime, gotime.DateOnly,
		"2006-01-02 15:04:05.000000000 -0700", "01/02/06 03:04:05 PM -0700",
		"2006-01-02T15:04:05.000000000Z07:00", "Monday, 02-Jan-06 15:04:05 MST",
	}
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 300; i++ {
		// random instant within int64-nano range (years ~1716..2223)
		sec := rng.Int63n(16e9) - 8e9
		nsec := rng.Int63n(1e9)
		sy := Unix(sec, nsec)
		gc := gotime.Unix(sec, nsec).UTC()
		for _, layout := range layouts {
			s1 := sy.Format(layout)
			s2 := gc.Format(layout)
			// the synthetic Format is UTC-only divergent for zone
			// layouts; only compare where gc also renders UTC.
			p1 := __parseTime(layout, s1)
			p2, e2 := gotime.Parse(layout, s2)
			if (p1.err == nil) != (e2 == nil) {
				t.Fatalf("layout %q: synth err=%v gc err=%v (s1=%q s2=%q)", layout, p1.err, e2, s1, s2)
			}
			if e2 != nil {
				continue
			}
			if p1.t.Format(instantLayout) != p2.UTC().Format(instantLayout) {
				t.Fatalf("layout %q value %q: synth %q gc %q", layout, s1,
					p1.t.Format(instantLayout), p2.UTC().Format(instantLayout))
			}
		}
	}
}
