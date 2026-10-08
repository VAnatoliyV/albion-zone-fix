//go:build windows

package screen

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                        = windows.NewLazySystemDLL("user32.dll")
	gdi32                         = windows.NewLazySystemDLL("gdi32.dll")
	shcore                        = windows.NewLazySystemDLL("shcore.dll")
	pSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
	pGetCursorPos                 = user32.NewProc("GetCursorPos")
	pGetSystemMetrics             = user32.NewProc("GetSystemMetrics")
	pGetDC                        = user32.NewProc("GetDC")
	pReleaseDC                    = user32.NewProc("ReleaseDC")
	pMonitorFromPoint             = user32.NewProc("MonitorFromPoint")
	pGetDpiForMonitor             = shcore.NewProc("GetDpiForMonitor")
	pCreateCompatibleDC           = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap       = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject                 = gdi32.NewProc("SelectObject")
	pBitBlt                       = gdi32.NewProc("BitBlt")
	pGetDIBits                    = gdi32.NewProc("GetDIBits")
	pDeleteObject                 = gdi32.NewProc("DeleteObject")
	pDeleteDC                     = gdi32.NewProc("DeleteDC")
)

const (
	dpiPerMonitorV2       = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
	smXVirtualScreen      = 76
	smYVirtualScreen      = 77
	smCxVirtualScreen     = 78
	smCyVirtualScreen     = 79
	monitorDefaultNearest = 2
	mdtEffectiveDPI       = 0
	srcCopy               = 0x00CC0020
	dibRGBColors          = 0
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [4]byte
}

// CaptureAroundCursor снимает область вокруг курсора (600×400 точек с учётом
// масштаба монитора), увеличивает для OCR и пишет PNG в path.
//
// Масштаб: на время снимка поток переводится в режим «по монитору» (V2) —
// тогда курсор, границы экрана и BitBlt в настоящих пикселях, а окно
// программы (WebView2) своё поведение не меняет. Игра в «эксклюзивном»
// полноэкранном режиме может сниматься чёрной — тогда нужен режим «окно без
// рамки» (проверяет тестер).
func CaptureAroundCursor(path string) (Info, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if pSetThreadDpiAwarenessContext.Find() == nil {
		if old, _, _ := pSetThreadDpiAwarenessContext.Call(dpiPerMonitorV2); old != 0 {
			defer pSetThreadDpiAwarenessContext.Call(old)
		}
	}

	var pt struct{ X, Y int32 }
	if r, _, err := pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
		return Info{}, fmt.Errorf("GetCursorPos: %v", err)
	}
	info := Info{Cursor: image.Pt(int(pt.X), int(pt.Y)), DPI: 96}
	if pMonitorFromPoint.Find() == nil && pGetDpiForMonitor.Find() == nil {
		packed := uintptr(uint32(pt.X)) | uintptr(uint32(pt.Y))<<32
		if mon, _, _ := pMonitorFromPoint.Call(packed, monitorDefaultNearest); mon != 0 {
			var dx, dy uint32
			if hr, _, _ := pGetDpiForMonitor.Call(mon, mdtEffectiveDPI, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); hr == 0 && dx > 0 {
				info.DPI = int(dx)
			}
		}
	}
	metric := func(i uintptr) int {
		v, _, _ := pGetSystemMetrics.Call(i)
		return int(int32(v))
	}
	vx, vy := metric(smXVirtualScreen), metric(smYVirtualScreen)
	bounds := image.Rect(vx, vy, vx+metric(smCxVirtualScreen), vy+metric(smCyVirtualScreen))
	w, h := Scaled(info.DPI)
	r := Around(info.Cursor.X, info.Cursor.Y, w, h, bounds)
	if r.Empty() {
		return info, errors.New("курсор вне экрана")
	}
	info.Rect = r

	pix, err := grab(r)
	if err != nil {
		return info, err
	}
	img := Upscale2(FromBGRA(pix, r.Dx(), r.Dy()))
	info.Out = img.Bounds().Size()
	return info, SavePNG(path, img)
}

// grab — пиксели рамки r экрана (BGRA, сверху вниз).
func grab(r image.Rectangle) ([]byte, error) {
	screen, _, _ := pGetDC.Call(0)
	if screen == 0 {
		return nil, errors.New("GetDC: нет доступа к экрану")
	}
	defer pReleaseDC.Call(0, screen)
	mem, _, _ := pCreateCompatibleDC.Call(screen)
	if mem == 0 {
		return nil, errors.New("CreateCompatibleDC не вышел")
	}
	defer pDeleteDC.Call(mem)
	bmp, _, _ := pCreateCompatibleBitmap.Call(screen, uintptr(r.Dx()), uintptr(r.Dy()))
	if bmp == 0 {
		return nil, errors.New("CreateCompatibleBitmap не вышел")
	}
	defer pDeleteObject.Call(bmp)
	old, _, _ := pSelectObject.Call(mem, bmp)
	ok, _, err := pBitBlt.Call(mem, 0, 0, uintptr(r.Dx()), uintptr(r.Dy()), screen,
		uintptr(int32(r.Min.X)), uintptr(int32(r.Min.Y)), srcCopy)
	pSelectObject.Call(mem, old) // GetDIBits — только с невыбранным битмапом
	if ok == 0 {
		return nil, fmt.Errorf("BitBlt: %v", err)
	}
	bi := bitmapInfo{Header: bitmapInfoHeader{Width: int32(r.Dx()), Height: -int32(r.Dy()), Planes: 1, BitCount: 32}}
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	pix := make([]byte, 4*r.Dx()*r.Dy())
	n, _, err := pGetDIBits.Call(mem, bmp, 0, uintptr(r.Dy()), uintptr(unsafe.Pointer(&pix[0])),
		uintptr(unsafe.Pointer(&bi)), dibRGBColors)
	if n == 0 {
		return nil, fmt.Errorf("GetDIBits: %v", err)
	}
	return pix, nil
}
