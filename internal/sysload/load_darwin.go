package sysload

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// loadAvg1 decodes struct loadavg { fixpt_t ldavg[3]; long fscale; }.
func loadAvg1() (float64, bool) {
	b, err := unix.SysctlRaw("vm.loadavg")
	if err != nil || len(b) < 24 {
		return 0, false
	}
	ld := binary.LittleEndian.Uint32(b[0:4])
	// fscale is a long, 8-byte aligned after the three uint32 fields.
	scale := binary.LittleEndian.Uint64(b[16:24])
	if scale == 0 {
		return 0, false
	}
	return float64(ld) / float64(scale), true
}
