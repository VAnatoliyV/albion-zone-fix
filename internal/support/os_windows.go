//go:build windows

package support

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// OSVersion — «Windows 10.0.26100» (Windows 11 — сборка 22000 и выше).
func OSVersion() string {
	v := windows.RtlGetVersion()
	name := "Windows"
	if v.MajorVersion == 10 && v.BuildNumber >= 22000 {
		name = "Windows 11"
	} else if v.MajorVersion == 10 {
		name = "Windows 10"
	}
	return fmt.Sprintf("%s (%d.%d.%d)", name, v.MajorVersion, v.MinorVersion, v.BuildNumber)
}
