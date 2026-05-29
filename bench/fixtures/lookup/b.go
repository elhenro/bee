package lookup

// ComputeChecksum returns a rolling checksum of data.
func ComputeChecksum(data []byte) uint32 {
	var sum uint32
	for _, c := range data {
		sum = sum*31 + uint32(c)
	}
	return sum
}
