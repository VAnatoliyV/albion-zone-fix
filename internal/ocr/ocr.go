// Пакет ocr — распознавание текста встроенным OCR Windows
// (Windows.Media.Ocr) через PowerShell 5.1, который есть в каждой Windows
// 10/11. Без CGO и без Tesseract: скрипт зашит в программу и передаётся
// PowerShell через стандартный ввод (`-Command -`): не файлом — программа
// работает с правами администратора, а файл в папке пользователя мог бы
// подменить кто угодно; и не -EncodedCommand с -ExecutionPolicy Bypass —
// так выглядят вредоносные загрузчики, и антивирусы на это злятся.
// Политика выполнения касается только файлов скриптов, ввод команд она не
// ограничивает. Параметры — через переменные среды.
//
// Здесь — скрипт и разбор его вывода (проверяется на маке); запуск — в
// ocr_windows.go.
package ocr

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Timeout — сколько ждём PowerShell: запуск с загрузкой WinRT — 1–2 с,
// распознавание двух языков — доли секунды. Дольше — процесс убивается.
const Timeout = 15 * time.Second

// ErrUnsupported — OCR есть только на Windows.
var ErrUnsupported = errors.New("распознавание текста есть только в Windows")

// Script — PowerShell: без AJ_OCR_PATH печатает установленные языки OCR;
// с ним — распознаёт PNG на каждом языке из AJ_OCR_LANGS (через запятую).
// Каждая строка вывода — JSON:
//
//	{"kind":"lang","tag":"ru-RU"}
//	{"kind":"ocr","lang":"ru-RU","lines":["Путь Авалона в", …]}
//	{"kind":"skip","lang":"ru-RU"}          — язык не установлен
//	{"kind":"error","msg":"…"}
const Script = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
function Emit($o) { [Console]::Out.WriteLine(($o | ConvertTo-Json -Compress -Depth 4)) }
try {
  Add-Type -AssemblyName System.Runtime.WindowsRuntime
  $null = [Windows.Media.Ocr.OcrEngine, Windows.Foundation, ContentType = WindowsRuntime]
  $null = [Windows.Globalization.Language, Windows.Globalization, ContentType = WindowsRuntime]
  if (-not $env:AJ_OCR_PATH) {
    foreach ($l in [Windows.Media.Ocr.OcrEngine]::AvailableRecognizerLanguages) { Emit @{kind='lang'; tag=$l.LanguageTag} }
    exit 0
  }
  $null = [Windows.Storage.StorageFile, Windows.Storage, ContentType = WindowsRuntime]
  $null = [Windows.Graphics.Imaging.BitmapDecoder, Windows.Graphics, ContentType = WindowsRuntime]
  $null = [Windows.Graphics.Imaging.SoftwareBitmap, Windows.Graphics, ContentType = WindowsRuntime]
  $asTask = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1' } | Select-Object -First 1
  function Await($op, [Type]$type) {
    $t = $asTask.MakeGenericMethod($type).Invoke($null, @($op))
    $null = $t.Wait(-1)
    $t.Result
  }
  $file = Await ([Windows.Storage.StorageFile]::GetFileFromPathAsync($env:AJ_OCR_PATH)) ([Windows.Storage.StorageFile])
  $stream = Await ($file.OpenAsync([Windows.Storage.FileAccessMode]::Read)) ([Windows.Storage.Streams.IRandomAccessStream])
  $dec = Await ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
  $bmp = Await ($dec.GetSoftwareBitmapAsync()) ([Windows.Graphics.Imaging.SoftwareBitmap])
  foreach ($tag in ($env:AJ_OCR_LANGS -split ',')) {
    if (-not $tag) { continue }
    $lang = [Windows.Globalization.Language]::new($tag)
    if (-not [Windows.Media.Ocr.OcrEngine]::IsLanguageSupported($lang)) { Emit @{kind='skip'; lang=$tag}; continue }
    $eng = [Windows.Media.Ocr.OcrEngine]::TryCreateFromLanguage($lang)
    $res = Await ($eng.RecognizeAsync($bmp)) ([Windows.Media.Ocr.OcrResult])
    $lines = @()
    foreach ($ln in $res.Lines) { $lines += [string]$ln.Text }
    Emit @{kind='ocr'; lang=$tag; lines=$lines}
  }
  $stream.Dispose()
} catch {
  Emit @{kind='error'; msg=[string]$_.Exception.Message}
  exit 1
}
`

// Stdin — скрипт для `powershell -Command -`. PowerShell читает ввод
// построчно, как с клавиатуры: многострочная конструкция (try/catch)
// завершается пустой строкой, поэтому в конце их две. Ввод читается в
// кодировке консоли (OEM), поэтому скрипты — только ASCII (тест).
func Stdin(script string) string {
	return strings.ReplaceAll(script, "\r\n", "\n") + "\n\n"
}

// Output — разобранный вывод скрипта.
type Output struct {
	Langs   []string            // установленные языки OCR (режим списка)
	Lines   map[string][]string // язык → строки
	Skipped []string            // языки, которых нет
	Err     string              // ошибка скрипта
}

// Parse разбирает вывод скрипта. Посторонние строки (предупреждения
// PowerShell) пропускаются; BOM в начале — тоже.
func Parse(out []byte) Output {
	o := Output{Lines: map[string][]string{}}
	out = bytes.TrimPrefix(out, []byte("\xef\xbb\xbf"))
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var j struct {
			Kind  string          `json:"kind"`
			Tag   string          `json:"tag"`
			Lang  string          `json:"lang"`
			Lines json.RawMessage `json:"lines"`
			Msg   string          `json:"msg"`
		}
		if json.Unmarshal([]byte(line), &j) != nil {
			continue
		}
		switch j.Kind {
		case "lang":
			if j.Tag != "" {
				o.Langs = append(o.Langs, j.Tag)
			}
		case "ocr":
			o.Lines[j.Lang] = parseLines(j.Lines)
		case "skip":
			o.Skipped = append(o.Skipped, j.Lang)
		case "error":
			o.Err = j.Msg
		}
	}
	return o
}

// parseLines: ConvertTo-Json в PowerShell 5.1 может отдать одну строку не
// массивом, а строкой, и null вместо пустого массива — принимаем всё.
func parseLines(raw json.RawMessage) []string {
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		return arr
	}
	var one string
	if json.Unmarshal(raw, &one) == nil && one != "" {
		return []string{one}
	}
	return nil
}

// Pick — какие языки OCR пробовать: русский и английский, если они
// установлены (теги вроде «ru-RU», «en-US»). Язык клиента игры неизвестен,
// поэтому распознаём на обоих, а тултип выберет zonecard.Choose. Список
// установленных пуст (не проверяли) — пробуем ru-RU и en-US наудачу.
func Pick(installed []string) []string {
	if len(installed) == 0 {
		return []string{"ru-RU", "en-US"}
	}
	var out []string
	for _, want := range []string{"ru", "en"} {
		for _, tag := range installed {
			if strings.EqualFold(strings.SplitN(tag, "-", 2)[0], want) {
				out = append(out, tag)
				break
			}
		}
	}
	return out
}

// Подсказки о языках OCR (коды для страницы, ocr.hint.*).
const (
	HintNone  = "none"  // ни русского, ни английского: распознавать нечем
	HintNoRu  = "noRu"  // нет русского: русский клиент игры не прочитается
	HintNoEn  = "noEn"  // нет английского: английский клиент не прочитается
	HintCheck = "check" // проверить не удалось
)

// Hint — чего не хватает среди установленных языков OCR; "" — всё есть.
func Hint(installed []string) string {
	ru, en := false, false
	for _, t := range installed {
		switch strings.ToLower(strings.SplitN(t, "-", 2)[0]) {
		case "ru":
			ru = true
		case "en":
			en = true
		}
	}
	switch {
	case !ru && !en:
		return HintNone
	case !ru:
		return HintNoRu
	case !en:
		return HintNoEn
	}
	return ""
}
