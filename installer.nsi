; Установщик «Albion Journal for Windows». Собирается на маке: makensis -DVERSION=1.2.3 installer.nsi
; (собрать.sh делает это сам после zip). Состав — та же папка dist/AlbionJournal, что и в zip.
; Автообновление zip не трогает: оно подменяет файлы в папке установки само.
Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma

!ifndef VERSION
  !error "нужен -DVERSION=1.2.3"
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
InstallDirRegKey HKLM "${UNKEY}" "InstallLocation"
BrandingText "${APP} ${VERSION}"
VIProductVersion "${VERSION}.0.0"
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
LangString T_UNLINK ${LANG_ENGLISH} "Albion Journal Uninstall"
LangString T_UNLINK ${LANG_RUSSIAN} "Удалить Albion Journal"
LangString T_DATA ${LANG_ENGLISH} "Also delete settings and logs ($APPDATA\${APP})?$\r$\n$\r$\nChoose No to keep them."
LangString T_DATA ${LANG_RUSSIAN} "Удалить также настройки и журналы ($APPDATA\${APP})?$\r$\n$\r$\nВыберите «Нет», чтобы оставить их."

; Закрыть программу. Работающий exe Windows удалить не даёт — по этому и видно,
; что он запущен: просим закрыть и ждём «Повторить». Потом гасим только наши
; фоновые процессы из папки установки (приёмник цен и обход), чужие не трогаем.
!macro CloseApp ID
  again_${ID}:
  ClearErrors
  Delete "$INSTDIR\AlbionJournal.exe"
  IfErrors 0 closed_${ID}
  MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "$(T_CLOSE)" /SD IDCANCEL IDRETRY again_${ID}
  Abort
  closed_${ID}:
  nsExec::Exec `powershell -NoProfile -NonInteractive -Command "Get-Process acp-prices,winws -ErrorAction SilentlyContinue | Where-Object { $$_.Path -like '$INSTDIR\*' } | Stop-Process -Force"`
  Pop $0
  Sleep 500
!macroend

Function .onInit
  SetRegView 64
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
  CreateShortcut "$SMPROGRAMS\${APP}\$(T_UNLINK).lnk" "$INSTDIR\uninstall.exe"

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
  Delete "$INSTDIR\AlbionJournal.exe.new"
  Delete "$INSTDIR\acp-prices.exe"
  Delete "$INSTDIR\acp-prices.exe.new"
  Delete "$INSTDIR\items_by_id.json"
  Delete "$INSTDIR\README-RU.txt"
  Delete "$INSTDIR\LICENSES.txt"
  RMDir /r "$INSTDIR\zapret"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR" ; только если пусто: чужие файлы в папке не трогаем

  Delete "$SMPROGRAMS\${APP}\${APP}.lnk"
  Delete "$SMPROGRAMS\${APP}\$(T_UNLINK).lnk"
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
