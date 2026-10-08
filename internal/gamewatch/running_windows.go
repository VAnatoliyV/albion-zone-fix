//go:build windows

package gamewatch

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Running — есть ли среди процессов клиент игры (снимок toolhelp32).
func Running() (bool, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if IsGame(windows.UTF16ToString(e.ExeFile[:])) {
			return true, nil
		}
	}
	if err == windows.ERROR_NO_MORE_FILES {
		return false, nil
	}
	return false, err
}
