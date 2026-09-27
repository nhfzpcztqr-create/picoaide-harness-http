param(
  [Parameter(Mandatory = $true)][string]$OutputRoot,
  [Parameter(Mandatory = $true)][string]$RepoRoot
)

$ErrorActionPreference = 'Stop'
$OutputRoot = (New-Item -ItemType Directory -Force -Path $OutputRoot).FullName
$existingFiles = Get-ChildItem -LiteralPath $OutputRoot -File -Recurse -ErrorAction SilentlyContinue
if ($existingFiles) { throw "OutputRoot is not empty: $OutputRoot" }
$cacheRoot = New-Item -ItemType Directory -Force -Path (Join-Path $OutputRoot 'cache')
$env:COREPACK_HOME = Join-Path $cacheRoot.FullName 'corepack'
$env:COREPACK_ENABLE_NETWORK = '1'
$env:COREPACK_DEFAULT_TO_LATEST = '0'

if (-not (Get-Command node -ErrorAction SilentlyContinue)) { throw 'Node.js 22.19+ is required' }
if (-not (Get-Command corepack -ErrorAction SilentlyContinue)) { throw 'Corepack is required' }
if (-not (Get-Command tar -ErrorAction SilentlyContinue)) { throw 'tar.exe is required (included with supported Windows)' }

Push-Location $RepoRoot
try {
  corepack yarn install --immutable
  if ($LASTEXITCODE -ne 0) { throw "corepack yarn install failed with exit code $LASTEXITCODE" }
  $nodeVersion = node --version
  if ($LASTEXITCODE -ne 0) { throw "node --version failed with exit code $LASTEXITCODE" }
  $yarnVersion = corepack yarn --version
  if ($LASTEXITCODE -ne 0) { throw "corepack yarn --version failed with exit code $LASTEXITCODE" }
  Push-Location (Join-Path $RepoRoot 'deepseek-harness')
  try {
    corepack pnpm install --frozen-lockfile
    if ($LASTEXITCODE -ne 0) { throw "online PNPM install failed with exit code $LASTEXITCODE" }
    $pnpmVersion = corepack pnpm --version
    if ($LASTEXITCODE -ne 0) { throw "corepack pnpm --version failed with exit code $LASTEXITCODE" }
  }
  finally { Pop-Location }
  corepack yarn build
  if ($LASTEXITCODE -ne 0) { throw "corepack yarn build failed with exit code $LASTEXITCODE" }
  $packageStartedAt = Get-Date
  corepack yarn workspace dsh-plugin-desktop dist:win --no-prebuild --no-gates
  if ($LASTEXITCODE -ne 0) { throw "Windows package build failed with exit code $LASTEXITCODE" }
  $artifact = Get-ChildItem (Join-Path $RepoRoot 'packages/host/desktop/dist') -Filter '*Setup*.exe' |
    Where-Object { $_.LastWriteTime -ge $packageStartedAt } |
    Sort-Object LastWriteTime -Descending | Select-Object -First 1
  if ($null -eq $artifact) { throw 'Windows NSIS installer was not produced' }
  $yarnCache = Join-Path $RepoRoot '.yarn/cache'
  if (-not (Test-Path $yarnCache)) { throw 'Yarn cache was not created' }
  tar.exe -czf (Join-Path $cacheRoot 'yarn-cache.tgz') -C $RepoRoot '.yarn/cache'
  if ($LASTEXITCODE -ne 0) { throw "Yarn cache archive failed with exit code $LASTEXITCODE" }
  Push-Location (Join-Path $RepoRoot 'deepseek-harness')
  try { $pnpmStore = corepack pnpm store path }
  finally { Pop-Location }
  if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($pnpmStore)) { throw 'Unable to locate PNPM store' }
  if (-not (Test-Path $pnpmStore)) { throw "PNPM store does not exist: $pnpmStore" }
  tar.exe -czf (Join-Path $cacheRoot 'pnpm-store.tgz') -C (Split-Path $pnpmStore) (Split-Path $pnpmStore -Leaf)
  if ($LASTEXITCODE -ne 0) { throw "PNPM store archive failed with exit code $LASTEXITCODE" }
}
finally {
  Pop-Location
}

$corepackHome = $env:COREPACK_HOME
if (-not (Test-Path $corepackHome)) { throw "Corepack cache was not created: $corepackHome" }
tar.exe -czf (Join-Path $cacheRoot 'corepack.tgz') -C (Split-Path $corepackHome) (Split-Path $corepackHome -Leaf)
if ($LASTEXITCODE -ne 0) { throw "Corepack cache archive failed with exit code $LASTEXITCODE" }

$electronCache = Join-Path $env:LOCALAPPDATA 'electron/Cache'
$builderCache = Join-Path $env:LOCALAPPDATA 'electron-builder/Cache'
if (-not (Test-Path $electronCache)) { throw "Electron cache was not created: $electronCache" }
if (-not (Test-Path $builderCache)) { throw "electron-builder cache was not created: $builderCache" }

tar.exe -czf (Join-Path $cacheRoot 'electron-cache.tgz') -C (Split-Path $electronCache) (Split-Path $electronCache -Leaf)
if ($LASTEXITCODE -ne 0) { throw "Electron cache archive failed with exit code $LASTEXITCODE" }
tar.exe -czf (Join-Path $cacheRoot 'electron-builder-cache.tgz') -C (Split-Path $builderCache) (Split-Path $builderCache -Leaf)
if ($LASTEXITCODE -ne 0) { throw "electron-builder cache archive failed with exit code $LASTEXITCODE" }

@(
  $nodeVersion,
  $yarnVersion,
  $pnpmVersion
) | Set-Content (Join-Path $OutputRoot 'toolchain-windows.txt')

Copy-Item (Join-Path $PSScriptRoot 'restore-windows-cache.ps1') $OutputRoot -Force
$sumFile = Join-Path $OutputRoot 'SHA256SUMS'
Get-ChildItem -LiteralPath $OutputRoot -File -Recurse |
  Where-Object { $_.Name -ne 'SHA256SUMS' } |
  ForEach-Object {
    $relative = $_.FullName.Substring($OutputRoot.Length).TrimStart('\', '/').Replace('\', '/')
    "$((Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant())  $relative"
  } | Set-Content $sumFile
Write-Host "Prepared Windows offline bundle: $OutputRoot"
