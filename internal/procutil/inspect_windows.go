//go:build windows

package procutil

import "golang.org/x/sys/windows"

// Inspect: путь к exe и время создания (FILETIME) процесса по pid.
// ok=false — процесса нет или к нему нет доступа.
func Inspect(pid int) (image string, created int64, ok bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", 0, false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "", 0, false
	}
	var c, e, k, u windows.Filetime
	if err := windows.GetProcessTimes(h, &c, &e, &k, &u); err != nil {
		return "", 0, false
	}
	return windows.UTF16ToString(buf[:n]), c.Nanoseconds(), true
}
