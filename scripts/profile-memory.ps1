param(
    [ValidateRange(30,3600)][int]$DurationSeconds = 600,
    [string]$Directory,
    [ValidateSet('workspace','settings','updates','device-picker')][string]$Scene = 'workspace',
    [ValidateSet('auto','opengl','directx')][string]$Graphics = 'auto',
    [switch]$NaturalGC,
    [switch]$LegacyInputs
)

$ErrorActionPreference = 'Stop'
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = $OutputEncoding
$profileProjectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$profileExe = Join-Path $profileProjectRoot 'bin/loghelp.exe'
if (!(Test-Path -LiteralPath $profileExe)) { throw 'Build loghelp.exe before starting diagnostics.' }
if (!$Directory) { $Directory = Join-Path $profileProjectRoot ('artifacts/memory-' + (Get-Date -Format 'yyyyMMdd-HHmmss')) }
$Directory = [IO.Path]::GetFullPath($Directory)
if ((Test-Path -LiteralPath $Directory) -and @(Get-ChildItem -LiteralPath $Directory -Force).Count -gt 0) { throw 'Use a new empty profile directory.' }
New-Item -ItemType Directory -Force -Path $Directory | Out-Null
$profileConfigRoot = Join-Path $Directory 'config'
$profileSettingsDir = Join-Path $profileConfigRoot 'loghelp'
New-Item -ItemType Directory -Force -Path $profileSettingsDir | Out-Null
$profileUserSettings = Join-Path $env:APPDATA 'loghelp/settings.json'
if (Test-Path -LiteralPath $profileUserSettings) {
    $profileSettings = Get-Content -LiteralPath $profileUserSettings -Raw -Encoding UTF8 | ConvertFrom-Json
    # Keep the selected device but exercise the full bounded cache.
    $profileSettings.tabs = @($profileSettings.tabs | Where-Object { !$_.file } | Select-Object -First 1)
    foreach ($profileTab in $profileSettings.tabs) {
        foreach ($profileField in @('package','filter','search')) {
            $profileTab | Add-Member -NotePropertyName $profileField -NotePropertyValue '' -Force
        }
    }
    $profileSettings | ConvertTo-Json -Depth 16 | Set-Content -LiteralPath (Join-Path $profileSettingsDir 'settings.json') -Encoding UTF8
}

$profileVariables = @('APPDATA','LOGHELP_PROFILE_DIR','LOGHELP_PROFILE_GC','LOGHELP_PROFILE_SCENE','LOGHELP_PROFILE_SNAPSHOT','LOGHELP_PROFILE_DURATION','LOGHELP_PROFILE_LEGACY_INPUTS','LOGHELP_PROFILE_BACKGROUND','EBITENGINE_GRAPHICS_LIBRARY')
$profilePrevious = @{}
foreach ($profileVariable in $profileVariables) { $profilePrevious[$profileVariable] = [Environment]::GetEnvironmentVariable($profileVariable, 'Process') }
try {
    $env:APPDATA = $profileConfigRoot
    $env:LOGHELP_PROFILE_DIR = $Directory
	$env:LOGHELP_PROFILE_BACKGROUND = '1'
    $env:LOGHELP_PROFILE_GC = if ($NaturalGC) { '0' } else { '1' }
    $env:LOGHELP_PROFILE_SCENE = $Scene
    $env:LOGHELP_PROFILE_SNAPSHOT = '1'
    $env:LOGHELP_PROFILE_DURATION = "${DurationSeconds}s"
    $env:LOGHELP_PROFILE_LEGACY_INPUTS = if ($LegacyInputs) { '1' } else { '0' }
    $env:EBITENGINE_GRAPHICS_LIBRARY = $Graphics
    $profileProcess = Start-Process -FilePath $profileExe -WorkingDirectory $profileProjectRoot -WindowStyle Hidden -RedirectStandardOutput (Join-Path $Directory 'stdout.txt') -RedirectStandardError (Join-Path $Directory 'stderr.txt') -PassThru
}
finally {
    foreach ($profileVariable in $profileVariables) { [Environment]::SetEnvironmentVariable($profileVariable, $profilePrevious[$profileVariable], 'Process') }
}
$profileProcess.Id | Set-Content -LiteralPath (Join-Path $Directory 'pid.txt') -Encoding UTF8
$profileStarted = Get-Date
$profileSamples = [Collections.Generic.List[object]]::new()
do {
    $profileProcess.Refresh()
    if ($profileProcess.HasExited) { break }
    $profileSamples.Add([pscustomobject]@{
        Seconds = [math]::Round(((Get-Date)-$profileStarted).TotalSeconds, 1)
        WorkingSet = $profileProcess.WorkingSet64
        PrivateBytes = $profileProcess.PrivateMemorySize64
        CPU = $profileProcess.CPU
    })
    $profileSamples | Export-Csv -LiteralPath (Join-Path $Directory 'process.csv') -Encoding UTF8 -NoTypeInformation
    Start-Sleep -Seconds 5
} while (((Get-Date)-$profileStarted).TotalSeconds -lt $DurationSeconds + 20)
$profileProcess.Refresh()
if (!$profileProcess.HasExited) { throw "Profile process $($profileProcess.Id) has not exited; check stderr and stop only that process if needed." }
$profileSamples | Select-Object -Last 3
Write-Output "Profile directory: $Directory"
Write-Output "Exit code: $($profileProcess.ExitCode)"
