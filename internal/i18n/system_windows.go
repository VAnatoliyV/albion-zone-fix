//go:build windows

package i18n

import "golang.org/x/sys/windows"

// System — язык интерфейса Windows ("ru-RU", "en-US"…); пусто, если не узнали.
func System() string {
	l, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil || len(l) == 0 {
		return ""
	}
	return l[0]
}
