//go:build !windows

package hotkey

// Hook — на маке хуков нет.
type Hook struct{}

// Start — на маке ничего не ловит.
func Start(k Key, fire func(), logf func(string, ...any)) (*Hook, error) { return &Hook{}, nil }

// Stop — нечего снимать.
func (h *Hook) Stop() {}
