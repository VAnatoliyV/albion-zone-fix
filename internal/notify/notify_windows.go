//go:build windows

package notify

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"albionzonefix/internal/ocr"

	"golang.org/x/sys/windows/registry"
)

const script = `$ErrorActionPreference = 'Stop'
$null = [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
$null = [Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime]
$x = New-Object Windows.Data.Xml.Dom.XmlDocument
$x.LoadXml($env:AJ_TOAST_XML)
$t = New-Object Windows.UI.Notifications.ToastNotification $x
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($env:AJ_TOAST_APP).Show($t)
`

var (
	mu    sync.Mutex
	appID = PowerShellAppID
	logf  = func(string, ...any) {}
	// Уведомления по одному: два PowerShell сразу — лишняя нагрузка посреди игры.
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
	out, err := ocr.RunScript(context.Background(), script, []string{"AJ_TOAST_XML=" + XML(title, subtitle, body), "AJ_TOAST_APP=" + id})
	if err != nil {
		logf("уведомление не показано: %v %s", err, string(out))
	}
}
