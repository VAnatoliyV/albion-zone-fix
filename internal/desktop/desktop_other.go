//go:build !windows

package desktop

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// На маке окна нет: страницу открывает обычный браузер (режим разработки).
type nativeState struct{ stop chan struct{} }

// Run открывает страницу в браузере и ждёт Quit или Ctrl+C.
func (d *Desktop) Run() {
	d.mu.Lock()
	d.fallback = true
	if d.native.stop == nil {
		d.native.stop = make(chan struct{})
	}
	stop, quit := d.native.stop, d.quit
	d.mu.Unlock()
	if quit {
		return
	}
	if !d.cfg.Hidden {
		OpenURL(d.cfg.URL)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
	case <-stop:
	}
}

// Show — открыть страницу ещё раз.
func (d *Desktop) Show() { OpenURL(d.cfg.URL) }

// ShowQuiet — на маке (разработка) окна нет, браузер не открываем.
func (d *Desktop) ShowQuiet() {}

// Quit завершает Run.
func (d *Desktop) Quit() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.quit {
		return
	}
	d.quit = true
	if d.native.stop != nil {
		close(d.native.stop)
	}
}

// OpenMap — на маке карта открывается в браузере.
func (d *Desktop) OpenMap(url string) { OpenURL(url) }

// MapEval — окна карты на маке нет.
func (d *Desktop) MapEval(js string) {}

// Relabel — на маке трея нет.
func (d *Desktop) Relabel() {}

// OpenURL открывает адрес в браузере системы.
func OpenURL(url string) { exec.Command("open", url).Start() }

// OpenFolder открывает папку в Finder.
func OpenFolder(dir string) { exec.Command("open", dir).Start() }

// Message — на маке просто в консоль.
func Message(title, text string) { os.Stderr.WriteString(title + ": " + text + "\n") }

// OtherRunning — на маке копий не считаем.
func OtherRunning() bool { return false }

// SingleInstance — на маке копий не считаем.
func SingleInstance() bool { return true }
