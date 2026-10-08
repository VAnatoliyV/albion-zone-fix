//go:build !windows

package procutil

// Inspect на не-Windows не знает процессов: ok=false (убивать по pid нельзя).
func Inspect(pid int) (image string, created int64, ok bool) { return "", 0, false }
