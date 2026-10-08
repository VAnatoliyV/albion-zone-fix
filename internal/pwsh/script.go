package pwsh

// WorkerScript — рабочий PowerShell (передаётся первыми строками ввода,
// см. Stdin). Определяет функции и загружает типы WinRT один раз, отвечает
// маркером готовности (id 0); дальше PowerShell читает со стандартного
// ввода строки «AjReq <id> '<base64>'» и выполняет их как команды. Движки
// OCR по языкам кэшируются. Команды:
//
//	langs — установленные языки OCR: {"kind":"lang","tag":…}
//	ocr   — распознать path на языках langs: {"kind":"ocr",…}/{"kind":"skip",…}
//	toast — показать уведомление xml от имени app
//
// Ошибка — элемент {"kind":"error","msg":…}. То же, что делают разовые
// ocr.Script и notify.Script. Только ASCII и без пустых строк внутри
// (пустая строка завершает ввод многострочной конструкции) — тест.
const WorkerScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
function AjSend($id, $obj) {
  $j = ConvertTo-Json -InputObject $obj -Compress -Depth 6
  $b = [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes([string]$j))
  [Console]::Out.WriteLine('@@AJ@@ ' + [string]$id + ' ' + $b)
  [Console]::Out.Flush()
}
function AjAwait($op, [Type]$type) {
  $t = $global:AjAsTask.MakeGenericMethod($type).Invoke($null, @($op))
  $null = $t.Wait(-1)
  $t.Result
}
function AjOcr($r, $items) {
  $file = AjAwait ([Windows.Storage.StorageFile]::GetFileFromPathAsync([string]$r.path)) ([Windows.Storage.StorageFile])
  $stream = AjAwait ($file.OpenAsync([Windows.Storage.FileAccessMode]::Read)) ([Windows.Storage.Streams.IRandomAccessStream])
  try {
    $dec = AjAwait ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
    $bmp = AjAwait ($dec.GetSoftwareBitmapAsync()) ([Windows.Graphics.Imaging.SoftwareBitmap])
    try {
      foreach ($tag in @($r.langs)) {
        $tag = [string]$tag
        if (-not $tag) { continue }
        $eng = $global:AjEngines[$tag]
        if ($null -eq $eng) {
          $lang = [Windows.Globalization.Language]::new($tag)
          if (-not [Windows.Media.Ocr.OcrEngine]::IsLanguageSupported($lang)) { $null = $items.Add(@{kind='skip'; lang=$tag}); continue }
          $eng = [Windows.Media.Ocr.OcrEngine]::TryCreateFromLanguage($lang)
          if ($null -eq $eng) { throw ('no OCR engine for ' + $tag) }
          $global:AjEngines[$tag] = $eng
        }
        $res = AjAwait ($eng.RecognizeAsync($bmp)) ([Windows.Media.Ocr.OcrResult])
        $lines = New-Object 'System.Collections.Generic.List[string]'
        foreach ($ln in $res.Lines) { $lines.Add([string]$ln.Text) }
        $null = $items.Add(@{kind='ocr'; lang=$tag; lines=$lines.ToArray()})
      }
    } finally {
      if ($bmp -is [System.IDisposable]) { $bmp.Dispose() }
    }
  } finally {
    $stream.Dispose()
  }
}
function AjToastTypes() {
  $null = [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
  $null = [Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime]
}
function AjToast($r) {
  AjToastTypes
  $x = New-Object Windows.Data.Xml.Dom.XmlDocument
  $x.LoadXml([string]$r.xml)
  $t = New-Object Windows.UI.Notifications.ToastNotification $x
  [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier([string]$r.app).Show($t)
}
function AjReq([int]$id, [string]$b64) {
  $items = New-Object System.Collections.ArrayList
  try {
    $r = [System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($b64)) | ConvertFrom-Json
    switch ([string]$r.cmd) {
      'langs' { foreach ($l in [Windows.Media.Ocr.OcrEngine]::AvailableRecognizerLanguages) { $null = $items.Add(@{kind='lang'; tag=$l.LanguageTag}) } }
      'ocr' { AjOcr $r $items }
      'toast' { AjToast $r }
      default { throw ('unknown command ' + [string]$r.cmd) }
    }
  } catch {
    $null = $items.Add(@{kind='error'; msg=[string]$_.Exception.Message})
  }
  AjSend $id @{items=$items}
}
try {
  Add-Type -AssemblyName System.Runtime.WindowsRuntime
  $null = [Windows.Media.Ocr.OcrEngine, Windows.Foundation, ContentType = WindowsRuntime]
  $null = [Windows.Globalization.Language, Windows.Globalization, ContentType = WindowsRuntime]
  $null = [Windows.Storage.StorageFile, Windows.Storage, ContentType = WindowsRuntime]
  $null = [Windows.Graphics.Imaging.BitmapDecoder, Windows.Graphics, ContentType = WindowsRuntime]
  $null = [Windows.Graphics.Imaging.SoftwareBitmap, Windows.Graphics, ContentType = WindowsRuntime]
  $global:AjAsTask = [System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1' } | Select-Object -First 1
  $global:AjEngines = @{}
  try { AjToastTypes } catch { }
  AjSend 0 @{ready=$true}
} catch {
  AjSend 0 @{error=[string]$_.Exception.Message}
  exit 1
}
`
