package sandbox

import "errors"

// WrapFunc returns a wrapper that applies p to an argv, or nil when the scope
// does not confine. A missing helper or unsupported platform degrades to the
// bare argv (best-effort hardening); any other failure means the scope cannot
// be applied, so the wrapper returns a refusing argv instead of running bare.
func WrapFunc(p Policy) func(argv []string) []string {
	if !p.Scope.Confines() {
		return nil
	}
	return func(argv []string) []string {
		wrapped, err := Wrap(p, argv)
		switch {
		case err == nil:
			return wrapped
		case errors.Is(err, ErrHelperMissing), errors.Is(err, ErrUnsupported):
			return argv
		default:
			return RefuseCmd(err.Error())
		}
	}
}
