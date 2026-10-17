; Skiff installer for Windows (NSIS 3), made by ./build.sh setup-windows:
;   makensis -DVERSION=0.1.0 -DNUMVERSION=0.1.0.0 -DARCH=amd64
;            -DEXE=/abs/skiff.exe -DOUTFILE=/abs/skiff-setup.exe installer.nsi
; It installs Skiff for the current user, without administrator rights, in
; %LOCALAPPDATA%\Programs\Skiff (as VS Code or GitHub Desktop do), with a
; Start menu shortcut, an entry in Settings > Apps to uninstall it, and
; (optional, checked) the sftp://, scp://, ftp://… links opened by Skiff.

Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma
RequestExecutionLevel user

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "x64.nsh"
!include "FileFunc.nsh"

!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Skiff"

Name "Skiff"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\Skiff"
InstallDirRegKey HKCU "${UNINSTKEY}" "InstallLocation"
BrandingText "Skiff ${VERSION}"

!define MUI_ICON "icon.ico"
!define MUI_UNICON "icon.ico"
!define MUI_ABORTWARNING
!define MUI_COMPONENTSPAGE_NODESC
!define MUI_FINISHPAGE_RUN "$INSTDIR\skiff.exe"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; the first one is used when the system speaks neither
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "French"

LangString SecDesktop ${LANG_ENGLISH} "Desktop shortcut"
LangString SecDesktop ${LANG_FRENCH} "Raccourci sur le bureau"
LangString SecLinks ${LANG_ENGLISH} "Open sftp://, scp://, ftp://… links with Skiff"
LangString SecLinks ${LANG_FRENCH} "Ouvrir les liens sftp://, scp://, ftp://… avec Skiff"
LangString Need64 ${LANG_ENGLISH} "Skiff needs a 64-bit Windows."
LangString Need64 ${LANG_FRENCH} "Skiff a besoin d'un Windows 64 bits."
LangString NeedArm ${LANG_ENGLISH} "This installer is for Windows on ARM. Download the x64 one (windows-amd64)."
LangString NeedArm ${LANG_FRENCH} "Cet installateur est pour Windows sur ARM. Téléchargez celui pour x64 (windows-amd64)."
LangString CloseSkiff ${LANG_ENGLISH} "Skiff is still open. Close it, then try again."
LangString CloseSkiff ${LANG_FRENCH} "Skiff est encore ouvert. Fermez-le, puis réessayez."
LangString AskData ${LANG_ENGLISH} "Also delete your sites, passwords and preferences ($APPDATA\Skiff)?$\n$\nKeep them to find them again after a new installation."
LangString AskData ${LANG_FRENCH} "Supprimer aussi vos sites, mots de passe et préférences ($APPDATA\Skiff) ?$\n$\nGardez-les pour les retrouver après une nouvelle installation."

VIProductVersion "${NUMVERSION}"
VIAddVersionKey /LANG=${LANG_ENGLISH} "ProductName" "Skiff"
VIAddVersionKey /LANG=${LANG_ENGLISH} "FileDescription" "Skiff installer"
VIAddVersionKey /LANG=${LANG_ENGLISH} "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=${LANG_ENGLISH} "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=${LANG_ENGLISH} "LegalCopyright" "Laterna"

Function .onInit
!if "${ARCH}" == "arm64"
  ${IfNot} ${IsNativeARM64}
    MessageBox MB_ICONSTOP "$(NeedArm)" /SD IDOK
    Abort
  ${EndIf}
!else
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "$(Need64)" /SD IDOK
    Abort
  ${EndIf}
!endif
FunctionEnd

Section "Skiff" SecMain
  SectionIn RO
  SetOutPath "$INSTDIR"
  ; a running Skiff keeps its file locked, but it can be renamed: the update
  ; goes in at once, and the old file goes at the next update
  Delete "$INSTDIR\skiff.old.exe"
  ${If} ${FileExists} "$INSTDIR\skiff.exe"
    Rename "$INSTDIR\skiff.exe" "$INSTDIR\skiff.old.exe"
  ${EndIf}
  File "/oname=skiff.exe" "${EXE}"
  Delete "$INSTDIR\skiff.old.exe"
  WriteUninstaller "$INSTDIR\uninstall.exe"
  CreateShortcut "$SMPROGRAMS\Skiff.lnk" "$INSTDIR\skiff.exe"

  WriteRegStr HKCU "${UNINSTKEY}" "DisplayName" "Skiff"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTKEY}" "Publisher" "Laterna"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\skiff.exe"
  WriteRegStr HKCU "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "${UNINSTKEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  WriteRegDWORD HKCU "${UNINSTKEY}" "EstimatedSize" $0
SectionEnd

Section /o "$(SecDesktop)" SecDesktop
  CreateShortcut "$DESKTOP\Skiff.lnk" "$INSTDIR\skiff.exe"
SectionEnd

; Skiff registers the links itself (the same as Preferences > System)
Section "$(SecLinks)" SecLinks
  nsExec::Exec '"$INSTDIR\skiff.exe" url-handler -register'
  Pop $0
SectionEnd

Section "Uninstall"
  ; the links go back to the other applications (before the program goes)
  ${If} ${FileExists} "$INSTDIR\skiff.exe"
    nsExec::Exec '"$INSTDIR\skiff.exe" url-handler -remove'
    Pop $0
  ${EndIf}
  retry:
  ClearErrors
  Delete "$INSTDIR\skiff.exe"
  ${If} ${Errors}
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "$(CloseSkiff)" /SD IDCANCEL IDRETRY retry
    Abort
  ${EndIf}
  Delete "$INSTDIR\skiff.old.exe"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\Skiff.lnk"
  Delete "$DESKTOP\Skiff.lnk"
  DeleteRegKey HKCU "${UNINSTKEY}"
  ; the configuration stays, unless the user asks (never in a silent uninstall)
  ${If} ${FileExists} "$APPDATA\Skiff\*.*"
    MessageBox MB_YESNO|MB_ICONQUESTION|MB_DEFBUTTON2 "$(AskData)" /SD IDNO IDNO keep
    RMDir /r "$APPDATA\Skiff"
    keep:
  ${EndIf}
SectionEnd
