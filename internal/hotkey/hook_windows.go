//go:build windows

package hotkey

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	pSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	pUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	pCallNextHookEx      = user32.NewProc("CallNextHookEx")
	pGetMessageW         = user32.NewProc("GetMessageW")
	pPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	pGetAsyncKeyState    = user32.NewProc("GetAsyncKeyState")
	pGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
)

const (
	whKeyboardLL = 13
	whMouseLL    = 14
	wmQuit       = 0x0012
	llkhfAltDown = 0x20
)

type msllHook struct {
	X, Y      int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	Extra     uintptr
}

type kbdllHook struct {
	VK, Scan, Flags, Time uint32
	Extra                 uintptr
}

// Hook — поставленный рабочий хук и его поток.
type Hook struct {
	thread uint32
	fire   chan struct{}
	done   chan struct{}
	once   sync.Once
}

// recEvent — решение по нажатию во время записи кнопки.
type recEvent struct {
	v Verdict
	k Key
}

// Обратные вызовы хуков — общие на процесс (syscall.NewCallback не
// освобождается), поэтому создаются один раз, а что ловить — в cur*/rec*.
// Пока идёт запись (recCh != nil), рабочая кнопка не срабатывает. Если
// стоят и рабочий хук, и хук записи одного вида, вызов придёт дважды на
// одно событие: запись берёт первое решение, лишнее отбрасывается.
var (
	cbOnce  sync.Once
	mouseCB uintptr
	keyCB   uintptr
	curMu   sync.Mutex
	curKey  Key
	curFire chan struct{}
	curRep  Repeat
	recCh   chan recEvent
	recMu   sync.Mutex // одна запись за раз
)

// modsNow — какие модификаторы сейчас зажаты (физически). Alt берём ещё и
// из флага события: у Alt+клавиши он надёжнее.
func modsNow(kbFlags uint32) uint8 {
	down := func(vk uintptr) bool {
		r, _, _ := pGetAsyncKeyState.Call(vk)
		return r&0x8000 != 0
	}
	var m uint8
	if down(0x11) {
		m |= ModCtrl
	}
	if down(0x12) || kbFlags&llkhfAltDown != 0 {
		m |= ModAlt
	}
	if down(0x10) {
		m |= ModShift
	}
	if down(0x5B) || down(0x5C) {
		m |= ModWin
	}
	return m
}

func send(ch chan struct{}) {
	select { // не ждём: хук должен вернуться сразу
	case ch <- struct{}{}:
	default:
	}
}

func sendRec(ch chan recEvent, e recEvent) {
	select {
	case ch <- e:
	default:
	}
}

func callbacks() {
	cbOnce.Do(func() {
		mouseCB = windows.NewCallback(func(code, wp uintptr, m *msllHook) uintptr {
			if int32(code) >= 0 {
				curMu.Lock()
				k, ch, rc := curKey, curFire, recCh
				curMu.Unlock()
				if rc != nil {
					if v, nk := DecideMouse(wp, m.MouseData, m.Flags); v != Ignore {
						sendRec(rc, recEvent{v, nk})
					}
				} else if ch != nil && MatchMouse(k, wp, m.MouseData, m.Flags) {
					send(ch)
				}
			}
			r, _, _ := pCallNextHookEx.Call(0, code, wp, uintptr(unsafe.Pointer(m)))
			return r
		})
		keyCB = windows.NewCallback(func(code, wp uintptr, kb *kbdllHook) uintptr {
			if int32(code) >= 0 {
				curMu.Lock()
				k, ch, rc := curKey, curFire, recCh
				fresh := true
				if isKeyDown(wp) {
					fresh = curRep.Down(kb.VK)
				} else if isKeyUp(wp) {
					curRep.Up(kb.VK)
				}
				curMu.Unlock()
				if rc != nil {
					if fresh {
						if v, nk := DecideKey(wp, kb.VK, kb.Flags, modsNow(kb.Flags)); v != Ignore {
							sendRec(rc, recEvent{v, nk})
						}
					}
				} else if ch != nil && fresh && isKeyDown(wp) && kb.VK == k.VK() &&
					MatchKey(k, wp, kb.VK, kb.Flags, modsNow(kb.Flags)) {
					send(ch)
				}
			}
			r, _, _ := pCallNextHookEx.Call(0, code, wp, uintptr(unsafe.Pointer(kb)))
			return r
		})
	})
}

// hookThread ставит хуки kinds на своём потоке с циклом сообщений; вернёт
// id потока (снять — PostThreadMessage WM_QUIT). Низкоуровневый хук
// работает, пока у потока крутится цикл сообщений.
func hookThread(kinds []uintptr, logf func(string, ...any), what string) (uint32, error) {
	callbacks()
	type res struct {
		thread uint32
		err    error
	}
	ready := make(chan res, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		th := windows.GetCurrentThreadId()
		mod, _, _ := pGetModuleHandleW.Call(0)
		var hs []uintptr
		for _, kind := range kinds {
			cb := mouseCB
			if kind == whKeyboardLL {
				cb = keyCB
			}
			hh, _, err := pSetWindowsHookExW.Call(kind, cb, mod, 0)
			if hh == 0 {
				for _, h := range hs {
					pUnhookWindowsHookEx.Call(h)
				}
				ready <- res{err: fmt.Errorf("SetWindowsHookEx: %v", err)}
				return
			}
			hs = append(hs, hh)
		}
		ready <- res{thread: th}
		var msg [48]byte
		for {
			r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
		}
		for _, h := range hs {
			pUnhookWindowsHookEx.Call(h)
		}
		logf("%s: хук снят", what)
	}()
	r := <-ready
	return r.thread, r.err
}

// Start ставит рабочий хук для кнопки k; fire зовётся в отдельной горутине
// (работа там может идти секундами — хук ждать не должен). Ставится только
// нужный хук: мышиный для кнопок мыши, клавиатурный — для клавиш
// (клавиатурный хук без нужды антивирусы не любят). Одновременно работает
// один Hook: прежний надо снять Stop.
func Start(k Key, fire func(), logf func(string, ...any)) (*Hook, error) {
	c, ok := Parse(string(k))
	if !ok {
		return nil, nil
	}
	ch := make(chan struct{}, 1)
	curMu.Lock()
	curKey, curFire, curRep = c.String(), ch, Repeat{}
	curMu.Unlock()
	h := &Hook{fire: ch, done: make(chan struct{})}
	go func() {
		for {
			select {
			case <-ch:
				fire()
			case <-h.done:
				return
			}
		}
	}()
	kind := uintptr(whMouseLL)
	if c.Mouse == 0 {
		kind = whKeyboardLL
	}
	th, err := hookThread([]uintptr{kind}, logf, "кнопка карточки")
	if err != nil {
		h.Stop()
		return nil, err
	}
	h.thread = th
	logf("кнопка карточки: жду %s", c.String().Label())
	return h, nil
}

// Stop снимает хук и останавливает его поток.
func (h *Hook) Stop() {
	if h == nil {
		return
	}
	h.once.Do(func() {
		close(h.done)
		curMu.Lock()
		if curFire == h.fire { // новый хук мог уже поставить свой
			curFire = nil
		}
		curMu.Unlock()
		if h.thread != 0 {
			pPostThreadMessageW.Call(uintptr(h.thread), wmQuit, 0, 0)
		}
	})
}

// Record ждёт следующее подходящее нажатие (мышь — средняя или боковая,
// клавиатура — с модификатором или F1–F24 и пр., см. DecideKey) и
// возвращает кнопку. Ловит и не из фокуса окна, и XBUTTON, который
// WebView2 съел бы как «назад». hint зовётся на нажатие, которое не
// годится («needMod»). ErrCancel — Esc или ctx отменён, ErrTimeout — время
// вышло. Рабочая кнопка на время записи молчит.
func Record(ctx context.Context, timeout time.Duration, hint func(string), logf func(string, ...any)) (Key, error) {
	if !recMu.TryLock() {
		return "", ErrBusy
	}
	defer recMu.Unlock()
	rc := make(chan recEvent, 4)
	curMu.Lock()
	recCh = rc
	curMu.Unlock()
	defer func() {
		curMu.Lock()
		recCh = nil
		curMu.Unlock()
	}()
	th, err := hookThread([]uintptr{whMouseLL, whKeyboardLL}, logf, "запись кнопки")
	if err != nil {
		return "", err
	}
	defer pPostThreadMessageW.Call(uintptr(th), wmQuit, 0, 0)
	logf("запись кнопки: жду нажатие")
	t := time.NewTimer(timeout)
	defer t.Stop()
	for {
		select {
		case e := <-rc:
			switch e.v {
			case Accept:
				logf("запись кнопки: %s", e.k.Label())
				return e.k, nil
			case Cancel:
				return "", ErrCancel
			case NeedMod:
				if hint != nil {
					hint("needMod")
				}
			}
		case <-t.C:
			return "", ErrTimeout
		case <-ctx.Done():
			return "", ErrCancel
		}
	}
}
