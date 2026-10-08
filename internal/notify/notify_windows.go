//go:build windows

package notify

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"albionzonefix/internal/pwsh"

	"golang.org/x/sys/windows/registry"
)

var (
	mu    sync.Mutex
	appID = PowerShellAppID
	logf  = func(string, ...any) {}
	// Уведомления по одному (рабочий PowerShell и так берёт запросы по одному).
	queue = make(chan [3]string, 4)
	start sync.Once
)

// Init регистрирует AUMID программы (HKCU\Software\Classes\AppUserModelId)
// со значком из icon (.ico) в каталоге данных.
func Init(dataDir string, icon []byte, lf func(string, ...any)) {
	if lf != nil {
		logf = lf
	}
	iconPath := ""
	if png := PNGFromICO(icon); png != nil {
		p := filepath.Join(dataDir, IconFile)
		if os.WriteFile(p, png, 0644) == nil {
			iconPath = p
		}
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\AppUserModelId\`+AppID, registry.SET_VALUE)
	if err != nil {
		logf("уведомления: AUMID не записан (%v) — будут от имени Windows PowerShell", err)
		return
	}
	defer k.Close()
	if err := k.SetStringValue("DisplayName", "Albion Journal"); err != nil {
		logf("уведомления: AUMID не записан (%v) — будут от имени Windows PowerShell", err)
		return
	}
	if iconPath != "" {
		k.SetStringValue("IconUri", iconPath)
	}
	mu.Lock()
	appID = AppID
	mu.Unlock()
}

// Show показывает уведомление; не ждёт (очередь в своей горутине). Очередь
// полна — уведомление пропадает (в журнал).
func Show(title, subtitle, body string) {
	start.Do(func() {
		go func() {
			for n := range queue {
				show(n[0], n[1], n[2])
			}
		}()
	})
	select {
	case queue <- [3]string{title, subtitle, body}:
	default:
		logf("уведомление пропущено: очередь полна (%s)", title)
	}
}

func show(title, subtitle, body string) {
	mu.Lock()
	id := appID
	mu.Unlock()
	x := XML(title, subtitle, body)
	t0 := time.Now()
	how := ""
	run := func(app string) error {
		out, h, err := pwsh.Call(context.Background(), pwsh.Request{Cmd: "toast", App: app, XML: x},
			Script, []string{"AJ_TOAST_XML=" + x, "AJ_TOAST_APP=" + app})
		how = h
		if err != nil {
			if h == "разовый" {
				return fmt.Errorf("%v %s", err, strings.TrimSpace(string(out)))
			}
			return err
		}
		return nil
	}
	err := run(id)
	if err != nil && id != PowerShellAppID {
		// Свой AUMID не сработал — ещё раз от имени Windows PowerShell.
		logf("уведомление от %s не показано (%v), пробую от имени Windows PowerShell", id, err)
		err = run(PowerShellAppID)
	}
	if err != nil {
		logf("уведомление не показано: %v", err)
		return
	}
	logf("уведомление показано за %v (%s PowerShell)", time.Since(t0).Round(time.Millisecond), how)
}
