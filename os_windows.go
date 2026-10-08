//go:build windows

package main

import (
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func isAdmin() bool { return windows.GetCurrentProcessToken().IsElevated() }

func relaunchAsAdmin() error {
	exe, _ := os.Executable()
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(exe)
	args, _ := syscall.UTF16PtrFromString(strings.Join(os.Args[1:], " "))
	cwd, _ := os.Getwd()
	dir, _ := syscall.UTF16PtrFromString(cwd)
	return windows.ShellExecute(0, verb, file, args, dir, windows.SW_NORMAL)
}
