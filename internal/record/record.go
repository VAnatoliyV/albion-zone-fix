// Пакет record пишет и читает запись пакетов игры (.azf), которую тестер
// присылает для разбора. Формат простой: заголовок «AZF1», затем записи
// [время unix-нано int64][направление 1 байт][длина адреса 1][адрес][длина 4][нагрузка].
package record

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"time"

	"albionzonefix/internal/game"
)

var magic = []byte("AZF1")

type Writer struct{ w *bufio.Writer }

func NewWriter(w io.Writer) (*Writer, error) {
	bw := bufio.NewWriter(w)
	if _, err := bw.Write(magic); err != nil {
		return nil, err
	}
	return &Writer{w: bw}, nil
}

func (w *Writer) Write(p game.Packet) error {
	var hdr [10]byte
	binary.LittleEndian.PutUint64(hdr[0:], uint64(p.T.UnixNano()))
	if p.Out {
		hdr[8] = 1
	}
	hdr[9] = byte(len(p.Addr))
	w.w.Write(hdr[:])
	w.w.WriteString(p.Addr)
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], uint32(len(p.Payload)))
	w.w.Write(n[:])
	_, err := w.w.Write(p.Payload)
	return err
}

func (w *Writer) Flush() error { return w.w.Flush() }

func Read(r io.Reader, each func(game.Packet)) error {
	br := bufio.NewReader(r)
	head := make([]byte, 4)
	if _, err := io.ReadFull(br, head); err != nil || string(head) != string(magic) {
		return errors.New("это не запись Albion Zone Fix")
	}
	for {
		var hdr [10]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		addr := make([]byte, hdr[9])
		if _, err := io.ReadFull(br, addr); err != nil {
			return err
		}
		var n [4]byte
		if _, err := io.ReadFull(br, n[:]); err != nil {
			return err
		}
		size := binary.LittleEndian.Uint32(n[:])
		if size > 1<<20 {
			return errors.New("запись повреждена")
		}
		pl := make([]byte, size)
		if _, err := io.ReadFull(br, pl); err != nil {
			return err
		}
		each(game.Packet{T: time.Unix(0, int64(binary.LittleEndian.Uint64(hdr[0:]))), Out: hdr[8] == 1, Addr: string(addr), Payload: pl})
	}
}
