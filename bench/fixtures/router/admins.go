package router

// admin handlers mirror the user handlers and are already correct.
// they are decoys: do not modify them.

func handleAdminGet(id string, known map[string]bool) int {
	if id == "" {
		return StatusBadRequest
	}
	if known[id] {
		return StatusOK
	}
	return StatusNotFound
}

func handleAdminDelete(id string, known map[string]bool) int {
	if id == "" {
		return StatusBadRequest
	}
	if !known[id] {
		return StatusNotFound
	}
	delete(known, id)
	return StatusOK
}

func handleAdminList(known map[string]bool) int {
	return StatusOK
}
