//go:build !windows

package hotkey

import (
	"context"
	"time"
)

// Hook — на маке хуков нет.
type Hook struct{}

// Start — на маке ничего не ловит.
func Start(k Key, fire func(), logf func(string, ...any)) (*Hook, error) { return &Hook{}, nil }

// Stop — нечего снимать.
func (h *Hook) Stop() {}

// Record — на маке записывать нечем.
func Record(ctx context.Context, timeout time.Duration, hint func(string), logf func(string, ...any)) (Key, error) {
	return "", ErrUnsupported
}
