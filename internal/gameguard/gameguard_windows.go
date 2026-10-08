//go:build windows

package gameguard

import (
	"time"

	"golang.org/x/sys/windows"
)

// Знак «сторож запущен» и просьба закрыться — в пространстве сеанса (Local):
// сторож, программа и установщик работают в одном сеансе пользователя.
const (
	mutexName = `Local\AlbionJournal.Watch`
	eventName = `Local\AlbionJournal.WatchStop`
)

type guard struct{ mutex, event windows.Handle }

// Wait спит на событии «закрыться»: процессор не тратится.
func (g *guard) Wait(d time.Duration) bool {
	ev, err := windows.WaitForSingleObject(g.event, uint32(d.Milliseconds()))
	return err == nil && ev == windows.WAIT_OBJECT_0
}

func (g *guard) Release() {
	if g.mutex != 0 {
		windows.CloseHandle(g.mutex)
		g.mutex = 0
	}
}

// Acquire — знак «сторож запущен»; false — уже есть другой. Событие
// «закрыться» сторож держит до выхода: просьба после снятия знака (пока
// поднимается программа) ничего не ломает.
func Acquire() (Guard, bool) {
	name, _ := windows.UTF16PtrFromString(mutexName)
	m, err := windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS || err == windows.ERROR_ACCESS_DENIED || m == 0 {
		if m != 0 {
			windows.CloseHandle(m)
		}
		return nil, false
	}
	ev, _ := windows.UTF16PtrFromString(eventName)
	e, err := windows.CreateEvent(nil, 1, 0, ev) // ручной сброс: просьба не теряется
	if err == windows.ERROR_ALREADY_EXISTS && e != 0 {
		// Осталось от прошлой просьбы (её автор ещё держит событие) — сбросить.
		windows.ResetEvent(e)
	} else if e == 0 {
		windows.CloseHandle(m)
		return nil, false
	}
	return &guard{mutex: m, event: e}, true
}

// IsRunning — работает ли сторож.
func IsRunning() bool {
	name, _ := windows.UTF16PtrFromString(mutexName)
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name)
	if err == nil {
		windows.CloseHandle(h)
		return true
	}
	return err == windows.ERROR_ACCESS_DENIED
}

// StopRunning просит сторожа закрыться и ждёт до wait, пока знак не
// пропадёт. was — сторож был; ok — его больше нет.
func StopRunning(wait time.Duration) (was, ok bool) {
	if !IsRunning() {
		return false, true
	}
	ev, _ := windows.UTF16PtrFromString(eventName)
	if e, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, ev); err == nil {
		windows.SetEvent(e)
		defer windows.CloseHandle(e)
	}
	deadline := time.Now().Add(wait)
	for IsRunning() {
		if time.Now().After(deadline) {
			return true, false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true, true
}
