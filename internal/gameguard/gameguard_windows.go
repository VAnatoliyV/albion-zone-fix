//go:build windows

package gameguard

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Знак «сторож запущен», просьба закрыться и номер процесса сторожа — в
// пространстве сеанса (Local): сторож, программа и установщик работают в
// одном сеансе пользователя.
const (
	mutexName = `Local\AlbionJournal.Watch`
	eventName = `Local\AlbionJournal.WatchStop`
	pidName   = `Local\AlbionJournal.WatchPID` // 4 байта: pid сторожа
)

type guard struct{ mutex, event, pidMap windows.Handle }

// Wait спит на событии «закрыться»: процессор не тратится. Сбой ожидания
// (не должен случаться) — просто спим, чтобы не крутить снимки впустую.
func (g *guard) Wait(d time.Duration) bool {
	ev, err := windows.WaitForSingleObject(g.event, uint32(d.Milliseconds()))
	if err != nil || ev == windows.WAIT_FAILED {
		time.Sleep(d)
		return false
	}
	return ev == windows.WAIT_OBJECT_0
}

func (g *guard) Release() {
	for _, h := range []*windows.Handle{&g.mutex, &g.pidMap, &g.event} {
		if *h != 0 {
			windows.CloseHandle(*h)
			*h = 0
		}
	}
}

// Acquire — знак «сторож запущен»; false — уже есть другой. Событие
// «закрыться» и номер процесса создаются ДО знака: кто видит знак, тот
// может и попросить, и дождаться выхода процесса. Событие не сбрасываем:
// оно живо, только пока кто-то просит закрыться, — просьба относится и к
// новому сторожу.
func Acquire() (Guard, bool) {
	g := &guard{}
	ev, _ := windows.UTF16PtrFromString(eventName)
	e, _ := windows.CreateEvent(nil, 1, 0, ev) // ручной сброс: просьба не теряется
	if e == 0 {
		return nil, false
	}
	g.event = e
	pn, _ := windows.UTF16PtrFromString(pidName)
	pm, _ := windows.CreateFileMapping(windows.InvalidHandle, nil, windows.PAGE_READWRITE, 0, 4, pn)
	if pm != 0 {
		g.pidMap = pm
	}
	name, _ := windows.UTF16PtrFromString(mutexName)
	m, err := windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS || err == windows.ERROR_ACCESS_DENIED || m == 0 {
		if m != 0 {
			windows.CloseHandle(m)
		}
		g.Release()
		return nil, false
	}
	g.mutex = m
	if g.pidMap != 0 {
		writePID(g.pidMap, windows.GetCurrentProcessId())
	}
	return g, true
}

func writePID(h windows.Handle, pid uint32) {
	addr, err := windows.MapViewOfFile(h, windows.FILE_MAP_WRITE, 0, 0, 4)
	if err != nil {
		return
	}
	*at(addr) = pid
	windows.UnmapViewOfFile(addr)
}

// at — uint32 по адресу отображения (адрес — от MapViewOfFile, не из кучи Go).
func at(addr uintptr) *uint32 {
	var p *uint32
	*(*uintptr)(unsafe.Pointer(&p)) = addr
	return p
}

// watcherPID — номер процесса сторожа; 0 — ещё не записан или сторожа нет.
func watcherPID() uint32 {
	pn, _ := windows.UTF16PtrFromString(pidName)
	h, err := windows.CreateFileMapping(windows.InvalidHandle, nil, windows.PAGE_READWRITE, 0, 4, pn)
	if h == 0 {
		return 0
	}
	defer windows.CloseHandle(h)
	if err != windows.ERROR_ALREADY_EXISTS {
		return 0 // создали сами — сторожа нет
	}
	addr, err := windows.MapViewOfFile(h, windows.FILE_MAP_READ, 0, 0, 4)
	if err != nil {
		return 0
	}
	defer windows.UnmapViewOfFile(addr)
	return *at(addr)
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

// StopRunning просит сторожа закрыться и ждёт до wait выхода его процесса
// (не только снятия знака: exe отпускается, когда процесс завершён).
// was — сторож был; ok — его больше нет.
func StopRunning(wait time.Duration) (was, ok bool) {
	var ev, proc windows.Handle
	defer func() {
		if ev != 0 {
			windows.CloseHandle(ev)
		}
		if proc != 0 {
			windows.CloseHandle(proc)
		}
	}()
	return stop(stopOps{
		running: IsRunning,
		signal: func() bool {
			name, _ := windows.UTF16PtrFromString(eventName)
			e, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
			if err != nil {
				return false
			}
			ev = e // держим до конца ожидания: просьба не пропадёт
			return windows.SetEvent(e) == nil
		},
		open: func() (func(time.Duration) bool, bool) {
			pid := watcherPID()
			if pid == 0 {
				return nil, false
			}
			h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
			if err != nil {
				return nil, false
			}
			proc = h
			return func(d time.Duration) bool {
				r, err := windows.WaitForSingleObject(h, uint32(d.Milliseconds()))
				return err == nil && r == windows.WAIT_OBJECT_0
			}, true
		},
		sleep: time.Sleep,
		now:   time.Now,
	}, wait)
}
