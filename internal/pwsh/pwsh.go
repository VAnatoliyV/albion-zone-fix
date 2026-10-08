// Пакет pwsh — запуск Windows PowerShell 5.1 для OCR и уведомлений.
//
// Раньше на каждое нажатие кнопки карточки запускался новый powershell.exe
// (старт процесса и загрузка типов WinRT — 0.7–1.5 с), и ещё раз — для
// уведомления. Теперь один «рабочий» PowerShell запускается заранее и живёт,
// пока жива программа: скрипт-цикл (WorkerScript) загружает типы WinRT и
// движки OCR один раз, а запросы приходят строками на стандартный ввод.
// Рабочий умер, завис или ответил чепухой — он убивается (в журнал), запрос
// выполняется по-старому разовым PowerShell (RunScript), а рабочий
// пересоздаётся при следующем запросе, но не чаще раза в RestartGap; после
// MaxStartFails неудачных запусков подряд — только разовый режим.
//
// Осторожность с антивирусами — как раньше: скрипт через стандартный ввод
// (`-Command -`), без -EncodedCommand и без -ExecutionPolicy Bypass.
//
// Протокол (только ASCII: ввод PowerShell читает в кодировке консоли):
//
//	запрос:  AjReq <id> '<base64 JSON запроса в UTF-8>'   и пустая строка
//	ответ:   @@AJ@@ <id> <base64 JSON ответа в UTF-8>
//
// Ответ на запрос — {"items":[…]}, элементы — те же строки, что печатают
// разовые скрипты ocr.Script и notify.Script ({"kind":"ocr",…},
// {"kind":"error","msg":…}). Готовность после запуска — ответ с id 0:
// {"ready":true} или {"error":"…"}. Строки без маркера (предупреждения,
// приглашение PowerShell) пропускаются; маркер может стоять не в начале
// строки.
package pwsh

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Timeout — сколько ждём разовый PowerShell: запуск с загрузкой WinRT —
// 1–2 с, распознавание — доли секунды. Дольше — процесс убивается.
const Timeout = 15 * time.Second

// Сроки рабочего PowerShell.
const (
	// ReadyTimeout — запуск рабочего до ответа «готов» (медленный ПК,
	// антивирус проверяет powershell.exe).
	ReadyTimeout = 20 * time.Second
	// ReqTimeout — один запрос к уже запущенному рабочему (OCR двух языков —
	// доли секунды; первое распознавание языка грузит движок).
	ReqTimeout = 8 * time.Second
	// RestartGap — пересоздавать рабочего не чаще.
	RestartGap = 30 * time.Second
	// MaxStartFails — столько неудачных запусков подряд, и рабочий больше
	// не запускается (до перезапуска программы).
	MaxStartFails = 3
)

// ErrUnsupported — PowerShell есть только в Windows.
var ErrUnsupported = errors.New("PowerShell есть только в Windows")

// Stdin — скрипт для `powershell -Command -`. PowerShell читает ввод
// построчно, как с клавиатуры: многострочная конструкция (try/catch)
// завершается пустой строкой, поэтому в конце их две. Ввод читается в
// кодировке консоли (OEM), поэтому скрипты — только ASCII (тест).
func Stdin(script string) string {
	return strings.ReplaceAll(script, "\r\n", "\n") + "\n\n"
}

// Request — запрос рабочему PowerShell.
type Request struct {
	Cmd   string   `json:"cmd"`             // ocr, langs, toast
	Path  string   `json:"path,omitempty"`  // ocr: PNG
	Langs []string `json:"langs,omitempty"` // ocr: теги языков
	App   string   `json:"app,omitempty"`   // toast: AUMID
	XML   string   `json:"xml,omitempty"`   // toast: XML уведомления
}

const marker = "@@AJ@@ "

// encodeRequest — строка для стандартного ввода рабочего (только ASCII).
// После команды — пустая строка: PowerShell точно выполнит введённое.
func encodeRequest(id int, r Request) (string, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("AjReq %d '%s'\n\n", id, base64.StdEncoding.EncodeToString(b)), nil
}

// errCorrupt — ответ с маркером, но испорченный.
var errCorrupt = errors.New("испорченный ответ")

// parseReply разбирает строку вывода рабочего. ok=false — строка без
// маркера (посторонний вывод, пропустить); err — маркер есть, а ответ
// испорчен.
func parseReply(line string) (id int, body []byte, ok bool, err error) {
	i := strings.Index(line, marker)
	if i < 0 {
		return 0, nil, false, nil
	}
	f := strings.Fields(line[i+len(marker):])
	if len(f) != 2 {
		return 0, nil, true, errCorrupt
	}
	id, err = strconv.Atoi(f[0])
	if err != nil {
		return 0, nil, true, errCorrupt
	}
	body, err = base64.StdEncoding.DecodeString(f[1])
	if err != nil || !json.Valid(body) {
		return id, nil, true, errCorrupt
	}
	return id, body, true, nil
}

// readyReply — ответ рабочего после запуска.
func readyReply(body []byte) error {
	var r struct {
		Ready bool   `json:"ready"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return errCorrupt
	}
	if r.Error != "" {
		return errors.New(r.Error)
	}
	if !r.Ready {
		return errCorrupt
	}
	return nil
}

// itemsOut — элементы ответа строками JSON, как вывод разового скрипта
// (его разбирает ocr.Parse). ConvertTo-Json в PowerShell 5.1 иногда
// заворачивает массив в {"value":[…],"Count":n}, а пустой отдаёт null —
// принимаем всё.
func itemsOut(body []byte) ([]byte, error) {
	var r struct {
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, errCorrupt
	}
	raw := bytes.TrimSpace(r.Items)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if raw[0] == '{' {
		var w struct {
			Value json.RawMessage `json:"value"`
		}
		if json.Unmarshal(raw, &w) != nil || len(w.Value) == 0 {
			// один элемент не массивом
			raw = append(append([]byte("["), raw...), ']')
		} else {
			raw = w.Value
		}
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, errCorrupt
	}
	var out bytes.Buffer
	for _, it := range items {
		if err := json.Compact(&out, it); err != nil {
			return nil, errCorrupt
		}
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

// ErrorMsg — текст элемента {"kind":"error"} в выводе; "" — ошибки нет.
func ErrorMsg(out []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var j struct {
			Kind string `json:"kind"`
			Msg  string `json:"msg"`
		}
		if json.Unmarshal(sc.Bytes(), &j) == nil && j.Kind == "error" {
			if j.Msg == "" {
				return "ошибка PowerShell"
			}
			return j.Msg
		}
	}
	return ""
}
