//go:build !windows

// Пакет procutil — запуск дочерних процессов на Windows (на других системах пустышки).
package procutil

import "os/exec"

func Hide(*exec.Cmd)      {}
func Detach(*exec.Cmd)    {}
func BindToJob(*exec.Cmd) {}
