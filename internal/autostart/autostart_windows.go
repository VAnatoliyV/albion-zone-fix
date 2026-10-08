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

// Enable создаёт (или пересоздаёт) задачу на текущую программу. XML
// кладём в свежую временную папку и сразу удаляем: в папке данных ему
// делать нечего.
func Enable() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return err
	}
	return create(Task{Exe: exe, Dir: filepath.Dir(exe), UserID: u.Username})
}

// Retarget переносит существующую задачу на другой exe (установщик убрал
// старую копию, на которую она указывала) с тем же пользователем.
func Retarget(exe string) error {
	out, err := schtasks(QueryArgs()...)
	if err != nil {
		return err
	}
	uid := UserOf(out)
	if uid == "" {
		u, err := user.Current()
		if err != nil {
			return err
		}
		uid = u.Username
	}
	return create(Task{Exe: exe, Dir: filepath.Dir(exe), UserID: uid})
}

func create(t Task) error {
	tmp, err := os.MkdirTemp("", "albion-journal-task-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	path := filepath.Join(tmp, "task.xml")
	if err := os.WriteFile(path, UTF16(t.XML()), 0600); err != nil {
		return err
	}
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
func Sync(on bool) error {
	if !on {
		return Disable()
	}
	exe, _ := os.Executable()
	if cur, ok := Installed(); ok && strings.EqualFold(cur, exe) {
		return nil
	}
	return Enable()
}
