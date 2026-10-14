param(
    [switch]$Desktop,
    [switch]$Run,
    [switch]$CrossDesktop,
    [ValidateSet('amd64','arm64')][string]$Architecture = 'amd64',
    [ValidateSet('windows','linux','darwin')][string]$TargetOS
)

$ErrorActionPreference = 'Stop'
$buildArguments = @('-X', 'utf8', (Join-Path $PSScriptRoot 'build.py'), '--arch', $Architecture)
if ($Desktop) { $buildArguments += '--desktop' }
if ($Run) { $buildArguments += '--run' }
if ($CrossDesktop) { $buildArguments += '--cross-desktop' }
if ($TargetOS) { $buildArguments += @('--target-os', $TargetOS) }
# Both entry points share process checks, locking, fingerprints and output hashes.
& python @buildArguments
if ($LASTEXITCODE -ne 0) { throw "Build failed (exit $LASTEXITCODE)." }
