; Package an already verified desktop build. No Go compilation happens here.
#if VER < EncodeVer(6, 4, 0)
  #error Inno Setup 6.4 or newer is required
#endif
#ifndef PayloadDir
  #error Use .agent/scripts/package-release.py --setup
#endif

[Setup]
AppId=LogHelp
AppName=LogHelp
AppVersion={#AppVersion} r{#AppRevision}
AppPublisher=LogHelp
AppPublisherURL=https://github.com/sunqing37/loghelp
AppSupportURL=https://github.com/sunqing37/loghelp/issues
DefaultDirName={localappdata}\Programs\LogHelp
UsePreviousAppDir=no
DisableDirPage=yes
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
#if AppArch == "arm64"
ArchitecturesAllowed=arm64
ArchitecturesInstallIn64BitMode=arm64
#else
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
#endif
MinVersion=10.0
AppMutex=Local\LogHelp.Application
SetupMutex=Local\LogHelp.Setup
CloseApplications=no
RestartApplications=no
UninstallDisplayIcon={app}\loghelp.exe
OutputDir={#PackageDir}
OutputBaseFilename={#PackageName}
SetupIconFile=..\..\assets\loghelp.ico
VersionInfoVersion={#AppVersion}
VersionInfoProductVersion={#AppVersion}
VersionInfoProductTextVersion={#AppVersion} r{#AppRevision}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern

[Tasks]
Name: desktopicon; Description: "创建桌面快捷方式"; Flags: unchecked

[Files]
Source: "{#PayloadDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{userprograms}\LogHelp"; Filename: "{app}\loghelp.exe"; WorkingDir: "{app}"
Name: "{userdesktop}\LogHelp"; Filename: "{app}\loghelp.exe"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
Filename: "{app}\loghelp.exe"; Description: "启动 LogHelp"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
; Never delete all of {app}: users may have stored recordings there.
Type: files; Name: "{app}\loghelp.exe.previous"
Type: files; Name: "{app}\loghelp-update-error.log"

[Code]
var
  DeleteUserData: Boolean;

function GetFileAttributesW(Name: String): Cardinal;
  external 'GetFileAttributesW@kernel32.dll stdcall';

function IsPlainDirectory(Name: String): Boolean;
var
  Attributes: Cardinal;
begin
  Attributes := GetFileAttributesW(Name);
  Result := (Attributes <> $FFFFFFFF) and ((Attributes and $10) <> 0) and
    ((Attributes and $400) = 0);
end;

function CompareNumber(A, B: String): Integer;
begin
  if Length(A) < Length(B) then Result := -1
  else if Length(A) > Length(B) then Result := 1
  else Result := CompareStr(A, B);
end;

function ParseVersion(Value: String; var Parts: TArrayOfString): Boolean;
var
  I, J: Integer;
begin
  Result := False;
  StringChangeEx(Value, ' r', '.', True);
  Parts := StringSplit(Value, ['.'], stAll);
  if GetArrayLength(Parts) <> 4 then Exit;
  for I := 0 to 3 do begin
    if Parts[I] = '' then Exit;
    for J := 1 to Length(Parts[I]) do
      if (Parts[I][J] < '0') or (Parts[I][J] > '9') then Exit;
    while (Length(Parts[I]) > 1) and (Parts[I][1] = '0') do
      Delete(Parts[I], 1, 1);
  end;
  Result := True;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Code, I, Comparison: Integer;
  Output: TExecOutput;
  CurrentParts, NewParts: TArrayOfString;
  CurrentVersion: String;
begin
  Result := '';
  if CompareText(RemoveBackslashUnlessRoot(ExpandConstant('{app}')),
    ExpandConstant('{localappdata}\Programs\LogHelp')) <> 0 then begin
    Result := '请使用当前用户的默认安装目录，确保应用内更新具有写入权限。';
    Exit;
  end;
  if CheckForMutexes('Local\LogHelp.Application') then begin
    Result := '请先退出 LogHelp，等待更新或重启完成后再安装。';
    Exit;
  end;
  if not FileExists(ExpandConstant('{app}\loghelp.exe')) then Exit;
  Result := '无法读取已安装程序的版本，已停止覆盖。请检查程序后重试。';
  try
    if not ExecAndCaptureOutput(ExpandConstant('{app}\loghelp.exe'), '--version',
      ExpandConstant('{app}'), SW_HIDE, ewWaitUntilTerminated, Code, Output) then Exit;
    if (Code <> 0) or Output.Error or (GetArrayLength(Output.StdOut) <> 1) then Exit;
    CurrentVersion := Trim(Output.StdOut[0]);
    if Copy(CurrentVersion, 1, 8) <> 'LogHelp ' then Exit;
    Delete(CurrentVersion, 1, 8);
    if not ParseVersion(CurrentVersion, CurrentParts) then Exit;
    if not ParseVersion('{#AppVersion} r{#AppRevision}', NewParts) then Exit;
    for I := 0 to 3 do begin
      Comparison := CompareNumber(CurrentParts[I], NewParts[I]);
      if Comparison > 0 then begin
        Result := '已安装较新的 LogHelp ' + CurrentVersion + '，不能用旧安装包覆盖。';
        Exit;
      end;
      if Comparison < 0 then Break;
    end;
    Result := '';
  except
    Log(GetExceptionMessage);
  end;
end;

function InitializeUninstall(): Boolean;
var
  Choice: Integer;
begin
  DeleteUserData := False;
  Result := True;
  if UninstallSilent then Exit;  // Silent uninstall always preserves data.
  Choice := MsgBox('卸载默认保留用户数据。是否同时删除以下目录中的设置、书签、历史、缓存和诊断日志？' + #13#10 + #13#10 +
    ExpandConstant('{userappdata}\loghelp') + #13#10 +
    ExpandConstant('{localappdata}\loghelp') + #13#10 + #13#10 +
    '这些数据也由 ZIP 便携版共用。其他位置的录制和导出文件不会删除。' + #13#10 +
    '选择“是”删除上述数据，“否”保留数据，“取消”退出卸载。',
    mbConfirmation, MB_YESNOCANCEL or MB_DEFBUTTON2);
  DeleteUserData := Choice = IDYES;
  Result := Choice <> IDCANCEL;
end;

// Delete only explicitly confirmed application data. Never traverse junctions
// or symlinks; preserve them and report incomplete cleanup instead.
function DeletePlainTree(Directory: String): Boolean;
var
  FindRec: TFindRec;
  Child: String;
begin
  Result := True;
  if not DirExists(Directory) then Exit;
  if not IsPlainDirectory(Directory) then begin
    Result := False;
    Exit;
  end;
  if FindFirst(AddBackslash(Directory) + '*', FindRec) then begin
    try
      repeat
        if (FindRec.Name <> '.') and (FindRec.Name <> '..') then begin
          Child := AddBackslash(Directory) + FindRec.Name;
          if (FindRec.Attributes and $400) <> 0 then Result := False
          else if (FindRec.Attributes and $10) <> 0 then begin
            if not DeletePlainTree(Child) then Result := False;
          end else if not DeleteFile(Child) then Result := False;
        end;
      until not FindNext(FindRec);
    finally
      FindClose(FindRec);
    end;
  end;
  if not RemoveDir(Directory) then Result := False;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  ConfigRemoved, CacheRemoved: Boolean;
begin
  if (CurUninstallStep = usPostUninstall) and DeleteUserData then begin
    ConfigRemoved := DeletePlainTree(ExpandConstant('{userappdata}\loghelp'));
    CacheRemoved := DeletePlainTree(ExpandConstant('{localappdata}\loghelp'));
    if not ConfigRemoved or not CacheRemoved then
      MsgBox('程序已卸载，但部分用户数据无法删除（文件占用、权限或目录链接）。请检查上述数据目录并手动清理。', mbError, MB_OK);
  end;
end;
