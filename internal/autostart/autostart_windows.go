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
	exe, _, ok = Current()
	return exe, ok
}

// Current — есть ли задача, на какую программу и что запускает.
func Current() (exe string, mode Mode, ok bool) {
	out, err := schtasks(QueryArgs()...)
	if err != nil {
		return "", Off, false
	}
	return CommandOf(out), ModeOf(ArgsOf(out)), true
}

// Run запускает задачу сейчас (её права — наивысшие доступные).
func Run() error {
	_, err := schtasks(RunArgs()...)
	return err
}

// Enable создаёт (или пересоздаёт) задачу на текущую программу. XML
// кладём в свежую временную папку и сразу удаляем: в папке данных ему
// делать нечего.
func Enable(m Mode) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return err
	}
	return create(Task{Exe: exe, Dir: filepath.Dir(exe), UserID: u.Username, Args: m.Args()})
}

// Retarget переносит существующую задачу на другой exe (установщик убрал
// старую копию, на которую она указывала) с тем же пользователем и теми
// же аргументами (программа или сторож).
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
	return create(Task{Exe: exe, Dir: filepath.Dir(exe), UserID: uid, Args: ModeOf(ArgsOf(out)).Args()})
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

// Sync приводит задачу к настройке: Off — задачи нет; иначе задача есть,
// указывает на этот exe (папку с программой могли перенести) и запускает
// то, что нужно (программу или сторожа игры).
func Sync(m Mode) error {
	if m == Off {
		return Disable()
	}
	exe, _ := os.Executable()
	if cur, mode, ok := Current(); ok && strings.EqualFold(cur, exe) && mode == m {
		return nil
	}
	return Enable(m)
}
