// Пакет notify — уведомления Windows (toast): карточка зоны и «сервер зоны
// не отвечает». Панели поверх игры нет (запрет SBI): баннер рисует сама
// Windows, как уведомление любой программы.
//
// Как Windows узнаёт, чьё уведомление (AppUserModelID). Программа без
// пакета MSIX должна зарегистрировать свой AUMID, иначе Windows 10/11
// уведомление молча не показывает. Выбрано то же, что делает Microsoft
// Toolkit (ToastNotificationManagerCompat) для обычных программ: ключ
// HKCU\Software\Classes\AppUserModelId\<AppID> с DisplayName и IconUri —
// без ярлыка и без COM. Ставит его сама программа при запуске (а не
// установщик): установщик NSIS без сторонних плагинов не умеет записать
// AUMID в ярлык, а ключ в реестре работает и для zip без установки.
// Не вышло записать ключ или Windows не приняла наш AUMID при показе —
// уведомление повторяется от имени Windows PowerShell (его AUMID
// зарегистрирован всегда), это видно в журнале.
//
// Здесь — XML уведомления и значок (проверяется на маке); показ — в
// notify_windows.go через PowerShell (WinRT), как OCR.
package notify

import (
	"bytes"
	"encoding/binary"
	"strings"
)

// Script — показать уведомление: XML и AUMID — в переменных среды
// AJ_TOAST_XML и AJ_TOAST_APP (через стандартный ввод PowerShell, как OCR;
// только ASCII). Ошибка WinRT (AUMID не принят) — код выхода не 0.
const Script = `$ErrorActionPreference = 'Stop'
try {
  $null = [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
  $null = [Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime]
  $x = New-Object Windows.Data.Xml.Dom.XmlDocument
  $x.LoadXml($env:AJ_TOAST_XML)
  $t = New-Object Windows.UI.Notifications.ToastNotification $x
  [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($env:AJ_TOAST_APP).Show($t)
} catch {
  [Console]::Out.WriteLine([string]$_.Exception.Message)
  exit 1
}
`

// AppID — AppUserModelID программы.
const AppID = "VAnatoliyV.AlbionJournal"

// PowerShellAppID — запасной AUMID: Windows PowerShell, есть всегда.
const PowerShellAppID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

// IconFile — значок уведомления в каталоге данных (PNG из rabbit.ico).
const IconFile = "toast-icon.png"

func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			// управляющие символы в XML недопустимы (кроме перевода строки)
			if r < 0x20 && r != '\n' && r != '\t' {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// XML — уведомление ToastGeneric: заголовок, подзаголовок, тело (строки
// через \n). Пустые части пропускаются. Без звука — как у мака (звук не
// задан).
func XML(title, subtitle, body string) string {
	var b strings.Builder
	b.WriteString(`<toast><visual><binding template="ToastGeneric">`)
	for _, t := range []string{title, subtitle, body} {
		if strings.TrimSpace(t) == "" {
			continue
		}
		b.WriteString("<text>" + esc(t) + "</text>")
	}
	b.WriteString(`</binding></visual><audio silent="true"/></toast>`)
	return b.String()
}

// PNGFromICO — самая большая PNG-картинка внутри .ico (наш кролик хранит
// размеры 16…256 как PNG). nil — PNG внутри нет.
func PNGFromICO(ico []byte) []byte {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		return nil
	}
	n := int(binary.LittleEndian.Uint16(ico[4:]))
	var best []byte
	bestW := -1
	for i := 0; i < n; i++ {
		e := 6 + 16*i
		if e+16 > len(ico) {
			break
		}
		w := int(ico[e])
		if w == 0 {
			w = 256
		}
		size := int(binary.LittleEndian.Uint32(ico[e+8:]))
		off := int(binary.LittleEndian.Uint32(ico[e+12:]))
		if off < 0 || size <= 0 || off+size > len(ico) {
			continue
		}
		data := ico[off : off+size]
		if !bytes.HasPrefix(data, []byte("\x89PNG")) {
			continue
		}
		if w > bestW {
			best, bestW = data, w
		}
	}
	return best
}
