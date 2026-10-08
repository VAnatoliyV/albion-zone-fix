//go:build windows

package oldcopy

import (
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"albionzonefix/internal/autostart"
	"albionzonefix/internal/procutil"

	"golang.org/x/sys/windows"
)

// System — Env этой машины: процессы (снимок toolhelp32 + путь exe),
// задача автозапуска, системные папки.
func System(logf func(string, ...any)) Env {
	e := Env{Procs: procs, TaskExe: autostart.Installed, Self: windows.GetCurrentProcessId(), Logf: logf}
	for _, id := range []*windows.KNOWNFOLDERID{windows.FOLDERID_ProgramFiles, windows.FOLDERID_ProgramFilesX86,
		windows.FOLDERID_ProgramData, windows.FOLDERID_UserProfiles, windows.FOLDERID_Profile} {
		if p, err := windows.KnownFolderPath(id, 0); err == nil {
			e.Keep = append(e.Keep, p)
		}
	}
	for _, v := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "ProgramData"} {
		if p := os.Getenv(v); p != "" {
			e.Keep = append(e.Keep, p)
		}
	}
	if p, err := windows.KnownFolderPath(windows.FOLDERID_Windows, 0); err == nil {
		e.Under = append(e.Under, p)
	}
	if p := os.Getenv("SystemRoot"); p != "" {
		e.Under = append(e.Under, p)
	}
	return e
}

// procs — процессы с нашими именами и путями их exe.
func procs() ([]Proc, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var out []Proc
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		name := windows.UTF16ToString(e.ExeFile[:])
		if !strings.EqualFold(name, ExeName) && !strings.EqualFold(name, RecvName) && !strings.EqualFold(name, BypassName) {
			continue
		}
		if img, _, ok := procutil.Inspect(int(e.ProcessID)); ok {
			out = append(out, Proc{PID: e.ProcessID, Name: name, Exe: img})
		}
	}
	if err == windows.ERROR_NO_MORE_FILES {
		err = nil
	}
	return out, err
}

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	pSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")
)

const (
	wmEndSession    = 0x0016
	smtoAbortIfHung = 0x0002
	smtoBlock       = 0x0001
	softPerWindowMS = 3000
	softTotal       = 4 * time.Second
)

// Окна процесса: EnumWindows с одним обратным вызовом на всю программу
// (их число в Go ограничено).
var (
	enumPID  uint32
	enumOut  []windows.HWND
	enumProc = windows.NewCallback(func(h windows.HWND, _ uintptr) uintptr {
		var pid uint32
		windows.GetWindowThreadProcessId(h, &pid)
		if pid == enumPID {
			enumOut = append(enumOut, h)
		}
		return 1
	})
)

func windowsOf(pid uint32) []windows.HWND {
	enumPID, enumOut = pid, nil
	windows.EnumWindows(enumProc, nil)
	return enumOut
}

// WinCloser закрывает процессы в Windows.
type WinCloser struct{}

// Soft: окнам программы — WM_ENDSESSION (как при выключении Windows): она
// сохраняет сессию и останавливает сбор и обход. Так умеют и старые версии.
func (WinCloser) Soft(p Proc) {
	deadline := time.Now().Add(softTotal)
	for _, h := range windowsOf(p.PID) {
		if time.Now().After(deadline) {
			return
		}
		var res uintptr
		pSendMessageTimeoutW.Call(uintptr(h), wmEndSession, 1, 0, smtoAbortIfHung|smtoBlock,
			softPerWindowMS, uintptr(unsafe.Pointer(&res)))
	}
}

// Kill — TerminateProcess, если под этим pid всё ещё тот же exe (путь
// проверяем через тот же дескриптор, которым завершаем).
func (WinCloser) Kill(p Proc) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, p.PID)
	if err != nil {
		if _, _, ok := procutil.Inspect(int(p.PID)); !ok {
			return nil // уже вышел
		}
		return err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(windows.UTF16ToString(buf[:n])), filepath.Clean(p.Exe)) {
		return nil // pid уже занят другим процессом
	}
	return windows.TerminateProcess(h, 1)
}

// Wait ждёт выхода процессов не дольше d; возвращает тех, кто жив.
func (WinCloser) Wait(ps []Proc, d time.Duration) []Proc {
	deadline := time.Now().Add(d)
	var alive []Proc
	for _, p := range ps {
		h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, p.PID)
		if err != nil {
			continue // уже нет
		}
		left := time.Until(deadline)
		if left < 0 {
			left = 0
		}
		r, _ := windows.WaitForSingleObject(h, uint32(left/time.Millisecond))
		windows.CloseHandle(h)
		if r != windows.WAIT_OBJECT_0 {
			alive = append(alive, p)
		}
	}
	return alive
}

// Retarget — перенос задачи автозапуска на новую копию.
func Retarget(exe string) error { return autostart.Retarget(exe) }
