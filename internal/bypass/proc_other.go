//go:build !windows

package bypass

import "os/exec"

func hideWindow(*exec.Cmd) {}
func bindToJob(*exec.Cmd)  {}
