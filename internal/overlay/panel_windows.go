//go:build windows

package overlay

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"albionzonefix/internal/zonecard"
)

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	gdi32   = windows.NewLazySystemDLL("gdi32.dll")
	shcore  = windows.NewLazySystemDLL("shcore.dll")
	kernel3 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassExW             = user32.NewProc("RegisterClassExW")
	pCreateWindowExW              = user32.NewProc("CreateWindowExW")
	pDefWindowProcW               = user32.NewProc("DefWindowProcW")
	pDestroyWindow                = user32.NewProc("DestroyWindow")
	pPostQuitMessage              = user32.NewProc("PostQuitMessage")
	pGetMessageW                  = user32.NewProc("GetMessageW")
	pTranslateMessage             = user32.NewProc("TranslateMessage")
	pDispatchMessageW             = user32.NewProc("DispatchMessageW")
	pPostMessageW                 = user32.NewProc("PostMessageW")
	pSetLayeredWindowAttributes   = user32.NewProc("SetLayeredWindowAttributes")
	pSetWindowPos                 = user32.NewProc("SetWindowPos")
	pShowWindow                   = user32.NewProc("ShowWindow")
	pSetWindowRgn                 = user32.NewProc("SetWindowRgn")
	pSetTimer                     = user32.NewProc("SetTimer")
	pKillTimer                    = user32.NewProc("KillTimer")
	pInvalidateRect               = user32.NewProc("InvalidateRect")
	pBeginPaint                   = user32.NewProc("BeginPaint")
	pEndPaint                     = user32.NewProc("EndPaint")
	pFillRect                     = user32.NewProc("FillRect")
	pDrawTextW                    = user32.NewProc("DrawTextW")
	pGetCursorPos                 = user32.NewProc("GetCursorPos")
	pMonitorFromPoint             = user32.NewProc("MonitorFromPoint")
	pGetMonitorInfoW              = user32.NewProc("GetMonitorInfoW")
	pSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
	pGetDpiForMonitor             = shcore.NewProc("GetDpiForMonitor")
	pGetModuleHandleW             = kernel3.NewProc("GetModuleHandleW")
	pCreateFontW                  = gdi32.NewProc("CreateFontW")
	pCreateSolidBrush             = gdi32.NewProc("CreateSolidBrush")
	pCreatePen                    = gdi32.NewProc("CreatePen")
	pCreateRoundRectRgn           = gdi32.NewProc("CreateRoundRectRgn")
	pRoundRect                    = gdi32.NewProc("RoundRect")
	pGetStockObject               = gdi32.NewProc("GetStockObject")
	pSelectObject                 = gdi32.NewProc("SelectObject")
	pDeleteObject                 = gdi32.NewProc("DeleteObject")
	pCreateCompatibleDC           = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap       = gdi32.NewProc("CreateCompatibleBitmap")
	pDeleteDC                     = gdi32.NewProc("DeleteDC")
	pBitBlt                       = gdi32.NewProc("BitBlt")
	pSetBkMode                    = gdi32.NewProc("SetBkMode")
	pSetTextColor                 = gdi32.NewProc("SetTextColor")
	pGetTextExtentPoint32W        = gdi32.NewProc("GetTextExtentPoint32W")
)

const (
	wsPopup          = 0x80000000
	wsExTopmost      = 0x00000008
	wsExTransparent  = 0x00000020
	wsExToolWindow   = 0x00000080
	wsExLayered      = 0x00080000
	wsExNoActivate   = 0x08000000
	lwaAlpha         = 0x2
	wmDestroy        = 0x0002
	wmClose          = 0x0010
	wmPaint          = 0x000F
	wmEraseBkgnd     = 0x0014
	wmTimer          = 0x0113
	wmNCHitTest      = 0x0084
	wmMouseActivate  = 0x0021
	wmDPIChanged     = 0x02E0
	wmApp            = 0x8000
	wmShow           = wmApp + 1
	htTransparent    = ^uintptr(0) // -1
	maNoActivate     = 3
	swHide           = 0
	swpNoActivate    = 0x0010
	swpShowWindow    = 0x0040
	hwndTopmost      = ^uintptr(0) // -1
	monitorNearest   = 2
	mdtEffectiveDPI  = 0
	dpiPerMonitorV2  = ^uintptr(3) // -4
	transparentBk    = 1
	nullBrush        = 5
	psSolid          = 0
	srcCopy          = 0x00CC0020
	dtLeft           = 0x0
	dtVCenter        = 0x4
	dtSingleLine     = 0x20
	dtNoPrefix       = 0x800
	dtEndEllipsis    = 0x8000
	fwNormal         = 400
	fwSemiBold       = 600
	defaultCharset   = 1
	cleartypeQuality = 5
	hideTimer        = 1
	panelAlpha       = 242 // ~95 %: игра чуть просвечивает, как у мака
)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	_       uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type paintStruct struct {
	Hdc       uintptr
	Erase     int32
	Paint     rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}

// Panel — окно панели. Живёт на своём потоке со своим циклом сообщений:
// главное окно и трей программы его не ждут.
type Panel struct {
	logf      func(string, ...any)
	once      sync.Once
	ready     chan struct{}
	readyOnce sync.Once
	hwnd      uintptr
	// broken — панель упала (паника на её потоке): до конца запуска не
	// показываем, карточка идёт уведомлением (zonecard.Present).
	broken atomic.Bool

	mu      sync.Mutex
	pending *request
	mark    point // курсор в момент нажатия кнопки карточки
	markAt  time.Time

	inflight *request // на экране сейчас (для запасного пути при панике в show/paint)

	// Дальше — только на потоке панели.
	cur   Layout
	fonts map[int]map[Font]uintptr // dpi → шрифты
}

type request struct {
	card     zonecard.Panel
	corner   string
	secs     int
	fallback func() // панель упала на этой карточке — показать её иначе
}

// панель одна на программу: оконной процедуре нужен её адрес.
var (
	active     *Panel
	wndProcPtr uintptr
)

// New — панель; окно создаётся при первом показе.
func New(logf func(string, ...any)) *Panel {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Panel{logf: logf, ready: make(chan struct{}), fonts: map[int]map[Font]uintptr{}}
}

// OK — можно ли показывать панель: не падала в этом запуске и окно
// создалось (или ещё не создавалось).
func (p *Panel) OK() bool {
	if p.broken.Load() {
		return false
	}
	select {
	case <-p.ready:
		return p.hwnd != 0
	default:
		return true
	}
}

// Mark запоминает, где курсор в момент нажатия кнопки карточки: панель
// встанет на этот монитор, даже если мышь потом увели на другой экран.
func (p *Panel) Mark() {
	var pt point
	if r, _, _ := pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
		return
	}
	p.mu.Lock()
	p.mark, p.markAt = pt, time.Now()
	p.mu.Unlock()
}

// Show показывает карточку в углу corner экрана с игрой на secs секунд.
// Не блокирует; новая карточка заменяет прежнюю. false — панели нет
// (сломалась или не создалась): показать иначе сразу. fallback зовётся,
// если панель упадёт на этой карточке.
func (p *Panel) Show(card zonecard.Panel, corner string, secs int, fallback func()) bool {
	if p.broken.Load() {
		return false
	}
	p.once.Do(func() { go p.loop() })
	<-p.ready
	if p.hwnd == 0 || p.broken.Load() {
		return false
	}
	p.mu.Lock()
	p.pending = &request{card: card, corner: corner, secs: secs, fallback: fallback}
	p.mu.Unlock()
	pPostMessageW.Call(p.hwnd, wmShow, 0, 0)
	return true
}

// fail — паника на потоке панели: в журнал со стеком, панель выключается
// до конца запуска и прячется, карточка, на которой упали, — запасным путём.
// Зовётся из recover на потоке панели.
func (p *Panel) fail(where string, v any) {
	p.broken.Store(true)
	p.logf("панель поверх игры упала (%s): %v — дальше карточка уведомлением\n%s", where, v, debug.Stack())
	req := p.inflight
	p.inflight = nil
	if p.hwnd != 0 {
		pKillTimer.Call(p.hwnd, hideTimer)
		pShowWindow.Call(p.hwnd, swHide)
	}
	if req != nil && req.fallback != nil {
		go req.fallback()
	}
}

func (p *Panel) markReady() { p.readyOnce.Do(func() { close(p.ready) }) }

// Close убирает окно (при выходе).
func (p *Panel) Close() {
	select {
	case <-p.ready:
		if p.hwnd != 0 {
			pPostMessageW.Call(p.hwnd, wmClose, 0, 0)
		}
	default:
	}
}

func (p *Panel) loop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Паника здесь не должна ронять программу (перехват, зона, окно).
	defer func() {
		if v := recover(); v != nil {
			p.fail("поток", v)
		}
		p.markReady()
	}()
	// Поток «по монитору» (V2): координаты и размеры — настоящие пиксели,
	// масштаб монитора учитываем сами (Scale).
	if pSetThreadDpiAwarenessContext.Find() == nil {
		pSetThreadDpiAwarenessContext.Call(dpiPerMonitorV2)
	}
	hwnd, err := p.create()
	if err != nil {
		p.logf("панель поверх игры не создалась: %v", err)
	}
	p.hwnd = hwnd
	p.markReady()
	if hwnd == 0 {
		return
	}
	var m msg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	for _, fs := range p.fonts {
		for _, f := range fs {
			pDeleteObject.Call(f)
		}
	}
}

func (p *Panel) create() (uintptr, error) {
	inst, _, _ := pGetModuleHandleW.Call(0)
	cls, _ := windows.UTF16PtrFromString("AlbionJournalZonePanel")
	title, _ := windows.UTF16PtrFromString("Albion Journal")
	active = p
	if wndProcPtr == 0 {
		wndProcPtr = windows.NewCallback(wndProc)
	}
	wc := wndClassEx{WndProc: wndProcPtr, Instance: inst, ClassName: cls}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return 0, err
	}
	// Не забирает фокус (NOACTIVATE), нет кнопки на панели задач
	// (TOOLWINDOW), всегда сверху, клики насквозь (LAYERED+TRANSPARENT).
	ex := uintptr(wsExTopmost | wsExToolWindow | wsExNoActivate | wsExTransparent | wsExLayered)
	hwnd, _, err := pCreateWindowExW.Call(ex, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)), wsPopup,
		0, 0, 10, 10, 0, 0, inst, 0)
	if hwnd == 0 {
		return 0, err
	}
	pSetLayeredWindowAttributes.Call(hwnd, 0, panelAlpha, lwaAlpha)
	return hwnd, nil
}

func wndProc(hwnd, m, wp, lp uintptr) (ret uintptr) {
	p := active
	// Go-паника не может пройти через кадры Win32 — ловим здесь. EndPaint
	// при панике в paint отработает отложенно, окно не будет перерисовываться
	// без конца.
	defer func() {
		if v := recover(); v != nil {
			p.fail(fmt.Sprintf("сообщение 0x%X", m), v)
			ret = 0
		}
	}()
	switch m {
	case wmShow:
		p.show(hwnd)
		return 0
	case wmTimer:
		if wp == hideTimer {
			pKillTimer.Call(hwnd, hideTimer)
			pShowWindow.Call(hwnd, swHide)
			p.inflight = nil // карточка показана и спрятана
		}
		return 0
	case wmPaint:
		p.paint(hwnd)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmDPIChanged:
		// Панель уже разложена под DPI монитора назначения (show) —
		// предложенный системой прямоугольник не берём, иначе масштаб дважды.
		return 0
	case wmNCHitTest:
		return htTransparent
	case wmMouseActivate:
		return maNoActivate
	case wmClose:
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

// colorRef — 0xRRGGBB → COLORREF (0x00BBGGRR).
func colorRef(c uint32) uintptr {
	return uintptr((c>>16)&0xFF | (c & 0xFF00) | (c&0xFF)<<16)
}

func (p *Panel) font(dpi int, f Font) uintptr {
	fs := p.fonts[dpi]
	if fs == nil {
		fs = map[Font]uintptr{}
		p.fonts[dpi] = fs
	}
	if h := fs[f]; h != 0 {
		return h
	}
	weight := fwNormal
	if f == FontTitle {
		weight = fwSemiBold
	}
	face, _ := windows.UTF16PtrFromString("Segoe UI")
	h, _, _ := pCreateFontW.Call(uintptr(uint32(int32(-Scale(FontPx[f], dpi)))), 0, 0, 0, uintptr(weight), 0, 0, 0,
		defaultCharset, 0, 0, cleartypeQuality, 0, uintptr(unsafe.Pointer(face)))
	fs[f] = h
	return h
}

// markFresh — сколько после нажатия верим запомненному курсору.
const markFresh = 15 * time.Second

// where — рабочая область и dpi монитора, где был курсор при нажатии
// кнопки карточки (или где он сейчас, если нажатие не запомнено).
func (p *Panel) where() (Rect, int) {
	p.mu.Lock()
	pt, at := p.mark, p.markAt
	p.mu.Unlock()
	if at.IsZero() || time.Since(at) > markFresh {
		pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	}
	packed := uintptr(uint32(pt.X)) | uintptr(uint32(pt.Y))<<32
	mon, _, _ := pMonitorFromPoint.Call(packed, monitorNearest)
	work, dpi := Rect{0, 0, 1920, 1080}, 96
	if mon == 0 {
		return work, dpi
	}
	mi := monitorInfo{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	if r, _, _ := pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); r != 0 {
		work = Rect{int(mi.Work.Left), int(mi.Work.Top), int(mi.Work.Right), int(mi.Work.Bottom)}
	}
	if pGetDpiForMonitor.Find() == nil {
		var dx, dy uint32
		if hr, _, _ := pGetDpiForMonitor.Call(mon, mdtEffectiveDPI, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); hr == 0 && dx > 0 {
			dpi = int(dx)
		}
	}
	return work, dpi
}

func (p *Panel) show(hwnd uintptr) {
	p.mu.Lock()
	req := p.pending
	p.pending = nil
	p.mu.Unlock()
	if req == nil {
		return
	}
	p.inflight = req
	// DPI и шрифты считаются заново при каждом показе: монитор и масштаб
	// могли поменяться.
	work, dpi := p.where()
	dc, _, _ := pCreateCompatibleDC.Call(0)
	origFont, _, _ := pSelectObject.Call(dc, p.font(dpi, FontText))
	measure := func(f Font, s string) int {
		u, _ := windows.UTF16FromString(s)
		if len(u) <= 1 {
			return 0
		}
		pSelectObject.Call(dc, p.font(dpi, f))
		var sz struct{ CX, CY int32 }
		pGetTextExtentPoint32W.Call(dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&sz)))
		return int(sz.CX)
	}
	p.cur = Arrange(req.card, dpi, measure)
	pSelectObject.Call(dc, origFont) // шрифт — из кэша, DC его не удаляет
	pDeleteDC.Call(dc)

	x, y := Place(work, p.cur.W, p.cur.H, req.corner, dpi)
	rgn, _, _ := pCreateRoundRectRgn.Call(0, 0, uintptr(p.cur.W+1), uintptr(p.cur.H+1), uintptr(p.cur.Radius), uintptr(p.cur.Radius))
	pSetWindowRgn.Call(hwnd, rgn, 1) // регион теперь принадлежит окну
	pSetWindowPos.Call(hwnd, hwndTopmost, uintptr(uint32(int32(x))), uintptr(uint32(int32(y))),
		uintptr(p.cur.W), uintptr(p.cur.H), swpNoActivate|swpShowWindow)
	pInvalidateRect.Call(hwnd, 0, 1)
	pSetTimer.Call(hwnd, hideTimer, uintptr(max(req.secs, 1)*1000), 0)
}

func (p *Panel) paint(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	l := p.cur
	if hdc == 0 || l.W == 0 {
		return
	}
	mem, _, _ := pCreateCompatibleDC.Call(hdc)
	bmp, _, _ := pCreateCompatibleBitmap.Call(hdc, uintptr(l.W), uintptr(l.H))
	oldBmp, _, _ := pSelectObject.Call(mem, bmp)

	fillRect := func(x, y, w, h int, c uint32) {
		br, _, _ := pCreateSolidBrush.Call(colorRef(c))
		r := rect{int32(x), int32(y), int32(x + w), int32(y + h)}
		pFillRect.Call(mem, uintptr(unsafe.Pointer(&r)), br)
		pDeleteObject.Call(br)
	}
	fillRect(0, 0, l.W, l.H, ColorBg)
	// Рамка по скруглению окна.
	pen, _, _ := pCreatePen.Call(psSolid, uintptr(max(1, Scale(1, l.DPI))), colorRef(ColorLine))
	oldPen, _, _ := pSelectObject.Call(mem, pen)
	nb, _, _ := pGetStockObject.Call(nullBrush)
	oldBr, _, _ := pSelectObject.Call(mem, nb)
	pRoundRect.Call(mem, 0, 0, uintptr(l.W), uintptr(l.H), uintptr(l.Radius), uintptr(l.Radius))
	pSelectObject.Call(mem, oldBr)
	pSelectObject.Call(mem, oldPen)
	pDeleteObject.Call(pen)

	pSetBkMode.Call(mem, transparentBk)
	oldFont, _, _ := pSelectObject.Call(mem, p.font(l.DPI, FontText))
	for _, op := range l.Ops {
		if !op.IsText {
			fillRect(op.X, op.Y, op.W, op.H, op.Color)
			continue
		}
		u, _ := windows.UTF16FromString(op.Text)
		if len(u) <= 1 || op.W <= 0 {
			continue
		}
		pSelectObject.Call(mem, p.font(l.DPI, op.Font))
		pSetTextColor.Call(mem, colorRef(op.Color))
		r := rect{int32(op.X), int32(op.Y), int32(op.X + op.W), int32(op.Y + op.H)}
		pDrawTextW.Call(mem, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&r)),
			dtLeft|dtVCenter|dtSingleLine|dtNoPrefix|dtEndEllipsis)
	}
	pSelectObject.Call(mem, oldFont)
	pBitBlt.Call(hdc, 0, 0, uintptr(l.W), uintptr(l.H), mem, 0, 0, srcCopy)
	pSelectObject.Call(mem, oldBmp)
	pDeleteObject.Call(bmp)
	pDeleteDC.Call(mem)
}
