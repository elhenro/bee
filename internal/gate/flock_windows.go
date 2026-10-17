package gate

import "os"

// no flock on windows: gates run unserialized there.
func tryFlock(*os.File) (bool, error) { return true, nil }

func unflock(*os.File) {}
