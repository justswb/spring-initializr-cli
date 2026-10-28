# Checks install.ps1 end to end: install, user PATH entry, reinstall (update), uninstall.
# It really edits the user PATH (HKCU\Environment), so run it on a throwaway machine such as a CI
# runner, not on a dev box.
#
#   .\install_test.ps1 -Exe dist\spring-init.exe -ExpectedVersion v1.2.3
param(
    [Parameter(Mandatory)] [string] $Exe,   # local build that install.ps1 installs instead of downloading
    [Parameter(Mandatory)] [string] $ExpectedVersion,
    # Not under %TEMP%: on CI runners that is an 8.3 short path, which would not compare equal.
    [string] $InstallDir = (Join-Path $env:LOCALAPPDATA "Programs\spring-init-test-$([guid]::NewGuid().ToString('N').Substring(0, 8))")
)

$ErrorActionPreference = 'Stop'
$Script = Join-Path $PSScriptRoot 'install.ps1'
$Exe = (Resolve-Path $Exe).Path

function Fail($msg) { throw "FAIL: $msg" }

function Norm($dir) { $dir.Trim().TrimEnd('\').ToLowerInvariant() }

# Raw user PATH, as stored in the registry (not expanded).
function UserPath { (Get-Item 'HKCU:\Environment').GetValue('Path', '', 'DoNotExpandEnvironmentNames') }

function OtherEntries { @(UserPath -split ';' | Where-Object { $_.Trim() -ne '' -and (Norm $_) -ne (Norm $InstallDir) }) -join ';' }

function PathCount { @(UserPath -split ';' | Where-Object { (Norm $_) -eq (Norm $InstallDir) }).Count }

# Runs install.ps1 in a fresh process, like `irm ... | iex` would in a new shell.
function Install([string[]] $extra) {
    $argList = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $Script, '-InstallDir', $InstallDir) + $extra
    Write-Host "> install.ps1 $($extra -join ' ')"
    & powershell.exe @argList
    if ($LASTEXITCODE -ne 0) { Fail "install.ps1 $($extra -join ' ') exited with $LASTEXITCODE" }
}

# A new shell finds spring-init through the registry PATH, and it reports the expected version.
function AssertInstalled {
    if (-not (Test-Path (Join-Path $InstallDir 'spring-init.exe'))) { Fail "spring-init.exe is missing from $InstallDir" }
    $n = PathCount
    if ($n -ne 1) { Fail "install dir is in the user PATH $n times, expected 1" }
    $env:Path = [Environment]::GetEnvironmentVariable('Path', 'Machine') + ';' + [Environment]::GetEnvironmentVariable('Path', 'User')
    $cmd = Get-Command spring-init -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $cmd) { Fail 'spring-init is not found via PATH' }
    if ((Norm (Split-Path $cmd.Source)) -ne (Norm $InstallDir)) { Fail "PATH resolves spring-init to $($cmd.Source)" }
    $out = (& $cmd.Source --version | Out-String).Trim()
    if ($out -ne "spring-init $ExpectedVersion") { Fail "spring-init --version printed '$out'" }
    Write-Host "ok: $out in $InstallDir, PATH entry once"
}

$before = OtherEntries
if (PathCount) { Fail "$InstallDir is already in the user PATH" }

Install @('-From', $Exe)
AssertInstalled

# Installing again (an update) replaces the exe and keeps a single PATH entry.
Install @('-From', $Exe)
AssertInstalled

Install @('-Uninstall')
if (Test-Path $InstallDir) { Fail "$InstallDir still exists after uninstall" }
if (PathCount) { Fail 'install dir is still in the user PATH after uninstall' }
if ((OtherEntries) -ne $before) { Fail "the rest of the user PATH changed:`n  before: $before`n  after:  $(OtherEntries)" }
Write-Host 'ok: uninstalled, PATH restored'

Write-Host 'PASS'
