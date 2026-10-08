// Пакет receiver — свой приёмник цен (acp-prices) глазами окна: слушает ли
// он порт и готов ли сайт. Запуск и остановка — Manager.
package receiver

import (
	"bytes"
	"net"
	"os"
	"time"
)

// Addr — порт приёмника, тот же, что у мака.
const Addr = "127.0.0.1:7777"

// SiteURL — сайт приёмника со своими ценами.
const SiteURL = "http://localhost:7777/"

// Up — слушает ли кто-то порт приёмника. Петля отвечает сразу.
func Up(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// SiteReady — приёмник печатает «Сайт готов», когда страница и цены

// журнала после последней строки «запуск приёмника» (как мак).
func SiteReady(logPath string) bool {
	f, err := os.Open(logPath)
	if err != nil {
		return false
	}
	defer f.Close()
	const tail = 32768
	if fi, err := f.Stat(); err == nil && fi.Size() > tail {
		f.Seek(fi.Size()-tail, 0)
	}
	b := make([]byte, tail)
	n, _ := f.Read(b)
	b = b[:n]
	if i := bytes.LastIndex(b, []byte("запуск приёмника")); i >= 0 {
		b = b[i:]
	}
	return bytes.Contains(b, []byte("Сайт готов"))
}
