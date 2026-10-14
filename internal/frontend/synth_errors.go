// P3-M7 (wave 1): synthetic `errors` package.
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer (no imports — the package is self-contained). The
// resulting *types.Package is what overlayImporter.Import("errors")
// returns, so the user's type-check and the synthetic source's own
// type-check share identical *types.Func/*types.TypeName objects; the
// exporter then emits the synthetic declarations' bodies into the same
// wire document (shared type/object tables) via exportSyntheticPackages.
//
// Exact gc 1.27.1 public API signatures (errors.go, wrap.go, join.go).
// Only frontend/v1-supported constructs (no reflect, no unsafe):
// New, Is, Unwrap, Join, AsType are real Go; As cannot be implemented
// without reflect, so it keeps its exact gc signature with a fail-fast
// body — fail fast, NEVER silent. Re-test when reflect lands.
package main

// syntheticErrorsSource is the P3-M7 synthetic `errors` package: exact
// gc 1.27.1 signatures for the full public API; real implementations for
// New/Is/Unwrap/Join/AsType; fail-fast body for As (needs reflect).
const syntheticErrorsSource = `package errors

// New returns an error that formats as the given text.
// Each call to New returns a distinct error value even if the text is identical.
func New(text string) error {
	return &errorString{text}
}

// errorString is a trivial implementation of error.
type errorString struct {
	s string
}

func (e *errorString) Error() string {
	return e.s
}

// ErrUnsupported indicates that a requested operation cannot be performed,
// because it is unsupported.
var ErrUnsupported = New("unsupported operation")

// unwrapper is the single-error unwrap method set. gc asserts against the
// anonymous interface{ Unwrap() error }; named here — identical semantics,
// and the v1 exporter handles named interface assertions.
type unwrapper interface {
	Unwrap() error
}

// multiUnwrapper is the multi-error unwrap method set (errors.Join results).
type multiUnwrapper interface {
	Unwrap() []error
}

// errorIsser is the Is(error) bool method set.
type errorIsser interface {
	Is(error) bool
}

// Unwrap returns the result of calling the Unwrap method on err, if err's
// type contains an Unwrap method returning error.
// Otherwise, Unwrap returns nil.
//
// Unwrap only calls a method of the form "Unwrap() error".
// In particular Unwrap does not unwrap errors returned by Join.
func Unwrap(err error) error {
	// NB: nil guard — gc's type assertion on a nil interface yields
	// ok=false, but the P3 runtime NPEs asserting a nil interface
	// (GoInterface.isNil on null). Behavior-preserving: gc returns nil
	// here too. Re-test if the runtime ever fixes nil assertions.
	if err == nil {
		return nil
	}
	u, ok := err.(unwrapper)
	if !ok {
		return nil
	}
	return u.Unwrap()
}

// Is reports whether any error in err's tree matches target.
// The target must be comparable.
//
// The tree consists of err itself, followed by the errors obtained by repeatedly
// calling its Unwrap() error or Unwrap() []error method. When err wraps multiple
// errors, Is examines err followed by a depth-first traversal of its children.
//
// An error is considered to match a target if it is equal to that target or if
// it implements a method Is(error) bool such that Is(target) returns true.
func Is(err, target error) bool {
	if err == nil || target == nil {
		return err == target
	}
	return errorsIs(err, target, errorsIsComparable(target))
}

// errorsIsComparable reports whether values of target's dynamic type can be
// compared with ==. gc uses reflectlite.TypeOf(target).Comparable(); the v1
// synthetic scope has no reflect, so comparability is probed: comparing an
// uncomparable value panics, and the probe converts that to false.
// (The probe itself is the only comparison that can panic: once
// targetComparable is true, err == target cannot panic — a panic needs
// identical uncomparable dynamic types on both sides.)
func errorsIsComparable(target error) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return target == target
}

func errorsIs(err, target error, targetComparable bool) bool {
	for {
		// NB: nil guard — a user Unwrap() []error may yield nil entries;
		// gc's assertion on them yields ok=false (→ false below), but the
		// P3 runtime NPEs asserting a nil interface. Behavior-preserving.
		if err == nil {
			return false
		}
		if targetComparable && err == target {
			return true
		}
		if x, ok := err.(errorIsser); ok && x.Is(target) {
			return true
		}
		switch x := err.(type) {
		case unwrapper:
			err = x.Unwrap()
			if err == nil {
				return false
			}
		case multiUnwrapper:
			for _, err := range x.Unwrap() {
				if errorsIs(err, target, targetComparable) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}
}

// As finds the first error in err's tree that matches target, and if one is found, sets
// target to that error value and returns true. Otherwise, it returns false.
//
// For most uses, prefer [AsType]. As is equivalent to [AsType] but sets its target
// argument rather than returning the matching error and doesn't require its target
// argument to implement error.
//
// The tree consists of err itself, followed by the errors obtained by repeatedly
// calling its Unwrap() error or Unwrap() []error method. When err wraps multiple
// errors, As examines err followed by a depth-first traversal of its children.
//
// An error matches target if the error's concrete value is assignable to the value
// pointed to by target, or if the error has a method As(any) bool such that
// As(target) returns true. In the latter case, the As method is responsible for
// setting target.
//
// As panics if target is not a non-nil pointer to either a type that implements
// error, or to any interface type.
//
// M7 scope: exact gc 1.27.1 signature, fail-fast body. A real
// implementation needs reflect (arbitrary *T target validation and
// setting) — unavailable to v1 synthetic packages: the synthetic
// reflect package's Implements/AssignableTo/Set are still fail-fast
// intrinsics. Re-test when the reflect runtime lands.
func As(err error, target any) bool {
	panic("errors.As: not in M7 scope (needs reflect; re-test when reflect lands)")
}

// errorAsser is the As(any) bool method set. gc asserts against the
// anonymous interface{ As(any) bool }; named here — identical semantics,
// and the v1 exporter handles named interface assertions.
type errorAsser interface {
	As(any) bool
}

// AsType finds the first error in err's tree that matches the type E, and
// if one is found, returns that error value and true. Otherwise, it
// returns the zero value of E and false.
//
// The tree consists of err itself, followed by the errors obtained by
// repeatedly calling its Unwrap() error or Unwrap() []error method. When
// err wraps multiple errors, AsType examines err followed by a
// depth-first traversal of its children.
//
// An error err matches the type E if the type assertion err.(E) holds,
// or if the error has a method As(any) bool such that err.As(target)
// returns true when target is a non-nil *E. In the latter case, the As
// method is responsible for setting target.
//
// P4 (was M7 scope): real implementation — unlike As it needs no
// reflect, only generic instantiation plus the type assertion err.(E).
// Faithful port of gc 1.27.1's asType, using the named
// unwrapper/multiUnwrapper interfaces above. NB: the loop-top
// assertions run only on non-nil err (nil guarded on entry, Unwrap-nil
// returns immediately, Join children skip nils) — matching gc, and
// avoiding the P3 runtime's nil-interface assertion NPE.
func AsType[E error](err error) (E, bool) {
	if err == nil {
		var zero E
		return zero, false
	}
	var pe *E // lazily initialized
	return asType(err, &pe)
}

func asType[E error](err error, ppe **E) (_ E, _ bool) {
	for {
		if e, ok := err.(E); ok {
			return e, true
		}
		if x, ok := err.(errorAsser); ok {
			if *ppe == nil {
				*ppe = new(E)
			}
			if x.As(*ppe) {
				return **ppe, true
			}
		}
		switch x := err.(type) {
		case unwrapper:
			err = x.Unwrap()
			if err == nil {
				return
			}
		case multiUnwrapper:
			for _, err := range x.Unwrap() {
				if err == nil {
					continue
				}
				if x, ok := asType(err, ppe); ok {
					return x, true
				}
			}
			return
		default:
			return
		}
	}
}

// Join returns an error that wraps the given errors.
// Any nil error values are discarded.
// Join returns nil if every value in errs is nil.
// The error formats as the concatenation of the strings obtained
// by calling the Error method of each element of errs, with a newline
// between each string.
//
// A non-nil error returned by Join implements the Unwrap() []error method.
// The errors may be inspected with Is and As.
func Join(errs ...error) error {
	n := 0
	for _, err := range errs {
		if err != nil {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	e := &joinError{
		errs: make([]error, 0, n),
	}
	for _, err := range errs {
		if err != nil {
			e.errs = append(e.errs, err)
		}
	}
	return e
}

type joinError struct {
	errs []error
}

func (e *joinError) Error() string {
	// Since Join returns nil if every value in errs is nil,
	// e.errs cannot be empty.
	if len(e.errs) == 1 {
		return e.errs[0].Error()
	}
	b := []byte(e.errs[0].Error())
	for _, err := range e.errs[1:] {
		b = append(b, '\n')
		b = append(b, err.Error()...)
	}
	// At this point, b has at least one byte '\n'.
	return string(b)
}

func (e *joinError) Unwrap() []error {
	return e.errs
}

// asTypePair is the pair-returning form of AsType for the WS D desugar
// (m7MultiResultFuncs): fields in result order (v, ok). v1 cannot emit
// multi-result funcdecls, so user call sites errors.AsType[T](err)
// desugar to the __asTypePub[T] helper below via the table; the pair
// struct is instantiated with the call's type argument.
type asTypePair[E error] struct {
	v  E
	ok bool
}

// __asTypePub is the WS D __-helper behind errors.AsType: the real
// implementation in single-result form. It mirrors AsType above
// structurally (the public multi-result body is skipped at export, so
// the logic cannot be shared by delegation — a call to AsType from
// here would desugar back into __asTypePub and recurse forever).
func __asTypePub[E error](err error) asTypePair[E] {
	if err == nil {
		return asTypePair[E]{}
	}
	var pe *E // lazily initialized
	for {
		if e, ok := err.(E); ok {
			return asTypePair[E]{v: e, ok: true}
		}
		if x, ok := err.(errorAsser); ok {
			if pe == nil {
				pe = new(E)
			}
			if x.As(pe) {
				return asTypePair[E]{v: *pe, ok: true}
			}
		}
		switch x := err.(type) {
		case unwrapper:
			err = x.Unwrap()
			if err == nil {
				return asTypePair[E]{}
			}
		case multiUnwrapper:
			for _, err := range x.Unwrap() {
				if err == nil {
					continue
				}
				if p := __asTypePub[E](err); p.ok {
					return p
				}
			}
			return asTypePair[E]{}
		default:
			return asTypePair[E]{}
		}
	}
}
`
