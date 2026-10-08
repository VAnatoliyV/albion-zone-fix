//go:build !windows

package sniff

import (
	"errors"

	"albionzonefix/internal/game"
)

type Divert struct{}

func Open(string) (*Divert, error) {
	return nil, errors.New("перехват пакетов работает только в Windows")
}
func (*Divert) Run(chan<- game.Packet, func([]byte)) error { return nil }
func (*Divert) Close()                                     {}
