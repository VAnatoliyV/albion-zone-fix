//go:build !windows

package gamewatch

// Running — на маке (разработка) за игрой не следим.
func Running() (bool, error) { return false, ErrUnsupported }
