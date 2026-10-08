//go:build !windows

package i18n

import "os"

// System — язык из окружения (разработка на маке): LC_ALL, LC_MESSAGES, LANG.
func System() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" && v != "C" && v != "POSIX" {
			return v
		}
	}
	return ""
}
