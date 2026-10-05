//go:build windows

package main

import (
	"bufio"
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

func openBrowser(url string) {
	verb, _ := syscall.UTF16PtrFromString("open")
	u, _ := syscall.UTF16PtrFromString(url)
	windows.ShellExecute(0, verb, u, nil, nil, windows.SW_SHOWNORMAL)
}

func waitEnter() {
	os.Stdout.WriteString("Нажмите Enter, чтобы закрыть.\n")
	bufio.NewReader(os.Stdin).ReadString('\n')
}
