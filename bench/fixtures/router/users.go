package router

// handleUserGet returns OK for a known id, NotFound otherwise.
func handleUserGet(id string, known map[string]bool) int {
	if id == "" {
		return StatusBadRequest
	}
	if known[id] {
		return StatusOK
	}
	return StatusNotFound
}

// handleUserCreate rejects an empty id, otherwise creates and returns OK.
func handleUserCreate(id string, known map[string]bool) int {
	if id == "" {
		return StatusBadRequest
	}
	known[id] = true
	return StatusOK
}

// handleUserUpdate rejects an empty id, NotFound for unknown, OK otherwise.
func handleUserUpdate(id string, known map[string]bool) int {
	if id == "" {
		return StatusBadRequest
	}
	if !known[id] {
		return StatusNotFound
	}
	return StatusOK
}

// handleUserDelete must reject an empty id with BadRequest, return NotFound
// for an unknown id, and OK when it deletes a known id.
func handleUserDelete(id string, known map[string]bool) int {
	if id == "" {
		return StatusNotFound
	}
	if !known[id] {
		return StatusNotFound
	}
	delete(known, id)
	return StatusOK
}
