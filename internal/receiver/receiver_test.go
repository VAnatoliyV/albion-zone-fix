package receiver

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUp(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if !Up(addr) {
		t.Fatal("порт слушают, а Up=false")
	}
	ln.Close()
	if Up(addr) {
		t.Fatal("порт закрыт, а Up=true")
	}
}

func TestSiteReady(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	if SiteReady(p) {
		t.Fatal("нет журнала")
	}
	os.WriteFile(p, []byte("запуск приёмника\nСайт готов\nзапуск приёмника\nкачаю\n"), 0644)
	if SiteReady(p) {
		t.Fatal("после перезапуска приёмника сайт ещё не готов")
	}
	os.WriteFile(p, []byte(strings.Repeat("x", 40000)+"запуск приёмника\nСайт готов\n"), 0644)
	if !SiteReady(p) {
		t.Fatal("готов")
	}
}
