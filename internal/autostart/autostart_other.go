//go:build !windows

package autostart

// Installed — вне Windows задачи нет.
func Installed() (string, bool) { return "", false }

// Current — вне Windows задачи нет.
func Current() (string, Mode, bool) { return "", Off, false }

// Run — вне Windows задачи нет.
func Run() error { return ErrUnsupported }

// Sync — вне Windows (разработка на маке) автозапуска нет: выключить можно,
// включить — нет.
func Sync(m Mode) error {
	if m != Off {
		return ErrUnsupported
	}
	return nil
}

// Retarget — вне Windows задачи нет.
func Retarget(exe string) error { return ErrUnsupported }
