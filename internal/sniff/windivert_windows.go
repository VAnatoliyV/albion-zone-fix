//go:build windows

package sniff

import (
	"errors"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"albionzonefix/internal/game"
)

// Флаги и слой WinDivert 2.x.
const (
	layerNetwork = 0
	flagSniff    = 0x0001 // копия пакета, сам пакет идёт дальше без задержки
	flagRecvOnly = 0x0004
	shutdownBoth = 3
	maxPacket    = 0xFFFF
	albionFilter = "udp and (udp.SrcPort == 5055 or udp.SrcPort == 5056 or udp.DstPort == 5055 or udp.DstPort == 5056)"
)

// address повторяет WINDIVERT_ADDRESS (80 байт): время, битовые поля, резерв, объединение.
type address struct {
	Timestamp int64
	Bits      uint32 // Layer:8 Event:8 Sniffed:1 Outbound:1 Loopback:1 Impostor:1 IPv6:1 ...
	Reserved2 uint32
	Union     [64]byte
}

type Divert struct {
	dll                     *windows.LazyDLL
	open, recv, close, shut *windows.LazyProc
	h                       windows.Handle
	once                    sync.Once
}

// Open открывает драйвер на пакеты Albion. binDir — папка с WinDivert.dll и WinDivert64.sys.
func Open(binDir string) (*Divert, error) { return OpenFilter(binDir, albionFilter) }

// OpenFilter — то же с любым фильтром WinDivert (только наблюдение).
func OpenFilter(binDir, filter string) (*Divert, error) {
	dll := windows.NewLazyDLL(filepath.Join(binDir, "WinDivert.dll"))
	if err := dll.Load(); err != nil {
		return nil, errors.New("нет WinDivert.dll в " + binDir + ": " + err.Error())
	}
	d := &Divert{dll: dll, open: dll.NewProc("WinDivertOpen"), recv: dll.NewProc("WinDivertRecv"),
		close: dll.NewProc("WinDivertClose"), shut: dll.NewProc("WinDivertShutdown")}
	f, _ := windows.BytePtrFromString(filter)
	h, _, err := d.open.Call(uintptr(unsafe.Pointer(f)), layerNetwork, 0, flagSniff|flagRecvOnly)
	if windows.Handle(h) == windows.InvalidHandle {
		return nil, explain(err)
	}
	d.h = windows.Handle(h)
	return d, nil
}

func explain(err error) error {
	var en windows.Errno
	if errors.As(err, &en) {
		switch en {
		case windows.ERROR_ACCESS_DENIED:
			return errors.New("драйверу нужны права администратора")
		case windows.ERROR_FILE_NOT_FOUND:
			return errors.New("не найден WinDivert64.sys рядом с WinDivert.dll")
		case 577: // ERROR_INVALID_IMAGE_HASH
			return errors.New("Windows не приняла подпись драйвера (антивирус или режим проверки подписей)")
		case 1275: // ERROR_DRIVER_BLOCKED
			return errors.New("драйвер заблокирован системой или антивирусом")
		}
	}
	return errors.New("драйвер WinDivert не открылся: " + err.Error())
}

// Run читает пакеты Albion, пока драйвер не закроют. Блокирующий.
func (d *Divert) Run(out chan<- game.Packet) error {
	return d.recvLoop(func(b []byte, outbound bool) {
		a, pl, ok := ParseIP(b, outbound)
		if !ok {
			return
		}
		cp := make([]byte, len(pl))
		copy(cp, pl)
		out <- game.Packet{T: time.Now(), Out: outbound, Addr: a, Payload: cp}
	})
}

// RunRaw отдаёт сырые IP-пакеты (для ICMP в трассировке).
func (d *Divert) RunRaw(each func([]byte)) error {
	return d.recvLoop(func(b []byte, _ bool) {
		cp := make([]byte, len(b))
		copy(cp, b)
		each(cp)
	})
}

func (d *Divert) recvLoop(each func(b []byte, outbound bool)) error {
	buf := make([]byte, maxPacket)
	var addr address
	for {
		var n uint32
		r, _, err := d.recv.Call(uintptr(d.h), uintptr(unsafe.Pointer(&buf[0])), maxPacket,
			uintptr(unsafe.Pointer(&n)), uintptr(unsafe.Pointer(&addr)))
		if r == 0 {
			if en, ok := err.(windows.Errno); ok && (en == windows.ERROR_NO_DATA || en == windows.ERROR_INVALID_HANDLE || en == windows.ERROR_OPERATION_ABORTED) {
				return nil // нас закрыли
			}
			return errors.New("чтение пакетов: " + err.Error())
		}
		each(buf[:n], addr.Bits>>17&1 == 1)
	}
}

func (d *Divert) Close() {
	d.once.Do(func() {
		d.shut.Call(uintptr(d.h), shutdownBoth)
		d.close.Call(uintptr(d.h))
	})
}
