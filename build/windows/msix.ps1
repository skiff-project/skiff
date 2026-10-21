# Makes the MSIX packages of Skiff for the Microsoft Store (x64 and arm64)
# and the bundle that holds both, from the Windows binaries:
#
#   pwsh build/windows/msix.ps1 -Version 1.2.0 -ExeDir . -OutDir dist `
#        -IdentityName Laterna.Skiff -Publisher "CN=..." -PublisherName Laterna
#
# The identity comes from Partner Center (Product identity); the Store signs
# the packages, so they are not signed here. Needs makeappx.exe (Windows
# SDK, present on the Windows machines of GitHub Actions).
param(
  [Parameter(Mandatory)][string]$Version,
  [string]$ExeDir = '.',
  [string]$OutDir = 'dist',
  [string]$IdentityName = 'Laterna.Skiff',
  [string]$Publisher = 'CN=Laterna',
  [string]$PublisherName = 'Laterna'
)
$ErrorActionPreference = 'Stop'
$here = $PSScriptRoot
$root = Resolve-Path (Join-Path $here '..\..')

$makeappx = Get-ChildItem "${env:ProgramFiles(x86)}\Windows Kits\10\bin\*\x64\makeappx.exe" -ErrorAction SilentlyContinue |
  Sort-Object FullName -Descending | Select-Object -First 1
if (-not $makeappx) { throw 'makeappx.exe not found: install the Windows SDK' }

# the Store wants four numbers, the last one 0: 1.2.0 -> 1.2.0.0
$parts = @(($Version -replace '[~+-].*$', '').Split('.') | Where-Object { $_ -match '^\d+$' })
while ($parts.Count -lt 3) { $parts += '0' }
$msixVersion = ($parts[0..2] -join '.') + '.0'

$esc = { param($s) [Security.SecurityElement]::Escape($s) }
$template = Get-Content -Raw (Join-Path $here 'msix\AppxManifest.xml')
$work = Join-Path ([IO.Path]::GetTempPath()) ('skiff-msix-' + [guid]::NewGuid())
$packages = Join-Path $work 'packages'
New-Item -ItemType Directory -Force $packages, $OutDir | Out-Null

foreach ($a in @(@{ go = 'amd64'; msix = 'x64' }, @{ go = 'arm64'; msix = 'arm64' })) {
  $layout = Join-Path $work $a.msix
  New-Item -ItemType Directory -Force $layout | Out-Null
  Copy-Item (Join-Path $ExeDir "skiff-windows-$($a.go).exe") (Join-Path $layout 'skiff.exe')
  Copy-Item -Recurse (Join-Path $here 'msix\Assets') (Join-Path $layout 'Assets')
  foreach ($f in 'LICENSE', 'NOTICE', 'THIRD_PARTY_NOTICES.txt') { Copy-Item (Join-Path $root $f) $layout }
  $manifest = $template.Replace('__NAME__', (& $esc $IdentityName)).Replace('__PUBLISHER_NAME__', (& $esc $PublisherName)).
    Replace('__PUBLISHER__', (& $esc $Publisher)).Replace('__VERSION__', $msixVersion).Replace('__ARCH__', $a.msix)
  Set-Content -Encoding utf8NoBOM -NoNewline (Join-Path $layout 'AppxManifest.xml') $manifest
  & $makeappx.FullName pack /o /h SHA256 /d $layout /p (Join-Path $packages "Skiff-$msixVersion-$($a.msix).msix")
  if ($LASTEXITCODE) { throw "makeappx pack failed ($($a.msix))" }
}

$bundle = Join-Path $OutDir "Skiff-$Version-windows.msixbundle"
& $makeappx.FullName bundle /o /bv $msixVersion /d $packages /p $bundle
if ($LASTEXITCODE) { throw 'makeappx bundle failed' }
Remove-Item -Recurse -Force $work
Write-Output $bundle
