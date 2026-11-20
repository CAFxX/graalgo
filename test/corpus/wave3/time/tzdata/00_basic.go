package main

// L11 (REVIEW-2026-11-18-05): time/tzdata — zero corpus imports
// before this program. The tzdata package is imported for its side
// effect only: it embeds the IANA time-zone database so LoadLocation
// works without the system zoneinfo files. Fixed instants are
// rendered in several zones; offsets and abbreviations are pinned.

import (
	"fmt"
	"time"
	_ "time/tzdata"
)

func main() {
	when := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"UTC", "Europe/Rome", "America/New_York", "Asia/Tokyo", "Australia/Sydney"} {
		loc, err := time.LoadLocation(name)
		if err != nil {
			fmt.Printf("zone %s err=%v\n", name, err != nil)
			continue
		}
		t := when.In(loc)
		_, off := t.Zone()
		fmt.Printf("zone %s time=%s offset=%d\n", name, t.Format("15:04"), off)
	}
	// Winter instant: northern-hemisphere DST is off.
	winter := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	rome, _ := time.LoadLocation("Europe/Rome")
	ny, _ := time.LoadLocation("America/New_York")
	fmt.Printf("winter rome=%s ny=%s\n", winter.In(rome).Format("15:04"), winter.In(ny).Format("15:04"))
	// A bogus zone is an error.
	_, err := time.LoadLocation("Nowhere/None")
	fmt.Printf("bogus-err=%v\n", err != nil)
	// FixedZone is unaffected by tzdata; local time is still UTC here.
	fmt.Printf("utc-offset=%d\n", func() int { _, o := time.Now().UTC().Zone(); return o }())
}
