//go:build windows

package hotkey

import (
	"fmt"
	"runtime"
	"sync"
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
	pGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
)

const (
	whKeyboardLL = 13
	whMouseLL    = 14
	wmQuit       = 0x0012
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

// Hook — поставленный хук и его поток.
type Hook struct {
	thread uint32
	done   chan struct{}
	once   sync.Once
}

// Обратные вызовы хуков — общие на процесс (syscall.NewCallback не
// освобождается), поэтому создаются один раз, а что ловить — в cur.
var (
	cbOnce  sync.Once
	mouseCB uintptr
	keyCB   uintptr
	curMu   sync.Mutex
	curKey  Key
	curFire chan struct{}
)

func callbacks() {
	cbOnce.Do(func() {
		mouseCB = windows.NewCallback(func(code, wp uintptr, m *msllHook) uintptr {
			if int32(code) >= 0 {
				curMu.Lock()
				k, ch := curKey, curFire
				curMu.Unlock()
				if ch != nil && MatchMouse(k, wp, m.MouseData, m.Flags) {
					select { // не ждём: хук должен вернуться сразу
					case ch <- struct{}{}:
					default:
					}
				}
			}
			r, _, _ := pCallNextHookEx.Call(0, code, wp, uintptr(unsafe.Pointer(m)))
			return r
		})
		keyCB = windows.NewCallback(func(code, wp uintptr, kb *kbdllHook) uintptr {
			if int32(code) >= 0 {
				curMu.Lock()
				k, ch := curKey, curFire
				curMu.Unlock()
				if ch != nil && MatchKey(k, wp, kb.VK, kb.Flags) {
					select {
					case ch <- struct{}{}:
					default:
					}
				}
			}
			r, _, _ := pCallNextHookEx.Call(0, code, wp, uintptr(unsafe.Pointer(kb)))
			return r
		})
	})
}

// Start ставит хук для кнопки k на своём потоке; fire зовётся в отдельной
// горутине (работа там может идти секундами — хук ждать не должен).
// Ставится только нужный хук: мышиный для кнопок мыши, клавиатурный — для
// F-клавиш (клавиатурный хук без нужды антивирусы не любят). Одновременно
// работает один Hook: прежний надо снять Stop.
func Start(k Key, fire func(), logf func(string, ...any)) (*Hook, error) {
	if k == Off || (k.Mouse() == 0 && k.VK() == 0) {
		return nil, nil
	}
	callbacks()
	ch := make(chan struct{}, 1)
	curMu.Lock()
	curKey, curFire = k, ch
	curMu.Unlock()
	h := &Hook{done: make(chan struct{})}
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

	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		h.thread = windows.GetCurrentThreadId()
		mod, _, _ := pGetModuleHandleW.Call(0)
		kind, cb := uintptr(whMouseLL), mouseCB
		if k.Mouse() == 0 {
			kind, cb = whKeyboardLL, keyCB
		}
		hh, _, err := pSetWindowsHookExW.Call(kind, cb, mod, 0)
		if hh == 0 {
			ready <- fmt.Errorf("SetWindowsHookEx: %v", err)
			return
		}
		ready <- nil
		// Низкоуровневый хук работает, пока у потока крутится цикл сообщений.
		var msg [48]byte
		for {
			r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
		}
		pUnhookWindowsHookEx.Call(hh)
		logf("кнопка карточки: хук снят")
	}()
	if err := <-ready; err != nil {
		h.Stop()
		return nil, err
	}
	logf("кнопка карточки: жду %s", k.Label())
	return h, nil
}

// Stop снимает хук и останавливает его поток.
func (h *Hook) Stop() {
	if h == nil {
		return
	}
	h.once.Do(func() {
		close(h.done)
		if h.thread != 0 {
			pPostThreadMessageW.Call(uintptr(h.thread), wmQuit, 0, 0)
		}
	})
}
