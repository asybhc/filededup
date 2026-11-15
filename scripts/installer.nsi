; FileDedup Go 安装包脚本（NSIS 3.x）。
;
; 约定：
;   - 只读写 HKCU，不需要管理员权限（RequestExecutionLevel user）。
;   - 安装到 %LOCALAPPDATA%\Programs\FileDedupGo，与 C# 版 FileDedupTool
;     的安装目录、注册表项、快捷方式、可执行文件名全部错开，可共存。
;   - 卸载只删自己写下的东西；%AppData%\FileDedup（设置、许可证）与
;     %AppData%\FileDedupTool（与 C# 版共用的试用记录）一律保留。
;   - 版本号由打包脚本通过 /DAPP_VERSION=... 注入，避免与代码里的版本漂移。
;
; 用法（不要直接双击）：
;   makensis /DAPP_VERSION=0.7.0 /DAPP_VERSION4=0.7.0.0 ^
;            /DSRC_EXE=<绝对路径>\fdedup-ui.exe /DOUT_FILE=<绝对路径>\Setup.exe scripts\installer.nsi

Unicode true

!include "MUI2.nsh"
!include "FileFunc.nsh"

!ifndef APP_VERSION
  !define APP_VERSION "0.7.0"
!endif
!ifndef APP_VERSION4
  !define APP_VERSION4 "0.7.0.0"
!endif
!ifndef SRC_EXE
  !define SRC_EXE "..\bin\fdedup-ui.exe"
!endif
!ifndef OUT_FILE
  !define OUT_FILE "..\bin\FileDedupGo-Setup-${APP_VERSION}.exe"
!endif

; 与 C# 版（FileDedupTool / 文件去重）明确区分：名称、安装目录、注册表键、exe 名都不同。
!define APP_NAME "文件去重 Go"
!define APP_ID "FileDedupGo"
!define APP_EXE "fdedup-ui.exe"
!define APP_PUBLISHER "FileDedup"
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_ID}"
!define APP_REG_KEY "Software\${APP_ID}"

Name "${APP_NAME} ${APP_VERSION}"
OutFile "${OUT_FILE}"
InstallDir "$LOCALAPPDATA\Programs\${APP_ID}"
InstallDirRegKey HKCU "${APP_REG_KEY}" "InstallDir"
RequestExecutionLevel user
SetCompressor /SOLID lzma
ShowInstDetails show
ShowUninstDetails show

VIProductVersion "${APP_VERSION4}"
VIAddVersionKey "ProductName" "${APP_NAME}"
VIAddVersionKey "ProductVersion" "${APP_VERSION}"
VIAddVersionKey "FileVersion" "${APP_VERSION}"
VIAddVersionKey "CompanyName" "${APP_PUBLISHER}"
VIAddVersionKey "FileDescription" "${APP_NAME} 安装程序"
VIAddVersionKey "LegalCopyright" ""

!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\${APP_EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "立即启动 ${APP_NAME}"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "SimpChinese"

Section "主程序" SecMain
  SetOutPath "$INSTDIR"
  File "${SRC_EXE}"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateDirectory "$SMPROGRAMS\${APP_NAME}"
  CreateShortcut "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk" "$INSTDIR\${APP_EXE}"
  CreateShortcut "$SMPROGRAMS\${APP_NAME}\卸载 ${APP_NAME}.lnk" "$INSTDIR\uninstall.exe"
  CreateShortcut "$DESKTOP\${APP_NAME}.lnk" "$INSTDIR\${APP_EXE}"

  WriteRegStr HKCU "${APP_REG_KEY}" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "${APP_REG_KEY}" "Version" "${APP_VERSION}"

  WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${APP_NAME}"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${APP_VERSION}"
  WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${APP_PUBLISHER}"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${APP_EXE}"
  WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1

  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"
SectionEnd

Section "Uninstall"
  Delete "$INSTDIR\${APP_EXE}"
  Delete "$INSTDIR\uninstall.exe"
  RMDir /r "$INSTDIR"

  Delete "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk"
  Delete "$SMPROGRAMS\${APP_NAME}\卸载 ${APP_NAME}.lnk"
  RMDir "$SMPROGRAMS\${APP_NAME}"
  Delete "$DESKTOP\${APP_NAME}.lnk"

  DeleteRegKey HKCU "${UNINST_KEY}"
  DeleteRegKey HKCU "${APP_REG_KEY}"

  ; 有意保留的用户数据（不属于安装目录）：
  ;   %AppData%\FileDedup\ui-settings.json、license.fdlic
  ;   %AppData%\FileDedupTool\trial.dat（与 C# 版共用，删掉等于帮用户重置试用）
SectionEnd
