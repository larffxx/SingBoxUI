<#
.SYNOPSIS
    Windows release packaging: sing-box UI .exe + privileged helper -> optional
    Authenticode signing -> NSIS installer.

.DESCRIPTION
    Signing is opt-in and driven only by environment variables (rewrite spec
    §74/§75). With none set this produces an unsigned local/dev build; nothing
    here ever stores or hardcodes a credential.

      WINDOWS_CERTIFICATE           base64-encoded .pfx (Authenticode)
      WINDOWS_CERTIFICATE_PASSWORD  password for that .pfx
      VERSION                       release version (default: git describe)
      SIGNTOOL                      path to signtool.exe (default: search PATH)

    The narrow privileged helper (cmd/singboxui-priv, ADR 005) is built for the
    same architecture and installed next to the application executable, both in
    the plain distribution and inside the NSIS installer: elevated TUN
    operations run in that helper, never in the UI process itself.

.PARAMETER Arch
    Target architecture: amd64 (default) or arm64.

.PARAMETER NoInstaller
    Skip the NSIS installer and only produce the application executable and the
    helper.

.EXAMPLE
    pwsh -NoProfile -File scripts/package-windows.ps1 -Arch amd64
#>
[CmdletBinding()]
param(
    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = 'amd64',

    [switch]$NoInstaller
)

$ErrorActionPreference = 'Stop'

# A credential materialised on disk must not survive a failure, so the temporary
# .pfx is removed by this trap on every terminating error path.
$pfx = $null
trap {
    if ($pfx -and (Test-Path $pfx)) { Remove-Item $pfx -Force }
    throw
}

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$appName = 'singboxui'
$privName = 'singboxui-priv'
$version = $env:VERSION
if (-not $version) {
    $version = (git describe --tags --always --dirty 2>$null)
    if (-not $version) { $version = 'dev' }
}
$env:VERSION = $version

# The bundle and installer metadata come from wails.json, while -ldflags only
# reaches the Go binary: a release packaged with a stale productVersion ships
# binaries whose reported version contradicts the tag. Only enforced for release
# versions, so `git describe` output on a working tree still builds.
if ($version -match '^v?\d+\.\d+\.\d+$') {
    $want = $version -replace '^v', ''
    $have = (Get-Content -Raw -Path (Join-Path $root 'wails.json') | ConvertFrom-Json).info.productVersion
    if ($have -ne $want) {
        throw "wails.json info.productVersion is '$have' but this build is version '$version'; bump it before releasing"
    }
}

# Wails appends "-mmacosx-version-min=10.13" on macOS; on Windows the only
# build-flag concern is that the version metadata must be injected explicitly,
# because a bare `wails build` ships a binary with no version.
$commit = (git rev-parse --short HEAD 2>$null)
if (-not $commit) { $commit = 'unknown' }
$buildDate = (Get-Date -AsUTC -Format 'yyyy-MM-ddTHH:mm:ssZ')
$ldflags = "-s -w -X main.version=$version -X main.commit=$commit -X main.buildDate=$buildDate"

Write-Host "==> building windows/$Arch (version $version)"
$buildArgs = @('build', '-platform', "windows/$Arch", '-clean', '-ldflags', $ldflags)
# The first pass also materialises build/windows/installer/{project.nsi,wails_tools.nsh};
# the installer itself is rebuilt below, after the helper exists and is signed.
if (-not $NoInstaller) { $buildArgs += '-nsis' }

& wails @buildArgs
if ($LASTEXITCODE -ne 0) { throw "wails build failed with exit code $LASTEXITCODE" }

$binDir = Join-Path $root 'build/bin'
$appExe = Join-Path $binDir "$appName.exe"
if (-not (Test-Path $appExe)) { throw "expected $appExe was not produced" }

Write-Host "==> building the privileged helper (windows/$Arch)"
$env:CGO_ENABLED = '0'
# The helper must match the requested architecture: inheriting the host's default
# left the arm64 package with an amd64 helper.
$env:GOOS = 'windows'
$env:GOARCH = $Arch
& go build -trimpath -ldflags $ldflags -o (Join-Path $binDir "$privName.exe") ./cmd/singboxui-priv
if ($LASTEXITCODE -ne 0) { throw "building cmd/singboxui-priv failed with exit code $LASTEXITCODE" }

$helperExe = Join-Path $binDir "$privName.exe"

# PE machine type: 0x8664 is x86-64, 0xAA64 is AArch64. A package that silently
# carries the wrong architecture only shows up on the user's machine, so check
# the executables that are about to be shipped.
function Get-PeMachine([string]$Path) {
    $bytes = [IO.File]::ReadAllBytes($Path)
    $peOffset = [BitConverter]::ToInt32($bytes, 0x3c)
    return [BitConverter]::ToUInt16($bytes, $peOffset + 4)
}

$expectedMachine = if ($Arch -eq 'arm64') { 0xAA64 } else { 0x8664 }
foreach ($binary in @($appExe, $helperExe)) {
    $machine = Get-PeMachine $binary
    if ($machine -ne $expectedMachine) {
        $name = [IO.Path]::GetFileName($binary)
        $found = '{0:x4}' -f $machine
        $wanted = '{0:x4}' -f $expectedMachine
        throw "$name is a 0x$found binary, expected 0x$wanted (windows/$Arch)"
    }
}

$signtool = $null
$signed = $false
if ($env:WINDOWS_CERTIFICATE) {
    if (-not $env:WINDOWS_CERTIFICATE_PASSWORD) {
        throw 'WINDOWS_CERTIFICATE is set but WINDOWS_CERTIFICATE_PASSWORD is missing'
    }

    $signtool = $env:SIGNTOOL
    if (-not $signtool) {
        $cmd = Get-Command 'signtool.exe' -ErrorAction SilentlyContinue
        if ($cmd) { $signtool = $cmd.Source }
    }
    if (-not $signtool) {
        throw 'signtool.exe not found; set SIGNTOOL or install the Windows SDK'
    }

    # Sign the application and the helper *before* the installer is built, so the
    # copies embedded in the installer are themselves signed.
    $pfx = Join-Path $env:TEMP 'singboxui-signing.pfx'
    [IO.File]::WriteAllBytes($pfx, [Convert]::FromBase64String($env:WINDOWS_CERTIFICATE))
    foreach ($binary in @($appExe, $helperExe)) {
        Write-Host "==> signing $([IO.Path]::GetFileName($binary))"
        & $signtool sign /fd SHA256 /f $pfx /p $env:WINDOWS_CERTIFICATE_PASSWORD `
            /tr 'http://timestamp.digicert.com' /td SHA256 $binary
        if ($LASTEXITCODE -ne 0) { throw "signtool failed for $binary" }
    }
    $signed = $true
}
else {
    Write-Host '!! WINDOWS_CERTIFICATE not set — producing an UNSIGNED build (local/dev only)'
}

$installerDir = Join-Path $root 'build/windows/installer'
if (-not $NoInstaller) {
    $projectNsi = Join-Path $installerDir 'project.nsi'
    if (-not (Test-Path $projectNsi)) {
        throw "NSIS project file $projectNsi is missing; run without -NoInstaller only on a host with the Wails NSIS assets"
    }

    $makensis = Get-Command 'makensis.exe' -ErrorAction SilentlyContinue
    if (-not $makensis) { $makensis = Get-Command 'makensis' -ErrorAction SilentlyContinue }
    if (-not $makensis) {
        throw 'makensis not found; install NSIS or pass -NoInstaller'
    }

    # Wails installs only the application executable, so the helper has to be
    # added to the installer explicitly. The edit is idempotent: it is skipped
    # when the generated project.nsi already mentions the helper.
    $nsi = Get-Content -Raw -Path $projectNsi
    if ($nsi -notmatch [regex]::Escape("$privName.exe")) {
        $anchor = '!insertmacro wails.files'
        if ($nsi -notmatch [regex]::Escape($anchor)) {
            throw "could not find '$anchor' in $projectNsi to inject the helper"
        }
        $helperLines = @(
            '    # SingBoxUI: the narrow privileged helper must ship next to the app so',
            '    # elevated TUN operations never run inside the UI process (ADR 005).',
            ('    File /oname={0}.exe "..\..\bin\{0}.exe"' -f $privName),
            ''
        ) -join "`r`n"
        $nsi = $nsi.Replace($anchor, $helperLines + "`r`n" + $anchor)
        Set-Content -Path $projectNsi -Value $nsi -NoNewline
        Write-Host '==> installer script patched to include the privileged helper'
    }

    Write-Host "==> building the NSIS installer (windows/$Arch)"
    $archDefine = "-DARG_WAILS_$($Arch.ToUpper())_BINARY=..\..\bin\$appName.exe"
    Push-Location $installerDir
    try {
        & $makensis.Source $archDefine $projectNsi
        if ($LASTEXITCODE -ne 0) { throw "makensis failed with exit code $LASTEXITCODE" }
    }
    finally {
        Pop-Location
    }
}

$dist = Join-Path $root 'dist'
New-Item -ItemType Directory -Force -Path $dist | Out-Null

$artifacts = @(Get-ChildItem -Path $binDir -Recurse -File -Include '*.exe' -ErrorAction SilentlyContinue)
if ($artifacts.Count -eq 0) { throw "no .exe produced under $binDir" }

if ($signed) {
    # The installer was rebuilt after the binaries were signed, so it is the one
    # artifact that is still unsigned at this point.
    foreach ($artifact in $artifacts) {
        if ($artifact.Name -notmatch 'setup|installer') { continue }
        Write-Host "==> signing $($artifact.Name)"
        & $signtool sign /fd SHA256 /f $pfx /p $env:WINDOWS_CERTIFICATE_PASSWORD `
            /tr 'http://timestamp.digicert.com' /td SHA256 $artifact.FullName
        if ($LASTEXITCODE -ne 0) { throw "signtool failed for $($artifact.FullName)" }
    }
}

foreach ($artifact in $artifacts) {
    # The privileged helper ships next to the application executable, so it needs
    # its own name: the two used to share the 'app' suffix, and `Copy-Item -Force`
    # then silently overwrote whichever came first, so the "app" download could
    # be the helper.
    $suffix = if ($artifact.Name -match 'setup|installer') {
        'setup'
    }
    elseif ($artifact.Name -eq "$privName.exe") {
        'helper'
    }
    else {
        'app'
    }
    $target = Join-Path $dist "$appName-$version-windows-$Arch-$suffix$($artifact.Extension)"
    Copy-Item -Path $artifact.FullName -Destination $target -Force
    Write-Host "==> artifact: $target"
}

Write-Host "==> done (signed=$signed, version=$version)"

if ($pfx -and (Test-Path $pfx)) { Remove-Item $pfx -Force }
