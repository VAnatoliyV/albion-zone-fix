//go:build windows

package update

import (
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// createBreakawayFromJob — процесс установки не должен умереть вместе с
// заданием, в котором, возможно, живёт программа (Планировщик).
const createBreakawayFromJob = 0x01000000

// WaitExit ждёт завершения процесса не дольше d.
func WaitExit(pid int, d time.Duration) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true // процесса уже нет
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, uint32(d.Milliseconds()))
	return err == nil && ev == windows.WAIT_OBJECT_0
}

// Unblock удаляет поток Zone.Identifier (Mark-of-the-Web): без него
// SmartScreen не спрашивает «запустить файл из интернета?».
func Unblock(path string) {
	if p, err := windows.UTF16PtrFromString(path + ":Zone.Identifier"); err == nil {
		windows.DeleteFile(p)
	}
}

// StartDetached запускает процесс отдельно от программы: своя группа, без
// консоли, по возможности вне задания. Права наследуются (программа —
// администратор, значит и установщик тоже, без второго окна UAC).
func StartDetached(exe string, args ...string) error {
	flags := uint32(windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP)
	try := func(f uint32) error {
		cmd := exec.Command(exe, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: f}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
	// Задание без права выхода из него отвечает «отказано» — тогда без флага.
	if err := try(flags | createBreakawayFromJob); err == nil {
		return nil
	}
	return try(flags)
}
