param(
  [Parameter(Mandatory = $true)][string]$OutputRoot,
  [string]$NodeVersion = '22.19.0',
  [ValidateSet('x64', 'arm64')][string]$Architecture = 'x64'
)

$ErrorActionPreference = 'Stop'
if ($NodeVersion -notmatch '^(22\.(?:19|[2-9]\d|\d{3,})|24)\.\d+\.\d+$') {
  throw 'Windows packaging supports Node 22.19+ or Node 24.x; Corepack must be bundled with Node.'
}
$OutputRoot = (New-Item -ItemType Directory -Force -Path $OutputRoot).FullName
$toolchainRoot = New-Item -ItemType Directory -Force -Path (Join-Path $OutputRoot 'toolchain')
$existingFiles = Get-ChildItem -LiteralPath $OutputRoot -File -Recurse -ErrorAction SilentlyContinue
if ($existingFiles) { throw "OutputRoot is not empty: $OutputRoot" }
$archiveName = "node-v$NodeVersion-win-$Architecture.zip"
$archivePath = Join-Path $toolchainRoot.FullName $archiveName
$downloadUrl = "https://nodejs.org/dist/v$NodeVersion/$archiveName"
Invoke-WebRequest -Uri $downloadUrl -OutFile $archivePath
$checksumsUrl = "https://nodejs.org/dist/v$NodeVersion/SHASUMS256.txt"
$checksumsPath = Join-Path $toolchainRoot.FullName 'SHASUMS256.txt'
Invoke-WebRequest -Uri $checksumsUrl -OutFile $checksumsPath
$checksumLine = Select-String -Path $checksumsPath -Pattern ("\s" + [regex]::Escape($archiveName) + "$") | Select-Object -First 1
if ($null -eq $checksumLine) { throw "Node checksum entry not found: $archiveName" }
$expectedHash = ($checksumLine.Line -split '\s+')[0].ToLowerInvariant()
$actualHash = (Get-FileHash $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
if ($expectedHash -ne $actualHash) { throw "Node archive checksum mismatch: expected $expectedHash, got $actualHash" }
"$NodeVersion" | Set-Content (Join-Path $toolchainRoot.FullName 'node-version.txt')
"$Architecture" | Set-Content (Join-Path $toolchainRoot.FullName 'architecture.txt')

$installer = @'
param(
  [string]$InstallRoot = (Join-Path $env:LOCALAPPDATA 'PicoAide\Toolchain')
)

$ErrorActionPreference = 'Stop'
$BundleRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
foreach ($line in Get-Content (Join-Path $BundleRoot 'SHA256SUMS')) {
  if ([string]::IsNullOrWhiteSpace($line)) { continue }
  $parts = $line -split '\s+', 2
  if ($parts.Count -ne 2) { throw "Invalid checksum line: $line" }
  $relative = $parts[1]
  if ([IO.Path]::IsPathRooted($relative) -or $relative -match '(^|[\\/])\.\.([\\/]|$)') {
    throw "Unsafe checksum path: $relative"
  }
  $path = Join-Path $BundleRoot $relative
  if (-not (Test-Path $path -PathType Leaf)) { throw "Missing bundle file: $relative" }
  $actual = (Get-FileHash $path -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne $parts[0].ToLowerInvariant()) { throw "Checksum mismatch: $relative" }
}

$nodeVersion = (Get-Content -Raw (Join-Path $BundleRoot 'toolchain/node-version.txt')).Trim()
$architecture = (Get-Content -Raw (Join-Path $BundleRoot 'toolchain/architecture.txt')).Trim()
$archiveName = "node-v$nodeVersion-win-$architecture.zip"
$archivePath = Join-Path $BundleRoot "toolchain/$archiveName"
$nodeDestination = Join-Path $InstallRoot "node-v$nodeVersion"
$activationPath = Join-Path $InstallRoot 'activate-picoaide-toolchain.ps1'
if ((Test-Path $nodeDestination) -or (Test-Path $activationPath)) {
  throw "Toolchain destination already exists: $InstallRoot. Choose another InstallRoot; existing files are not overwritten."
}
New-Item -ItemType Directory -Force -Path $InstallRoot | Out-Null
$staging = Join-Path $InstallRoot ('.install-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $staging | Out-Null
try {
  Expand-Archive -LiteralPath $archivePath -DestinationPath $staging
  $source = Join-Path $staging "node-v$nodeVersion-win-$architecture"
  if (-not (Test-Path (Join-Path $source 'node.exe'))) { throw 'Node archive layout is unexpected' }
  Move-Item -LiteralPath $source -Destination $nodeDestination
  $activation = @"
`$nodeBin = Join-Path `$PSScriptRoot 'node-v$nodeVersion'
`$env:PATH = "`$nodeBin;`$env:PATH"
"@
  Set-Content -LiteralPath $activationPath -Value $activation
}
finally {
  Remove-Item -LiteralPath $staging -Recurse -Force -ErrorAction SilentlyContinue
}
Write-Host "Installed Node $nodeVersion under $InstallRoot"
Write-Host "Activate in this PowerShell session with: . '$activationPath'"
'@
Set-Content -LiteralPath (Join-Path $OutputRoot 'install-windows-toolchain.ps1') -Value $installer

$sumFile = Join-Path $OutputRoot 'SHA256SUMS'
Get-ChildItem -LiteralPath $OutputRoot -File -Recurse |
  Where-Object { $_.FullName -ne $sumFile } |
  ForEach-Object {
    $relative = $_.FullName.Substring($OutputRoot.Length).TrimStart('\', '/').Replace('\', '/')
    "$((Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant())  $relative"
  } | Set-Content $sumFile
Write-Host "Prepared Windows toolchain bundle: $OutputRoot"
