//go:build !windows

package gameguard

import "time"

// Вне Windows (разработка на маке) сторожа нет.

// Acquire — сторожа вне Windows не бывает.
func Acquire() (Guard, bool) { return nil, false }

// IsRunning — сторожа вне Windows не бывает.
func IsRunning() bool { return false }

// StopRunning — закрывать нечего.
func StopRunning(wait time.Duration) (was, ok bool) { return false, true }
