//go:build windows

package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"fyne.io/systray"
	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	pSetWindowLongPtrW   = user32.NewProc("SetWindowLongPtrW")
	pGetWindowLongPtrW   = user32.NewProc("GetWindowLongPtrW")
	pCallWindowProcW     = user32.NewProc("CallWindowProcW")
	pShowWindow          = user32.NewProc("ShowWindow")
	pIsIconic            = user32.NewProc("IsIconic")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	pGetMessageW         = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessageW    = user32.NewProc("DispatchMessageW")
	pLoadImageW          = user32.NewProc("LoadImageW")
	pSendMessageW        = user32.NewProc("SendMessageW")
	pGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	pFindWindowW         = user32.NewProc("FindWindowW")
	pDestroyWindow       = user32.NewProc("DestroyWindow")
	pGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	pGetWindowThreadPID  = user32.NewProc("GetWindowThreadProcessId")
	pAttachThreadInput   = user32.NewProc("AttachThreadInput")
	pBringWindowToTop    = user32.NewProc("BringWindowToTop")
	pIsWindowVisible     = user32.NewProc("IsWindowVisible")
)

const (
	gwlpWndProc     = ^uintptr(3) // -4
	wmClose         = 0x0010
	wmQuit          = 0x0012
	wmQueryEndSess  = 0x0011
	wmEndSession    = 0x0016
	wmSetIcon       = 0x0080
	swHide          = 0
	swShow          = 5
	wmSize          = 0x0005
	sizeMinimized   = 1
	swRestore       = 9
	imageIcon       = 1
	lrLoadFromFile  = 0x10
	smCxIcon        = 11
	smCxSmIcon      = 49
	iconSmall       = 0
	iconBig         = 1
	windowWidth     = 1040
	windowHeight    = 820
	windowMinWidth  = 480
	windowMinHeight = 560
	mapWidth        = 1100
	mapHeight       = 760
	wmApp           = 0x8000 // WM_APP: так библиотека будит свою очередь Dispatch
)

type nativeState struct {
	thread   uint32 // поток окна и трея (главный)
	w        webview2.WebView
	hwnd     uintptr
	origProc uintptr
	quitting bool
	collect  *systray.MenuItem
	show     *systray.MenuItem
	exit     *systray.MenuItem

	// Окно «Карта Авалона» — второе окно WebView2 на том же потоке. Крестик
	// его прячет; при следующем открытии оно же показывается снова.
	mapW      webview2.WebView
	mapHwnd   uintptr
	mapProc   uintptr
	mapFailed bool // не создалось — дальше карта открывается в браузере
}

// Run показывает окно и трей и крутит цикл сообщений до Quit. Звать из
// главной горутины: окна Windows принадлежат потоку, который их создал, а
// systray закрепляет главную горутину за потоком при загрузке пакета.
func (d *Desktop) Run() {
	runtime.LockOSThread()
	d.mu.Lock()
	d.native.thread = windows.GetCurrentThreadId()
	quit := d.quit
	d.mu.Unlock()
	if quit {
		d.markReady()
		return
	}

	// Трей — на этом же потоке; его сообщения разбирает цикл окна.
	systray.Register(d.trayReady, func() {
		if d.cfg.EndSession != nil {
			d.cfg.EndSession()
		}
	})

	w := d.createWindow()
	d.markReady()
	if w == nil {
		d.mu.Lock()
		d.fallback = true
		d.hidden = false // страница в браузере
		d.mu.Unlock()
		if !d.cfg.Hidden {
			OpenURL(d.cfg.URL)
		}
		pumpMessages()
	} else {
		w.Run()
	}
	systray.Quit()
}

// createWindow — окно WebView2. nil — не вышло (причина в журнале).
func (d *Desktop) createWindow() webview2.WebView {
	// Без среды WebView2 библиотека падает через log.Fatal, поэтому сначала
	// спрашиваем, есть ли она. В Windows 11 она стоит всегда, в Windows 10 —
	// почти всегда (её ставит Edge).
	ver, err := webviewloader.GetInstalledVersion()
	if err != nil || ver == "" {
		d.cfg.Logf("WebView2 нет (%q, %v) — открываю страницу в браузере", ver, err)
		return nil
	}
	d.cfg.Logf("WebView2 %s", ver)
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  filepath.Join(d.cfg.DataDir, "WebView2"),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title: d.cfg.Title, Width: windowWidth, Height: windowHeight, Center: true,
		},
	})
	if w == nil {
		d.cfg.Logf("окно WebView2 не создалось — открываю страницу в браузере")
		// Окно библиотека уже показала, а встроить браузер не смогла: убираем пустое.
		cls, _ := windows.UTF16PtrFromString("webview")
		title, _ := windows.UTF16PtrFromString(d.cfg.Title)
		if h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title))); h != 0 {
			pDestroyWindow.Call(h)
		}
		return nil
	}
	hwnd := uintptr(w.Window())
	w.SetSize(windowMinWidth, windowMinHeight, webview2.HintMin)
	d.setIcon(hwnd)
	// Свой обработчик сообщений поверх библиотечного: крестик прячет окно в
	// трей, выключение Windows успевает сохранить сессию.
	// Прежний обработчик запоминаем до подмены: сообщения могут прийти сразу.
	orig, _, _ := pGetWindowLongPtrW.Call(hwnd, gwlpWndProc)
	d.mu.Lock()
	d.native.w, d.native.hwnd, d.native.origProc = w, hwnd, orig
	d.mu.Unlock()
	pSetWindowLongPtrW.Call(hwnd, gwlpWndProc, windows.NewCallback(d.wndProc))
	if d.cfg.Hidden {
		pShowWindow.Call(hwnd, swHide)
	}
	w.Navigate(d.cfg.URL)
	return w
}

func (d *Desktop) wndProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case wmClose:
		d.mu.Lock()
		q := d.native.quitting
		d.mu.Unlock()
		if !q {
			pShowWindow.Call(hwnd, swHide)
			d.setHidden(true)
			return 0
		}
	case wmSize:
		// Свернули — кролик замирает; развернули — снова живой.
		// Размер спрятанного окна (смена DPI) видимым его не делает.
		if v, _, _ := pIsWindowVisible.Call(hwnd); v != 0 {
			d.setHidden(wp == sizeMinimized)
		}
	case wmQueryEndSess:
		return 1
	case wmEndSession:
		if wp != 0 && d.cfg.EndSession != nil {
			d.cfg.EndSession()
		}
		return 0
	}
	r, _, _ := pCallWindowProcW.Call(d.native.origProc, hwnd, msg, wp, lp)
	return r
}

// setIcon — кролик в заголовке и на панели задач. Значок exe (из ресурса)
// окну WebView2 не достаётся: библиотека берёт стандартный.
func (d *Desktop) setIcon(hwnd uintptr) {
	path := filepath.Join(d.cfg.DataDir, "albion-journal.ico")
	if err := os.WriteFile(path, Icon, 0644); err != nil {
		return
	}
	p, _ := windows.UTF16PtrFromString(path)
	for _, k := range []struct{ metric, which uintptr }{{smCxIcon, iconBig}, {smCxSmIcon, iconSmall}} {
		sz, _, _ := pGetSystemMetrics.Call(k.metric)
		h, _, _ := pLoadImageW.Call(0, uintptr(unsafe.Pointer(p)), imageIcon, sz, sz, lrLoadFromFile)
		if h != 0 {
			pSendMessageW.Call(hwnd, wmSetIcon, k.which, h)
		}
	}
}

func (d *Desktop) trayReady() {
	systray.SetIcon(Icon)
	systray.SetTooltip(d.cfg.Title)
	systray.SetOnTapped(d.Show)
	show := systray.AddMenuItem(d.cfg.Label("tray.show"), "")
	collect := systray.AddMenuItem("", "")
	systray.AddSeparator()
	exit := systray.AddMenuItem(d.cfg.Label("tray.quit"), "")
	d.mu.Lock()
	d.native.show, d.native.collect, d.native.exit = show, collect, exit
	d.mu.Unlock()
	d.Relabel()
	for {
		select {
		case <-show.ClickedCh:
			d.Show()
		case <-collect.ClickedCh:
			if d.cfg.SetCollecting != nil && d.cfg.Collecting != nil {
				if err := d.cfg.SetCollecting(!d.cfg.Collecting()); err != nil {
					d.cfg.Logf("сбор из трея: %v", err)
				}
			}
			d.Relabel()
		case <-exit.ClickedCh:
			d.Quit()
			return
		}
	}
}

// Relabel обновляет надписи трея (язык сменился, сбор запущен/остановлен).
func (d *Desktop) Relabel() {
	d.mu.Lock()
	show, collect, exit := d.native.show, d.native.collect, d.native.exit
	d.mu.Unlock()
	if show == nil {
		return
	}
	show.SetTitle(d.cfg.Label("tray.show"))
	exit.SetTitle(d.cfg.Label("tray.quit"))
	key := "tray.startCollect"
	if d.cfg.Collecting != nil && d.cfg.Collecting() {
		key = "tray.stopCollect"
	}
	collect.SetTitle(d.cfg.Label(key))
}

// OpenMap показывает окно «Карта Авалона» с адресом url. Без WebView2 (или
// если окно карты не создалось) — в браузере.
func (d *Desktop) OpenMap(url string) {
	d.mu.Lock()
	w, fb, failed := d.native.w, d.fallback, d.native.mapFailed
	d.mu.Unlock()
	if fb || w == nil || failed {
		OpenURL(url)
		return
	}
	w.Dispatch(func() { d.showMap(url) })
}

// showMap — на потоке окон (через Dispatch главного окна: очередь Dispatch
// разбирает только его цикл сообщений).
func (d *Desktop) showMap(url string) {
	d.mu.Lock()
	mw, hwnd := d.native.mapW, d.native.mapHwnd
	d.mu.Unlock()
	if mw == nil {
		if mw = d.createMap(); mw == nil {
			d.mu.Lock()
			d.native.mapFailed = true
			d.mu.Unlock()
			OpenURL(url)
			return
		}
		hwnd = uintptr(mw.Window())
	}
	mw.Navigate(url)
	if r, _, _ := pIsIconic.Call(hwnd); r != 0 {
		pShowWindow.Call(hwnd, swRestore)
	} else {
		pShowWindow.Call(hwnd, swShow)
	}
	pSetForegroundWindow.Call(hwnd)
}

func (d *Desktop) createMap() webview2.WebView {
	title := d.cfg.Label("map.window")
	// Пока WebView2 встраивается, библиотека крутит свой цикл сообщений и
	// съедает то, что адресовано потоку: пробуждение очереди Dispatch и
	// WM_QUIT от «Выход». После создания посылаем их заново.
	defer d.repost()
	mw := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  filepath.Join(d.cfg.DataDir, "WebView2"),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title: title, Width: mapWidth, Height: mapHeight, Center: true,
		},
	})
	if mw == nil {
		d.cfg.Logf("окно карты WebView2 не создалось — карта откроется в браузере")
		// Окно уже показано, а браузер в него не встроился. Только прячем:
		// на WM_DESTROY библиотека завершила бы цикл сообщений всей программы.
		cls, _ := windows.UTF16PtrFromString("webview")
		t, _ := windows.UTF16PtrFromString(title)
		if h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(t))); h != 0 {
			pShowWindow.Call(h, swHide)
		}
		return nil
	}
	hwnd := uintptr(mw.Window())
	mw.SetSize(windowMinWidth, 400, webview2.HintMin)
	d.setIcon(hwnd)
	orig, _, _ := pGetWindowLongPtrW.Call(hwnd, gwlpWndProc)
	d.mu.Lock()
	d.native.mapW, d.native.mapHwnd, d.native.mapProc = mw, hwnd, orig
	d.mu.Unlock()
	pSetWindowLongPtrW.Call(hwnd, gwlpWndProc, windows.NewCallback(d.mapWndProc))
	d.cfg.Logf("окно карты открыто")
	return mw
}

// mapWndProc: крестик окна карты только прячет его. Библиотека бы его
// уничтожила, а на WM_DESTROY она завершает цикл сообщений — закрылась бы
// вся программа.
func (d *Desktop) mapWndProc(hwnd, msg, wp, lp uintptr) uintptr {
	if msg == wmClose {
		pShowWindow.Call(hwnd, swHide)
		return 0
	}
	d.mu.Lock()
	orig := d.native.mapProc
	d.mu.Unlock()
	r, _, _ := pCallWindowProcW.Call(orig, hwnd, msg, wp, lp)
	return r
}

// repost будит очередь Dispatch и повторяет выход, если его просили, пока
// работал вложенный цикл сообщений.
func (d *Desktop) repost() {
	d.mu.Lock()
	th, quit := d.native.thread, d.native.quitting
	d.mu.Unlock()
	pPostThreadMessageW.Call(uintptr(th), wmApp, 0, 0)
	if quit {
		pPostThreadMessageW.Call(uintptr(th), wmQuit, 0, 0)
	}
}

// MapEval выполняет скрипт на открытой карте (подсветить новую зону).
// Окна карты нет — ничего.
func (d *Desktop) MapEval(js string) {
	if js == "" {
		return
	}
	d.mu.Lock()
	w, mw := d.native.w, d.native.mapW
	d.mu.Unlock()
	if w == nil || mw == nil {
		return
	}
	w.Dispatch(func() { mw.Eval(js) })
}

// Show выводит окно вперёд (из трея, из второй копии программы).
func (d *Desktop) Show() {
	d.mu.Lock()
	w, hwnd, fb := d.native.w, d.native.hwnd, d.fallback
	d.mu.Unlock()
	if fb || w == nil {
		if fb {
			OpenURL(d.cfg.URL)
		}
		return
	}
	w.Dispatch(func() {
		if r, _, _ := pIsIconic.Call(hwnd); r != 0 {
			pShowWindow.Call(hwnd, swRestore)
		} else {
			pShowWindow.Call(hwnd, swShow)
		}
		d.setHidden(false)
		pSetForegroundWindow.Call(hwnd)
	})
}

// setHidden запоминает, видно ли окно, и сразу говорит странице (звать на
// потоке окна). WebView2 у спрятанного окна страницу скрытой не считает
// (document.hidden остаётся false), поэтому кролика останавливает сама
// страница по этому сигналу.
func (d *Desktop) setHidden(h bool) {
	d.mu.Lock()
	changed := d.hidden != h
	d.hidden = h
	w := d.native.w
	d.mu.Unlock()
	if changed && w != nil {
		v := "true"
		if h {
			v = "false"
		}
		w.Eval("window.ajVisible && window.ajVisible(" + v + ")")
	}
}

// ShowFront выводит окно вперёд и отдаёт ему фокус — один раз, когда
// запустилась игра (main ждёт несколько секунд, чтобы окно игры уже
// появилось и не перекрыло наше). Windows не даёт фоновой программе
// забрать фокус, поэтому на время подключаемся к очереди ввода окна,
// у которого фокус сейчас (обычный приём AttachThreadInput). Окно в
// браузере (без WebView2) не трогаем.
func (d *Desktop) ShowFront() {
	d.mu.Lock()
	w, hwnd, fb := d.native.w, d.native.hwnd, d.fallback
	d.mu.Unlock()
	if fb || w == nil {
		return
	}
	w.Dispatch(func() {
		if r, _, _ := pIsIconic.Call(hwnd); r != 0 {
			pShowWindow.Call(hwnd, swRestore)
		} else {
			pShowWindow.Call(hwnd, swShow)
		}
		d.setHidden(false)
		cur := uintptr(windows.GetCurrentThreadId())
		if fg, _, _ := pGetForegroundWindow.Call(); fg != 0 && fg != hwnd {
			if th, _, _ := pGetWindowThreadPID.Call(fg, 0); th != 0 && th != cur {
				pAttachThreadInput.Call(cur, th, 1)
				defer pAttachThreadInput.Call(cur, th, 0)
			}
		}
		pBringWindowToTop.Call(hwnd)
		pSetForegroundWindow.Call(hwnd)
	})
}

// Quit завершает цикл сообщений: Run вернётся, программа выйдет.
func (d *Desktop) Quit() {
	d.mu.Lock()
	d.quit = true
	d.native.quitting = true
	th := d.native.thread
	d.mu.Unlock()
	if th != 0 {
		pPostThreadMessageW.Call(uintptr(th), wmQuit, 0, 0)
	}
}

// pumpMessages — цикл сообщений без окна (трей при запасном пути).
func pumpMessages() {
	var msg struct {
		hwnd    uintptr
		message uint32
		wParam  uintptr
		lParam  uintptr
		time    uint32
		pt      struct{ x, y int32 }
	}
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// OpenURL открывает адрес в браузере пользователя. Программа работает с
// правами администратора, и прямой ShellExecute поднял бы браузер тоже
// с ними; explorer.exe передаёт адрес уже запущенной оболочке, и браузер
// открывается с обычными правами.
func OpenURL(url string) { explorer(url) }

// OpenFolder открывает папку в Проводнике.
func OpenFolder(dir string) { explorer(dir) }

func explorer(arg string) {
	cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "explorer.exe"), arg)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err == nil {
		go cmd.Wait()
	}
}

// Message — окно с сообщением (консоли у программы нет).
func Message(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONWARNING|windows.MB_SETFOREGROUND)
}

const mutexName = `Local\AlbionJournal.Single`

// OtherRunning — запущена ли уже копия программы. Знак не создаёт: так
// зовёт копия без прав администратора перед перезапуском с ними, иначе её
// же наследница с правами приняла бы этот знак за чужую копию.
func OtherRunning() bool {
	name, _ := windows.UTF16PtrFromString(mutexName)
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name)
	if err == nil {
		windows.CloseHandle(h)
		return true
	}
	// Знак создан копией с правами администратора — открыть его без прав
	// нельзя, но он есть.
	return err == windows.ERROR_ACCESS_DENIED
}

// instanceMutex держит знак «программа уже запущена» до выхода.
var instanceMutex windows.Handle

// SingleInstance — первая ли это копия программы. Вторая копия (запуск из
// автозапуска поверх ручного и наоборот) должна только показать окно первой:
// два перехватчика WinDivert и два обхода на одном трафике мешают друг другу.
func SingleInstance() bool {
	name, _ := windows.UTF16PtrFromString(mutexName)
	h, err := windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS || err == windows.ERROR_ACCESS_DENIED {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return false
	}
	instanceMutex = h
	return true
}
