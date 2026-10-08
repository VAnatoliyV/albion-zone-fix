; Установщик «Albion Journal for Windows». Собирается на маке: makensis -DVERSION=1.2.3 installer.nsi
; (собрать.sh делает это сам после zip). Состав — та же папка dist/AlbionJournal, что и в zip.
; Автообновление zip не трогает: оно подменяет файлы в папке установки само.
Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma

!ifndef VERSION
  !error "нужен -DVERSION=1.2.3"
!endif
!ifndef ZLIST
  !error "нужен -DZLIST=файл со списком Delete для zapret (его делает собрать.sh)"
!endif
!ifndef SRC
  !define SRC "dist\AlbionJournal"
!endif
!ifndef OUTFILE
  !define OUTFILE "dist\AlbionJournalSetup-${VERSION}.exe"
!endif

!define APP "Albion Journal"
!define UNKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\AlbionJournal"
; Имя задачи автозапуска — internal/autostart.TaskName.
!define TASK "Albion Journal"

Name "${APP}"
OutFile "${OUTFILE}"
RequestExecutionLevel admin
InstallDir "$PROGRAMFILES64\${APP}"
BrandingText "${APP} ${VERSION}"
!ifndef VERSION4
  !define VERSION4 "${VERSION}.0"
!endif
VIProductVersion "${VERSION4}"
VIAddVersionKey "ProductName" "${APP}"
VIAddVersionKey "FileDescription" "${APP} (установщик)"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "CompanyName" "${APP}"
VIAddVersionKey "LegalCopyright" "${APP}"

!include "MUI2.nsh"
!include "FileFunc.nsh"
!include "LogicLib.nsh"

!define MUI_ICON "internal\desktop\rabbit.ico"
!define MUI_UNICON "internal\desktop\rabbit.ico"
!define MUI_ABORTWARNING

!insertmacro MUI_PAGE_WELCOME
!define MUI_PAGE_CUSTOMFUNCTION_LEAVE DirLeave
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "$(T_RUN)"
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchApp
!define MUI_FINISHPAGE_SHOWREADME
!define MUI_FINISHPAGE_SHOWREADME_TEXT "$(T_DESKTOP)"
!define MUI_FINISHPAGE_SHOWREADME_FUNCTION DesktopLink
!define MUI_FINISHPAGE_SHOWREADME_NOTCHECKED
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; Язык выбирается по системе; если её языка нет — первый (English).
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "Russian"

LangString T_RUN ${LANG_ENGLISH} "Run Albion Journal"
LangString T_RUN ${LANG_RUSSIAN} "Запустить Albion Journal"
LangString T_DESKTOP ${LANG_ENGLISH} "Create a desktop shortcut"
LangString T_DESKTOP ${LANG_RUSSIAN} "Создать ярлык на рабочем столе"
LangString T_CLOSE ${LANG_ENGLISH} "Albion Journal is running. Close it (tray icon -> Exit) and click Retry."
LangString T_CLOSE ${LANG_RUSSIAN} "Albion Journal сейчас запущен. Закройте его (значок в трее -> Выход) и нажмите «Повторить»."
LangString T_DATA ${LANG_ENGLISH} "Also delete settings and logs ($APPDATA\${APP})?$\r$\n$\r$\nChoose No to keep them."
LangString T_DATA ${LANG_RUSSIAN} "Удалить также настройки и журналы ($APPDATA\${APP})?$\r$\n$\r$\nВыберите «Нет», чтобы оставить их."

; Закрыть программу. Запущенный exe открыть на запись нельзя — по этому и видно,
; что он запущен (файл при этом не меняется): просим закрыть и ждём «Повторить».
; Потом гасим только наши фоновые процессы (приёмник цен и обход), чей путь
; начинается с папки установки (путь передаём переменной среды, без шаблонов).
; 64-битные процессы видны через WMI из любой разрядности PowerShell.
!macro CloseApp ID
  again_${ID}:
  IfFileExists "$INSTDIR\AlbionJournal.exe" 0 closed_${ID}
  ClearErrors
  FileOpen $0 "$INSTDIR\AlbionJournal.exe" a
  IfErrors busy_${ID}
  FileClose $0
  Goto closed_${ID}
  busy_${ID}:
  MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "$(T_CLOSE)" /SD IDCANCEL IDRETRY again_${ID}
  Abort
  closed_${ID}:
  System::Call 'kernel32::SetEnvironmentVariableW(w "AJ_DIR", w "$INSTDIR")'
  nsExec::Exec `powershell -NoProfile -NonInteractive -Command "Get-CimInstance Win32_Process -Filter \"Name='winws.exe' or Name='acp-prices.exe'\" | Where-Object { $$_.ExecutablePath -and $$_.ExecutablePath.StartsWith($$env:AJ_DIR + '\', [StringComparison]::OrdinalIgnoreCase) } | ForEach-Object { Stop-Process -Id $$_.ProcessId -Force }"`
  Pop $0
  Sleep 500
!macroend

; Папка установки всегда оканчивается на «Albion Journal»: удаление не заденет
; чужие файлы общей папки.
Function FixDir
  StrLen $1 "\${APP}"
  IntOp $1 0 - $1
  StrCpy $0 $INSTDIR "" $1
  StrCmp $0 "\${APP}" +2
  StrCpy $INSTDIR "$INSTDIR\${APP}"
FunctionEnd

Function DirLeave
  Call FixDir
FunctionEnd

Function .onInit
  SetRegView 64
  ; Обновление: та же папка, куда ставили в прошлый раз (если не задана /D=).
  ; (если $INSTDIR уже не стандартный — задан через /D= — его не трогаем)
  StrCmp $INSTDIR "$PROGRAMFILES64\${APP}" 0 +4
  ReadRegStr $0 HKLM "${UNKEY}" "InstallLocation"
  StrCmp $0 "" +2
  StrCpy $INSTDIR $0
FunctionEnd

Function LaunchApp
  SetOutPath "$INSTDIR"
  Exec '"$INSTDIR\AlbionJournal.exe"'
FunctionEnd

Function DesktopLink
  SetShellVarContext all
  SetOutPath "$INSTDIR"
  CreateShortcut "$DESKTOP\${APP}.lnk" "$INSTDIR\AlbionJournal.exe"
FunctionEnd

Section "Install"
  SetShellVarContext all
  Call FixDir
  !insertmacro CloseApp inst
  SetOutPath "$INSTDIR"
  File "${SRC}\AlbionJournal.exe"
  File "${SRC}\acp-prices.exe"
  File "${SRC}\items_by_id.json"
  File "${SRC}\README-RU.txt"
  File "${SRC}\LICENSES.txt"
  File /r "${SRC}\zapret"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateDirectory "$SMPROGRAMS\${APP}"
  CreateShortcut "$SMPROGRAMS\${APP}\${APP}.lnk" "$INSTDIR\AlbionJournal.exe"
  CreateShortcut "$SMPROGRAMS\${APP}\Uninstall ${APP}.lnk" "$INSTDIR\uninstall.exe"

  SetRegView 64
  WriteRegStr HKLM "${UNKEY}" "DisplayName" "${APP}"
  WriteRegStr HKLM "${UNKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNKEY}" "Publisher" "${APP}"
  WriteRegStr HKLM "${UNKEY}" "DisplayIcon" "$INSTDIR\AlbionJournal.exe"
  WriteRegStr HKLM "${UNKEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKLM "${UNKEY}" "InstallLocation" "$INSTDIR"
  WriteRegDWORD HKLM "${UNKEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNKEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  WriteRegDWORD HKLM "${UNKEY}" "EstimatedSize" $0
SectionEnd

Function un.onInit
  SetRegView 64
FunctionEnd

Section "Uninstall"
  SetShellVarContext all
  !insertmacro CloseApp uninst

  ; Автозапуск (задача Планировщика); если её нет — schtasks ругнётся, это не страшно.
  nsExec::Exec `"$SYSDIR\schtasks.exe" /Delete /TN "${TASK}" /F`
  Pop $0

  Delete "$INSTDIR\AlbionJournal.exe"
  Delete "$INSTDIR\acp-prices.exe"
  Delete "$INSTDIR\*.old"
  Delete "$INSTDIR\*.new"
  Delete "$INSTDIR\items_by_id.json"
  Delete "$INSTDIR\README-RU.txt"
  Delete "$INSTDIR\LICENSES.txt"
  !include "${ZLIST}"
  RMDir "$INSTDIR\zapret\bin"
  RMDir "$INSTDIR\zapret"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR" ; только если пусто: чужие файлы в папке не трогаем

  Delete "$SMPROGRAMS\${APP}\*.lnk"
  RMDir "$SMPROGRAMS\${APP}"
  Delete "$DESKTOP\${APP}.lnk"

  ; Папка обновлений: %ProgramData%\Albion Journal (при «all» $APPDATA — это ProgramData).
  RMDir /r "$APPDATA\${APP}"

  ; Настройки и журналы — %AppData% того, кто запустил удаление (при повышении
  ; прав через другую учётную запись это не его папка). По умолчанию оставляем.
  SetShellVarContext current
  MessageBox MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2 "$(T_DATA)" /SD IDNO IDNO keep_data
  RMDir /r "$APPDATA\${APP}"
  keep_data:

  SetRegView 64
  DeleteRegKey HKLM "${UNKEY}"
SectionEnd
