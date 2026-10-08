//go:build !windows

package support

import "runtime"

// OSVersion — на маке и в Linux (разработка) только имя системы.
func OSVersion() string { return runtime.GOOS + "/" + runtime.GOARCH }
