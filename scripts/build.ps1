param(
    [switch]$Desktop,
    [switch]$Run,
    [ValidateSet('amd64','arm64')][string]$Architecture = 'amd64',
    [ValidateSet('windows','linux','darwin')][string]$TargetOS
)

$ErrorActionPreference = 'Stop'
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = $OutputEncoding
$projectRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$artifactRoot = Join-Path $projectRoot 'artifacts'
$outputRoot = Join-Path $projectRoot 'bin'
$oldGoArch = $env:GOARCH
$oldGoOS = $env:GOOS
$oldCgo = $env:CGO_ENABLED

function Test-ActiveJavaBuild($process) {
    if ($process.CommandLine -match 'GradleWorkerMain|org\.gradle\.launcher\.GradleMain|org\.jetbrains\.kotlin\.cli') { return $true }
    if ($process.CommandLine -notmatch 'GradleDaemon') { return $false }
    $gradleRoot = if ($env:GRADLE_USER_HOME) { $env:GRADLE_USER_HOME } else { Join-Path $env:USERPROFILE '.gradle' }
    $logs = @(Get-ChildItem -LiteralPath (Join-Path $gradleRoot 'daemon') -Recurse -Filter "daemon-$($process.ProcessId).out.log" -ErrorAction SilentlyContinue)
    if ($logs.Count -eq 0) { return $true }
    $events = @(Select-String -LiteralPath $logs[0].FullName -Pattern 'daemon has (started|finished) executing the build' -Encoding UTF8 | Select-Object -Last 1)
    if ($events.Count -eq 0) { return $true }
    return $events[-1].Line -match 'daemon has started executing'
}

try {
    Push-Location -LiteralPath $projectRoot
    if (!(Get-Command go -ErrorAction SilentlyContinue)) { throw 'Go is not available in PATH.' }
    $env:GOARCH = $Architecture
    $env:CGO_ENABLED = '0'
    $hostOS = (& go env GOHOSTOS).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Cannot determine Go host platform.' }
    if (!$TargetOS) { $TargetOS = $hostOS }
    $env:GOOS = $TargetOS
    if ($Desktop -and $TargetOS -ne $hostOS) { throw 'Full desktop builds must be explicitly requested on their target platform.' }
    if ($Run -and !$Desktop) { throw '-Run requires -Desktop.' }
    if ($Desktop) {
        Write-Host 'Desktop build explicitly requested. This is the full build requiring manual approval under AGENTS.md.'
    }

    $deadline = (Get-Date).AddMinutes(10)
    do {
        $busy = @(Get-CimInstance Win32_Process | Where-Object {
            ($_.Name -match '^(compile|link|gcc|clang|gradle|ninja|cmake)(\.exe)?$') -or
            ($_.Name -eq 'go.exe' -and $_.CommandLine -match '\s(build|run|install|test)\s') -or
            ($_.Name -eq 'java.exe' -and (Test-ActiveJavaBuild $_))
        })
        if ($busy.Count -gt 0) {
            if ((Get-Date) -gt $deadline) { throw 'Another compilation is still running; retry after it finishes.' }
            Write-Host 'Waiting for another compilation to finish...'
            Start-Sleep -Seconds 5
        }
    } while ($busy.Count -gt 0)

    New-Item -ItemType Directory -Force -Path $artifactRoot | Out-Null
    $files = @(Get-ChildItem -LiteralPath (Join-Path $projectRoot 'internal') -Filter '*.go' -Recurse)
    $files += @(Get-ChildItem -LiteralPath $projectRoot -Filter '*.go')
    $files += @(Get-Item -LiteralPath (Join-Path $projectRoot 'VERSION'))
    if ($Desktop) { $files += @(Get-ChildItem -LiteralPath (Join-Path $projectRoot 'cmd') -Filter '*.go' -Recurse) }
    if ($Desktop) { $files += @(Get-ChildItem -LiteralPath (Join-Path $projectRoot 'cmd') -Filter '*.syso' -Recurse) }
    $files += @(Get-Item -LiteralPath (Join-Path $projectRoot 'go.mod'),(Join-Path $projectRoot 'go.sum'))
    $fingerprint = (($files | Sort-Object FullName | ForEach-Object { $_.FullName + ':' + (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash }) -join "`n")
    $mode = if ($Desktop) { 'desktop' } else { 'modules' }
    $stamp = Join-Path $artifactRoot "$mode-$TargetOS-$Architecture.json"
    $outputFile = Join-Path $outputRoot $(if ($hostOS -eq 'windows') { 'loghelp.exe' } else { 'loghelp' })
    $same = $false
    if (Test-Path -LiteralPath $stamp) {
        $previous = Get-Content -LiteralPath $stamp -Raw -Encoding UTF8 | ConvertFrom-Json
        $same = $previous.Fingerprint -ceq $fingerprint
        if ($Desktop -and !(Test-Path -LiteralPath $outputFile)) { $same = $false }
    }
    if ($same) { Write-Host 'Source unchanged since the successful check; skipping duplicate build.' }
    else {
        if ($Desktop) {
            New-Item -ItemType Directory -Force -Path $outputRoot | Out-Null
            if ($hostOS -eq 'windows') { & go build -ldflags '-H=windowsgui' -o $outputFile ./cmd/loghelp }
            else { & go build -o $outputFile ./cmd/loghelp }
        }
        else { & go build ./internal/core ./internal/ui }
        if ($LASTEXITCODE -ne 0) { throw 'Compilation failed.' }
        @{ Fingerprint = $fingerprint; Completed = (Get-Date).ToString('o'); Mode = $mode } |
            ConvertTo-Json | Set-Content -LiteralPath $stamp -Encoding UTF8
    }
    if ($Run) {
        # -Run is an explicit request for the interactive desktop window.
        Start-Process -FilePath $outputFile -WorkingDirectory $projectRoot -WindowStyle Normal
    }
}
finally {
    $env:GOARCH = $oldGoArch
    $env:GOOS = $oldGoOS
    $env:CGO_ENABLED = $oldCgo
    Pop-Location
}
