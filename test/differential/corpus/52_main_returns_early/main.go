package main

// RV-P1-19 (gc probe p2): main returning exits the process immediately,
// abandoning the blocked child — exit 0, no deadlock report.
func main() { go func() { select {} }() }
