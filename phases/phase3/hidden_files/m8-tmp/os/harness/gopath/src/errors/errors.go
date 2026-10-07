package errors

func New(text string) error { return &errorString{text} }
type errorString struct{ s string }
func (e *errorString) Error() string { return e.s }
func Is(err, target error) bool {
	if err == target { return true }
	type iser interface{ Is(error) bool }
	if x, ok := err.(iser); ok { return x.Is(target) }
	if x, ok := err.(interface{ Unwrap() error }); ok { return Is(x.Unwrap(), target) }
	return false
}
func As(err error, target any) bool { return false }
