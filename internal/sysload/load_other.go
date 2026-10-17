//go:build !darwin && !linux

package sysload

func loadAvg1() (float64, bool) { return 0, false }
