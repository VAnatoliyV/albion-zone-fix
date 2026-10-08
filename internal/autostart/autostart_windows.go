//go:build windows

package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func schtasks(args ...string) (string, error) {
	cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "schtasks.exe"), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("schtasks %s: %v: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// Installed — есть ли задача и на какую программу она указывает.
func Installed() (exe string, ok bool) {
	out, err := schtasks(QueryArgs()...)
	if err != nil {
		return "", false
	}
	return CommandOf(out), true
}

// Enable создаёт (или пересоздаёт) задачу на текущую программу.
// tmpDir — куда положить XML на время создания.
func Enable(tmpDir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return err
	}
	t := Task{Exe: exe, Dir: filepath.Dir(exe), UserID: u.Username}
	path := filepath.Join(tmpDir, "autostart-task.xml")
	if err := os.WriteFile(path, UTF16(t.XML()), 0600); err != nil {
		return err
	}
	defer os.Remove(path)
	_, err = schtasks(CreateArgs(path)...)
	return err
}

// Disable убирает задачу; если её нет — не ошибка.
func Disable() error {
	if _, ok := Installed(); !ok {
		return nil
	}
	_, err := schtasks(DeleteArgs()...)
	return err
}

// Sync приводит задачу к настройке: включено — задача есть и указывает на
// этот exe (папку с программой могли перенести); выключено — задачи нет.
func Sync(on bool, tmpDir string) error {
	if !on {
		return Disable()
	}
	exe, _ := os.Executable()
	if cur, ok := Installed(); ok && strings.EqualFold(cur, exe) {
		return nil
	}
	return Enable(tmpDir)
}
