//go:build !windows

package main

import (
	"bufio"
	"os"
	"os/exec"
)

func isAdmin() bool          { return os.Geteuid() == 0 }
func relaunchAsAdmin() error { return os.ErrPermission }
func openBrowser(url string) { exec.Command("open", url).Start() }
func waitEnter()             { bufio.NewReader(os.Stdin).ReadString('\n') }
