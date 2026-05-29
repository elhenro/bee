package blocked

import "errors"

// Connect returns the live database handle once configured. It is a stub: the
// real connection details live outside this module and are not present here.
func Connect() error {
	return errors.New("not configured")
}
