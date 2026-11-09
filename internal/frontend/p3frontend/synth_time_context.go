package p3frontend

// P3-M5 (WS E, ADR-0005): p3frontend mirrors of the phase-1 synthetic
// "time" and "context" packages. The importer reimplements the frozen
// exporter's synthetic signatures (it cannot reuse them — unexported and
// frozen); these mirrors pin the signatures so drift fails fast.
//
// The signatures match internal/frontend/synth_time_context.go
// exactly (not gc's real packages — the synthetic Time is
// struct{unixnano int64; isSet bool}, Context.Done returns
// chan struct{}, etc.).
//
// P3-M9: isSet distinguishes the zero Time (0001-01-01, isSet=false)
// from 1970-01-01 (unixnano==0, isSet=true).
//
// DIVERGENCE (documented, pinned by TestTimeSyntheticSignatures): the
// mirror declares Time.String, which the phase-1 synthetic source lacks
// (a live-path gap — gc has it). Removing it would break the pinned
// test; the drift test allows exactly this one extra method.

import (
	"go/constant"
	"go/token"
	"go/types"
)

// constantValue builds a go/constant int64 value for typed constants.
func constantValue(v int64) constant.Value {
	return constant.MakeInt64(v)
}

// syntheticTimePackage mirrors the frozen P3-M5 synthetic "time".
func syntheticTimePackage() *types.Package {
	pkg := types.NewPackage("time", "time")
	byteT := types.Universe.Lookup("byte").Type()
	intT := types.Typ[types.Int]
	int64T := types.Typ[types.Int64]
	stringT := types.Typ[types.String]
	boolT := types.Typ[types.Bool]
	float64T := types.Typ[types.Float64]
	errorT := types.Universe.Lookup("error").Type()

	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}
	newField := func(name string, t types.Type) *types.Var {
		return types.NewField(token.NoPos, pkg, name, t, false)
	}

	// Duration = int64.
	durationName := types.NewTypeName(token.NoPos, pkg, "Duration", nil)
	pkg.Scope().Insert(durationName)
	duration := types.NewNamed(durationName, int64T, nil)

	// Duration constants.
	durationConsts := []struct {
		name string
		val  int64
	}{
		{"Nanosecond", 1},
		{"Microsecond", 1000},
		{"Millisecond", 1000000},
		{"Second", 1000000000},
		{"Minute", 60000000000},
		{"Hour", 3600000000000},
	}
	for _, c := range durationConsts {
		pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, c.name,
			duration, constantValue(c.val)))
	}

	// Reference layout constants (gc 1.27.1 time/format.go exact).
	// P4 (driver-harden): mirror of the layout consts added to the
	// phase-1 synthetic; real-world packages reference them.
	layoutConsts := []struct {
		name string
		val  string
	}{
		{"Layout", "01/02 03:04:05PM '06 -0700"},
		{"ANSIC", "Mon Jan _2 15:04:05 2006"},
		{"UnixDate", "Mon Jan _2 15:04:05 MST 2006"},
		{"RubyDate", "Mon Jan 02 15:04:05 -0700 2006"},
		{"RFC822", "02 Jan 06 15:04 MST"},
		{"RFC822Z", "02 Jan 06 15:04 -0700"},
		{"RFC850", "Monday, 02-Jan-06 15:04:05 MST"},
		{"RFC1123", "Mon, 02 Jan 2006 15:04:05 MST"},
		{"RFC1123Z", "Mon, 02 Jan 2006 15:04:05 -0700"},
		{"RFC3339", "2006-01-02T15:04:05Z07:00"},
		{"RFC3339Nano", "2006-01-02T15:04:05.999999999Z07:00"},
		{"Kitchen", "3:04PM"},
		{"Stamp", "Jan _2 15:04:05"},
		{"StampMilli", "Jan _2 15:04:05.000"},
		{"StampMicro", "Jan _2 15:04:05.000000"},
		{"StampNano", "Jan _2 15:04:05.000000000"},
		{"DateTime", "2006-01-02 15:04:05"},
		{"DateOnly", "2006-01-02"},
		{"TimeOnly", "15:04:05"},
	}
	for _, c := range layoutConsts {
		// Untyped, as in the phase-1 source (and gc): the drift test
		// compares the rendered const type.
		pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, c.name,
			types.Typ[types.UntypedString], constant.MakeString(c.val)))
	}

	// Duration methods.
	durationMethods := []struct {
		name    string
		params  []*types.Var
		results []*types.Var
	}{
		{"String", nil, []*types.Var{newParam("", stringT)}},
		{"Nanoseconds", nil, []*types.Var{newParam("", int64T)}},
		{"Microseconds", nil, []*types.Var{newParam("", int64T)}},
		{"Milliseconds", nil, []*types.Var{newParam("", int64T)}},
		{"Seconds", nil, []*types.Var{newParam("", float64T)}},
		{"Minutes", nil, []*types.Var{newParam("", float64T)}},
		{"Hours", nil, []*types.Var{newParam("", float64T)}},
		{"Truncate", []*types.Var{newParam("m", duration)}, []*types.Var{newParam("", duration)}},
		{"Round", []*types.Var{newParam("m", duration)}, []*types.Var{newParam("", duration)}},
		{"Abs", nil, []*types.Var{newParam("", duration)}},
	}
	for _, m := range durationMethods {
		sig := types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", duration),
			nil, nil,
			types.NewTuple(m.params...), types.NewTuple(m.results...), false)
		duration.AddMethod(types.NewFunc(token.NoPos, pkg, m.name, sig))
	}

	// Time = struct{ unixnano int64; isSet bool }.
	timeName := types.NewTypeName(token.NoPos, pkg, "Time", nil)
	pkg.Scope().Insert(timeName)
	timeStruct := types.NewStruct(
		[]*types.Var{newField("unixnano", int64T), newField("isSet", boolT)},
		[]string{"unixnano", "isSet"})
	timeType := types.NewNamed(timeName, timeStruct, nil)

	// Time methods.
	timeMethods := []struct {
		name    string
		params  []*types.Var
		results []*types.Var
	}{
		{"Unix", nil, []*types.Var{newParam("", int64T)}},
		{"UnixNano", nil, []*types.Var{newParam("", int64T)}},
		{"Add", []*types.Var{newParam("d", duration)}, []*types.Var{newParam("", timeType)}},
		{"Sub", []*types.Var{newParam("u", timeType)}, []*types.Var{newParam("", duration)}},
		{"Before", []*types.Var{newParam("u", timeType)}, []*types.Var{newParam("", boolT)}},
		{"After", []*types.Var{newParam("u", timeType)}, []*types.Var{newParam("", boolT)}},
		{"Equal", []*types.Var{newParam("u", timeType)}, []*types.Var{newParam("", boolT)}},
		{"Compare", []*types.Var{newParam("u", timeType)}, []*types.Var{newParam("", intT)}},
		{"IsZero", nil, []*types.Var{newParam("", boolT)}},
		{"String", nil, []*types.Var{newParam("", stringT)}},
		{"Format", []*types.Var{newParam("layout", stringT)}, []*types.Var{newParam("", stringT)}},
		{"Round", []*types.Var{newParam("d", duration)}, []*types.Var{newParam("", timeType)}},
		{"Truncate", []*types.Var{newParam("d", duration)}, []*types.Var{newParam("", timeType)}},
		{"AppendFormat", []*types.Var{newParam("b", types.NewSlice(byteT)), newParam("layout", stringT)}, []*types.Var{newParam("", types.NewSlice(byteT))}},
		{"MarshalText", nil, []*types.Var{newParam("", types.NewSlice(byteT)), newParam("", errorT)}},
		{"GoString", nil, []*types.Var{newParam("", stringT)}},
		{"Hour", nil, []*types.Var{newParam("", intT)}},
		{"Minute", nil, []*types.Var{newParam("", intT)}},
		{"Second", nil, []*types.Var{newParam("", intT)}},
		{"Nanosecond", nil, []*types.Var{newParam("", intT)}},
		{"YearDay", nil, []*types.Var{newParam("", intT)}},
		{"UTC", nil, []*types.Var{newParam("", timeType)}},
		{"Local", nil, []*types.Var{newParam("", timeType)}},
		{"UnixMilli", nil, []*types.Var{newParam("", int64T)}},
		{"UnixMicro", nil, []*types.Var{newParam("", int64T)}},
	}
	for _, m := range timeMethods {
		sig := types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", timeType),
			nil, nil,
			types.NewTuple(m.params...), types.NewTuple(m.results...), false)
		timeType.AddMethod(types.NewFunc(token.NoPos, pkg, m.name, sig))
	}
	// UnmarshalText has a pointer receiver, so it cannot live in the
	// value-receiver table above (same shape as the multi-result
	// methods below).
	timeType.AddMethod(types.NewFunc(token.NoPos, pkg, "UnmarshalText",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", types.NewPointer(timeType)),
			nil, nil,
			types.NewTuple(newParam("data", types.NewSlice(byteT))),
			types.NewTuple(newParam("", errorT)), false)))

	// addSec is the synthetic Time's only unexported method (used by
	// the RFC 3339 parser); the drift test requires it in the mirror
	// like every other Time method.
	timeType.AddMethod(types.NewFunc(token.NoPos, pkg, "addSec",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", types.NewPointer(timeType)),
			nil, nil,
			types.NewTuple(newParam("sec", int64T)),
			nil, false)))

	// (Multi-result Date/Clock/Zone methods are added after Month is
	// defined below.)

	// Month = int.
	monthName := types.NewTypeName(token.NoPos, pkg, "Month", nil)
	pkg.Scope().Insert(monthName)
	month := types.NewNamed(monthName, intT, nil)
	month.AddMethod(types.NewFunc(token.NoPos, pkg, "String",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", month),
			nil, nil, nil,
			types.NewTuple(newParam("", stringT)), false)))
	monthConsts := []struct {
		name string
		val  int64
	}{
		{"January", 1}, {"February", 2}, {"March", 3}, {"April", 4},
		{"May", 5}, {"June", 6}, {"July", 7}, {"August", 8},
		{"September", 9}, {"October", 10}, {"November", 11}, {"December", 12},
	}
	for _, c := range monthConsts {
		pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, c.name,
			month, constantValue(c.val)))
	}

	// Weekday = int.
	weekdayName := types.NewTypeName(token.NoPos, pkg, "Weekday", nil)
	pkg.Scope().Insert(weekdayName)
	weekday := types.NewNamed(weekdayName, intT, nil)
	weekday.AddMethod(types.NewFunc(token.NoPos, pkg, "String",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", weekday),
			nil, nil, nil,
			types.NewTuple(newParam("", stringT)), false)))
	weekdayConsts := []struct {
		name string
		val  int64
	}{
		{"Sunday", 0}, {"Monday", 1}, {"Tuesday", 2}, {"Wednesday", 3},
		{"Thursday", 4}, {"Friday", 5}, {"Saturday", 6},
	}
	for _, c := range weekdayConsts {
		pkg.Scope().Insert(types.NewConst(token.NoPos, pkg, c.name,
			weekday, constantValue(c.val)))
	}

	// Location = struct{ name string } with String on *Location and
	// the Local/UTC vars (phase-1 source: both point at name "UTC";
	// Local is the same object in the synthetic world).
	locationName := types.NewTypeName(token.NoPos, pkg, "Location", nil)
	pkg.Scope().Insert(locationName)
	locationStruct := types.NewStruct(
		[]*types.Var{newField("name", stringT)},
		[]string{"name"})
	locationType := types.NewNamed(locationName, locationStruct, nil)
	locationPtr := types.NewPointer(locationType)
	locationType.AddMethod(types.NewFunc(token.NoPos, pkg, "String",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", locationPtr),
			nil, nil, nil,
			types.NewTuple(newParam("", stringT)), false)))
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "Local", locationPtr))
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "UTC", locationPtr))

	// ParseError = struct{Layout, Value, LayoutElem, ValueElem,
	// Message string} with Error on *ParseError (gc time/format.go:841;
	// added for the v2 json engine, which constructs it directly).
	parseErrorName := types.NewTypeName(token.NoPos, pkg, "ParseError", nil)
	pkg.Scope().Insert(parseErrorName)
	parseErrorStruct := types.NewStruct(
		[]*types.Var{
			newField("Layout", stringT),
			newField("Value", stringT),
			newField("LayoutElem", stringT),
			newField("ValueElem", stringT),
			newField("Message", stringT),
		},
		nil)
	parseErrorType := types.NewNamed(parseErrorName, parseErrorStruct, nil)
	parseErrorType.AddMethod(types.NewFunc(token.NoPos, pkg, "Error",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", types.NewPointer(parseErrorType)),
			nil, nil, nil,
			types.NewTuple(newParam("", stringT)), false)))

	// Multi-result Time methods (defined with exact gc signatures, fail
	// fast at emission — see phase-1 isMultiResultSynthetic).
	timeType.AddMethod(types.NewFunc(token.NoPos, pkg, "Date",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", timeType),
			nil, nil, nil,
			types.NewTuple(
				newParam("year", intT),
				newParam("month", month),
				newParam("day", intT)), false)))
	timeType.AddMethod(types.NewFunc(token.NoPos, pkg, "Clock",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", timeType),
			nil, nil, nil,
			types.NewTuple(
				newParam("", intT),
				newParam("", intT),
				newParam("", intT)), false)))
	timeType.AddMethod(types.NewFunc(token.NoPos, pkg, "Zone",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", timeType),
			nil, nil, nil,
			types.NewTuple(
				newParam("", stringT),
				newParam("", intT)), false)))
	timeType.AddMethod(types.NewFunc(token.NoPos, pkg, "AddDate",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", timeType),
			nil, nil,
			types.NewTuple(
				newParam("years", intT),
				newParam("months", intT),
				newParam("days", intT)),
			types.NewTuple(newParam("", timeType)), false)))
	timeType.AddMethod(types.NewFunc(token.NoPos, pkg, "Year",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", timeType),
			nil, nil, nil,
			types.NewTuple(newParam("", intT)), false)))
	timeType.AddMethod(types.NewFunc(token.NoPos, pkg, "Month",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", timeType),
			nil, nil, nil,
			types.NewTuple(newParam("", month)), false)))
	timeType.AddMethod(types.NewFunc(token.NoPos, pkg, "Day",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", timeType),
			nil, nil, nil,
			types.NewTuple(newParam("", intT)), false)))
	timeType.AddMethod(types.NewFunc(token.NoPos, pkg, "Weekday",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", timeType),
			nil, nil, nil,
			types.NewTuple(newParam("", weekday)), false)))
	timeType.AddMethod(types.NewFunc(token.NoPos, pkg, "In",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", timeType),
			nil, nil,
			types.NewTuple(newParam("loc", locationPtr)),
			types.NewTuple(newParam("", timeType)), false)))
	timeType.AddMethod(types.NewFunc(token.NoPos, pkg, "Location",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", timeType),
			nil, nil, nil,
			types.NewTuple(newParam("", locationPtr)), false)))

	// Timer = struct{ C <-chan Time } with Stop/Reset.
	timerName := types.NewTypeName(token.NoPos, pkg, "Timer", nil)
	pkg.Scope().Insert(timerName)
	timeChan := types.NewChan(types.RecvOnly, timeType)
	timerStruct := types.NewStruct(
		[]*types.Var{newField("C", timeChan)},
		[]string{"C"})
	timerType := types.NewNamed(timerName, timerStruct, nil)
	timerPtr := types.NewPointer(timerType)
	timerType.AddMethod(types.NewFunc(token.NoPos, pkg, "Stop",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", timerPtr),
			nil, nil, nil,
			types.NewTuple(newParam("", boolT)), false)))
	timerType.AddMethod(types.NewFunc(token.NoPos, pkg, "Reset",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", timerPtr),
			nil, nil,
			types.NewTuple(newParam("d", duration)),
			types.NewTuple(newParam("", boolT)), false)))

	// Ticker = struct{ C <-chan Time } with Stop/Reset.
	tickerName := types.NewTypeName(token.NoPos, pkg, "Ticker", nil)
	pkg.Scope().Insert(tickerName)
	tickerStruct := types.NewStruct(
		[]*types.Var{newField("C", timeChan)},
		[]string{"C"})
	tickerType := types.NewNamed(tickerName, tickerStruct, nil)
	tickerPtr := types.NewPointer(tickerType)
	tickerType.AddMethod(types.NewFunc(token.NoPos, pkg, "Stop",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", tickerPtr),
			nil, nil, nil, nil, false)))
	tickerType.AddMethod(types.NewFunc(token.NoPos, pkg, "Reset",
		types.NewSignatureType(
			types.NewParam(token.NoPos, pkg, "", tickerPtr),
			nil, nil,
			types.NewTuple(newParam("d", duration)), nil, false)))

	// Package-level functions.
	funcType := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	_ = funcType
	insertFunc := func(name string, params, results []*types.Var) {
		sig := types.NewSignatureType(nil, nil, nil,
			types.NewTuple(params...), types.NewTuple(results...), false)
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, name, sig))
	}
	insertFunc("Now", nil, []*types.Var{newParam("", timeType)})
	insertFunc("Sleep", []*types.Var{newParam("d", duration)}, nil)
	insertFunc("After", []*types.Var{newParam("d", duration)},
		[]*types.Var{newParam("", timeChan)})
	insertFunc("Tick", []*types.Var{newParam("d", duration)},
		[]*types.Var{newParam("", timeChan)})
	insertFunc("NewTimer", []*types.Var{newParam("d", duration)},
		[]*types.Var{newParam("", timerPtr)})
	insertFunc("NewTicker", []*types.Var{newParam("d", duration)},
		[]*types.Var{newParam("", tickerPtr)})
	insertFunc("AfterFunc", []*types.Var{
		newParam("d", duration),
		newParam("f", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
	}, []*types.Var{newParam("", timerPtr)})
	insertFunc("Since", []*types.Var{newParam("t", timeType)},
		[]*types.Var{newParam("", duration)})
	insertFunc("Until", []*types.Var{newParam("t", timeType)},
		[]*types.Var{newParam("", duration)})
	insertFunc("ParseDuration", []*types.Var{newParam("s", stringT)},
		[]*types.Var{newParam("", duration), newParam("", errorT)})
	insertFunc("Date", []*types.Var{
		newParam("year", intT),
		newParam("month", month),
		newParam("day", intT),
		newParam("hour", intT),
		newParam("min", intT),
		newParam("sec", intT),
		newParam("nsec", intT),
		newParam("loc", locationPtr),
	}, []*types.Var{newParam("", timeType)})
	insertFunc("Parse", []*types.Var{
		newParam("layout", stringT),
		newParam("value", stringT),
	}, []*types.Var{newParam("", timeType), newParam("", errorT)})
	insertFunc("Unix", []*types.Var{
		newParam("sec", int64T),
		newParam("nsec", int64T),
	}, []*types.Var{newParam("", timeType)})

	pkg.MarkComplete()
	return pkg
}

// syntheticContextPackage mirrors the frozen P3-M5 synthetic "context".
// It references the synthetic time package for time.Time/time.Duration.
func syntheticContextPackage(timePkg *types.Package) *types.Package {
	pkg := types.NewPackage("context", "context")
	boolT := types.Typ[types.Bool]
	errorT := types.Universe.Lookup("error").Type()
	anyT := types.Universe.Lookup("any").Type()

	// time.Time and time.Duration from the synthetic time package.
	timeType := timePkg.Scope().Lookup("Time").Type()
	durationType := timePkg.Scope().Lookup("Duration").Type()

	newParam := func(name string, t types.Type) *types.Var {
		return types.NewParam(token.NoPos, pkg, name, t)
	}

	// Context interface. Matches the phase-1 synthetic exactly: Deadline
	// is PRESENT (restored in phase-1 by the P4 multi-result interface
	// mechanism, 046afc86 — the old afc0dfb omission, "v1 cannot emit
	// multi-result methods", no longer holds), and Done() returns
	// <-chan struct{} (receive-only, as in the synthetic source).
	// Result names (deadline, ok) match the phase-1 source so the
	// drift test's rendered-signature comparison agrees exactly.
	ctxMethods := []*types.Func{
		types.NewFunc(token.NoPos, pkg, "Deadline",
			types.NewSignatureType(nil, nil, nil, nil,
				types.NewTuple(
					newParam("deadline", timeType),
					newParam("ok", boolT)), false)),
		types.NewFunc(token.NoPos, pkg, "Done",
			types.NewSignatureType(nil, nil, nil, nil,
				types.NewTuple(
					newParam("", types.NewChan(types.RecvOnly,
						types.NewStruct(nil, nil)))), false)),
		types.NewFunc(token.NoPos, pkg, "Err",
			types.NewSignatureType(nil, nil, nil, nil,
				types.NewTuple(newParam("", errorT)), false)),
		types.NewFunc(token.NoPos, pkg, "Value",
			types.NewSignatureType(nil, nil, nil,
				types.NewTuple(newParam("key", anyT)),
				types.NewTuple(newParam("", anyT)), false)),
	}
	ctxIface := types.NewInterfaceType(ctxMethods, nil).Complete()
	ctxName := types.NewTypeName(token.NoPos, pkg, "Context", nil)
	pkg.Scope().Insert(ctxName)
	ctxNamed := types.NewNamed(ctxName, ctxIface, nil)

	// CancelFunc = func().
	cancelFuncName := types.NewTypeName(token.NoPos, pkg, "CancelFunc", nil)
	pkg.Scope().Insert(cancelFuncName)
	cancelFunc := types.NewNamed(cancelFuncName,
		types.NewSignatureType(nil, nil, nil, nil, nil, false), nil)

	// CancelCauseFunc = func(error).
	cancelCauseFuncName := types.NewTypeName(token.NoPos, pkg, "CancelCauseFunc", nil)
	pkg.Scope().Insert(cancelCauseFuncName)
	cancelCauseFunc := types.NewNamed(cancelCauseFuncName,
		types.NewSignatureType(nil, nil, nil,
			types.NewTuple(newParam("", errorT)), nil, false), nil)

	// Package-level functions.
	insertFunc := func(name string, params, results []*types.Var) {
		sig := types.NewSignatureType(nil, nil, nil,
			types.NewTuple(params...), types.NewTuple(results...), false)
		pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, name, sig))
	}
	insertFunc("Background", nil, []*types.Var{newParam("", ctxNamed)})
	insertFunc("TODO", nil, []*types.Var{newParam("", ctxNamed)})
	insertFunc("WithCancel",
		[]*types.Var{newParam("parent", ctxNamed)},
		[]*types.Var{newParam("", ctxNamed), newParam("", cancelFunc)})
	insertFunc("WithCancelCause",
		[]*types.Var{newParam("parent", ctxNamed)},
		[]*types.Var{newParam("", ctxNamed), newParam("", cancelCauseFunc)})
	insertFunc("WithDeadline",
		[]*types.Var{newParam("parent", ctxNamed), newParam("d", timeType)},
		[]*types.Var{newParam("", ctxNamed), newParam("", cancelFunc)})
	insertFunc("WithTimeout",
		[]*types.Var{newParam("parent", ctxNamed), newParam("timeout", durationType)},
		[]*types.Var{newParam("", ctxNamed), newParam("", cancelFunc)})
	insertFunc("WithValue",
		[]*types.Var{
			newParam("parent", ctxNamed),
			newParam("key", anyT),
			newParam("val", anyT),
		},
		[]*types.Var{newParam("", ctxNamed)})
	insertFunc("Cause",
		[]*types.Var{newParam("c", ctxNamed)},
		[]*types.Var{newParam("", errorT)})
	insertFunc("AfterFunc",
		[]*types.Var{
			newParam("ctx", ctxNamed),
			newParam("f", types.NewSignatureType(nil, nil, nil, nil, nil, false)),
		},
		[]*types.Var{newParam("", types.NewSignatureType(nil, nil, nil, nil,
			types.NewTuple(newParam("", boolT)), false))})
	insertFunc("WithoutCancel",
		[]*types.Var{newParam("parent", ctxNamed)},
		[]*types.Var{newParam("", ctxNamed)})

	// Package-level vars: Canceled, DeadlineExceeded (both error).
	// The phase-1 synthetic defines them as package-level error vars.
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "Canceled", errorT))
	pkg.Scope().Insert(types.NewVar(token.NoPos, pkg, "DeadlineExceeded", errorT))

	pkg.MarkComplete()
	return pkg
}
