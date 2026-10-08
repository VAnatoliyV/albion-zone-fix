//go:build !windows

package autostart

// Installed — вне Windows задачи нет.
func Installed() (string, bool) { return "", false }

// Sync — вне Windows (разработка на маке) автозапуска нет: выключить можно,
// включить — нет.
func Sync(on bool, tmpDir string) error {
	if on {
		return ErrUnsupported
	}
	return nil
}
