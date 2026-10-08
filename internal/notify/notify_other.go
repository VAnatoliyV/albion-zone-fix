//go:build !windows

package notify

import "os"

// Init — на маке регистрировать нечего.
func Init(dataDir string, icon []byte, logf func(string, ...any)) {}

// Show — на маке уведомление просто в консоль (режим разработки).
func Show(title, subtitle, body string) {
	os.Stderr.WriteString("уведомление: " + title + " | " + subtitle + " | " + body + "\n")
}
