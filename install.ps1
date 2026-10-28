# Installs spring-init for the current user (no admin) and adds it to the user PATH.
#
#   irm https://raw.githubusercontent.com/justswb/spring-initializr-cli/master/install.ps1 | iex
#
# Run it again to update. Other options (via a script block, since iex can't pass arguments):
#
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/justswb/spring-initializr-cli/master/install.ps1))) -Version v2.0.3
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/justswb/spring-initializr-cli/master/install.ps1))) -Uninstall
#
# -From installs a local exe instead of downloading one (used by install_test.ps1).
# Runs in the caller's shell when piped to iex, so it throws instead of calling exit.
param(
    [string] $Version = 'latest',
    [string] $InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\spring-init'),
    [string] $From,
    [switch] $Uninstall
)

$repo = 'https://github.com/justswb/spring-initializr-cli'
$exe = Join-Path $InstallDir 'spring-init.exe'

function Norm($dir) { $dir.Trim().TrimEnd('\').ToLowerInvariant() }

# Writes the user PATH back as REG_EXPAND_SZ (entries may use %VARS%) and tells running programs,
# so terminals opened from Explorer afterwards see the change.
function Set-UserPath($value) {
    Set-ItemProperty -Path 'HKCU:\Environment' -Name Path -Value $value -Type ExpandString -ErrorAction Stop
    if (-not ('SpringInit.Env' -as [type])) {
        Add-Type -Namespace SpringInit -Name Env -MemberDefinition @'
[DllImport("user32.dll", CharSet = CharSet.Unicode)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint msg, UIntPtr wParam, string lParam, uint flags, uint timeout, out UIntPtr result);
'@
    }
    $result = [UIntPtr]::Zero
    [void][SpringInit.Env]::SendMessageTimeout([IntPtr]0xffff, 0x1a, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
}

# User PATH entries as stored (not expanded), without empty ones and without the install dir.
$userPath = (Get-Item 'HKCU:\Environment').GetValue('Path', '', 'DoNotExpandEnvironmentNames')
$others = @($userPath -split ';' | Where-Object { $_.Trim() -ne '' -and (Norm $_) -ne (Norm $InstallDir) })
$onPath = @($userPath -split ';' | Where-Object { (Norm $_) -eq (Norm $InstallDir) }).Count -gt 0

if ($Uninstall) {
    if ($onPath) { Set-UserPath ($others -join ';') }
    if (Test-Path $InstallDir) { Remove-Item $InstallDir -Recurse -Force -ErrorAction Stop }
    Write-Host 'spring-init uninstalled.'
    return
}

New-Item -ItemType Directory -Force -Path $InstallDir -ErrorAction Stop | Out-Null
if ($From) {
    Copy-Item $From $exe -Force -ErrorAction Stop
} else {
    $url = if ($Version -eq 'latest') { "$repo/releases/latest/download/spring-init.exe" } else { "$repo/releases/download/$Version/spring-init.exe" }
    Write-Host "Downloading $url"
    # Windows PowerShell 5.1 may not offer TLS 1.2 by default; the progress bar makes downloads very slow there.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    $progress = $ProgressPreference
    $ProgressPreference = 'SilentlyContinue'
    try { Invoke-WebRequest $url -OutFile "$exe.download" -UseBasicParsing -ErrorAction Stop }
    finally { $ProgressPreference = $progress }
    Move-Item "$exe.download" $exe -Force -ErrorAction Stop
}

if (-not $onPath) { Set-UserPath ((@($others) + $InstallDir) -join ';') }
# Also make it usable in this shell right away.
if (-not (@($env:Path -split ';' | Where-Object { (Norm $_) -eq (Norm $InstallDir) }).Count)) { $env:Path += ";$InstallDir" }

Write-Host "Installed $(& $exe --version) to $InstallDir"
