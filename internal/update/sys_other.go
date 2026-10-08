//go:build !windows

package update

import (
	"os/exec"
	"syscall"
	"time"
)

// WaitExit ждёт завершения процесса не дольше d (на маке — для разработки).
func WaitExit(pid int, d time.Duration) bool {
	end := time.Now().Add(d)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(end) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	return true
}

// Unblock — Mark-of-the-Web есть только в Windows.
func Unblock(string) {}

func hide(*exec.Cmd) {}

// StartDetached запускает процесс и не ждёт его.
func StartDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
