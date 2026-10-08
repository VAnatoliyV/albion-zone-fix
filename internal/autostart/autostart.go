// Пакет autostart — запуск Albion Journal вместе с Windows.
//
// Почему задача Планировщика, а не ключ HKCU\...\Run: программе нужны права
// администратора (драйвер WinDivert). Из ключа Run Windows поднимает
// программы с обычными правами, и та при каждом входе в систему
// перезапускалась бы через окно UAC (а без согласия — не работала бы вовсе).
// Задача Планировщика с «наивысшими правами» (RunLevel HighestAvailable)
// запускает программу сразу с правами администратора и без вопроса — так
// делают все программы, которым при старте нужны такие права.
//
// Создаётся задача через schtasks.exe /Create /XML: только в XML можно
// снять ограничение времени работы (по умолчанию Планировщик убивает
// задачу через 72 часа) и запуск «только от сети» на ноутбуке.
// Программа сама работает с правами администратора, поэтому создать такую
// задачу ей можно без дополнительного запроса.
package autostart

import (
	"bytes"
	"encoding/xml"
	"errors"
	"strings"
	"unicode/utf16"
)

// TaskName — имя задачи в Планировщике (корневая папка).
const TaskName = "Albion Journal"

// Flag — ключ запуска, с которым программу поднимает задача: окно не
// показывается, программа сразу уходит в трей.
const Flag = "-autostart"

// WatchFlag — ключ запуска сторожа игры (internal/gameguard): при входе в
// Windows задача поднимает не программу, а маленького сторожа, если
// «Запускать вместе с Windows» выключен, а «вместе с игрой» включено.
const WatchFlag = "-watch-game"

// Mode — что задача запускает при входе в Windows.
type Mode int

const (
	Off   Mode = iota // задачи нет
	App               // программа (-autostart)
	Watch             // сторож игры (-watch-game)
)

func (m Mode) String() string {
	switch m {
	case App:
		return "программа"
	case Watch:
		return "сторож игры"
	}
	return "выключен"
}

// Args — аргументы программы в задаче.
func (m Mode) Args() string {
	if m == Watch {
		return WatchFlag
	}
	return Flag
}

// ModeFor — задача по настройкам. Запуск с Windows важнее сторожа: поднятая
// программа сама следит за игрой, второй процесс ни к чему.
func ModeFor(withWindows, watchGame bool) Mode {
	switch {
	case withWindows:
		return App
	case watchGame:
		return Watch
	}
	return Off
}

// ModeOf — режим существующей задачи по её аргументам.
func ModeOf(args string) Mode {
	if strings.TrimSpace(args) == WatchFlag {
		return Watch
	}
	return App
}

// ErrUnsupported — автозапуск есть только в Windows.
var ErrUnsupported = errors.New("автозапуск есть только в Windows")

// Task — что запускать и от чьего имени.
type Task struct {
	Exe    string // полный путь к AlbionJournal.exe
	Dir    string // рабочая папка (папка программы)
	UserID string // DOMAIN\user — чей вход в систему запускает задачу
	Args   string // аргументы; пусто — Flag (программа в трей)
}

func esc(s string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// XML — описание задачи для schtasks /Create /XML.
func (t Task) XML() string {
	args := t.Args
	if args == "" {
		args = Flag
	}
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Albion Journal: сбор цен, счётчик фейма и Zone Fix. Запуск при входе в Windows.</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>` + esc(t.UserID) + `</UserId>
      <Delay>PT15S</Delay>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + esc(t.UserID) + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>false</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>4</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + esc(t.Exe) + `</Command>
      <Arguments>` + esc(args) + `</Arguments>
      <WorkingDirectory>` + esc(t.Dir) + `</WorkingDirectory>
    </Exec>
  </Actions>
</Task>
`
}

// UTF16 — файл для schtasks: UTF-16 LE с меткой порядка байтов. Другую
// кодировку schtasks читает через раз, а кириллица в пути ломается.
func UTF16(s string) []byte {
	u := utf16.Encode([]rune(strings.ReplaceAll(s, "\n", "\r\n")))
	out := make([]byte, 2, 2+2*len(u))
	out[0], out[1] = 0xFF, 0xFE
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8))
	}
	return out
}

// CreateArgs — аргументы schtasks для создания (с заменой) задачи из файла.
func CreateArgs(xmlPath string) []string {
	return []string{"/Create", "/TN", TaskName, "/XML", xmlPath, "/F"}
}

// DeleteArgs — аргументы schtasks для удаления задачи.
func DeleteArgs() []string { return []string{"/Delete", "/TN", TaskName, "/F"} }

// RunArgs — аргументы schtasks для запуска задачи сейчас (с её правами:
// так сторож без прав администратора поднимает программу без окна UAC).
func RunArgs() []string { return []string{"/Run", "/TN", TaskName} }

// QueryArgs — аргументы schtasks для чтения задачи в XML.
func QueryArgs() []string { return []string{"/Query", "/TN", TaskName, "/XML"} }

// UserOf достаёт из XML задачи, от чьего имени она запускается (UserId
// раздела Principals, иначе первый UserId) — чтобы при переносе задачи на
// другую папку не сменить пользователя (установщик мог поднять права через
// другую учётную запись).
func UserOf(taskXML string) string {
	s := taskXML
	if i := strings.Index(s, "<Principals>"); i >= 0 {
		s = s[i:]
	}
	return tagText(s, "UserId")
}

// tagText — текст первого <tag>…</tag> (с разбором экранирования XML).
func tagText(s, tag string) string {
	i := strings.Index(s, "<"+tag+">")
	if i < 0 {
		return ""
	}
	s = s[i+len(tag)+2:]
	j := strings.Index(s, "</"+tag+">")
	if j < 0 {
		return ""
	}
	var v struct {
		S string `xml:",chardata"`
	}
	if xml.Unmarshal([]byte("<c>"+s[:j]+"</c>"), &v) != nil {
		return ""
	}
	return strings.TrimSpace(v.S)
}

// CommandOf достаёт из XML задачи путь к программе — чтобы понять, не
// переехала ли папка с программой с тех пор, как задачу создали.
func CommandOf(taskXML string) string {
	return strings.Trim(tagText(taskXML, "Command"), `"`)
}

// ArgsOf — аргументы программы из XML задачи.
func ArgsOf(taskXML string) string { return tagText(taskXML, "Arguments") }
