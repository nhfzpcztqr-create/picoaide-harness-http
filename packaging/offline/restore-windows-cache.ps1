param(
  [Parameter(Mandatory = $true)][string]$BundleRoot,
  [Parameter(Mandatory = $true)][string]$RepoRoot
)

$ErrorActionPreference = 'Stop'
if (-not (Get-Command tar -ErrorAction SilentlyContinue)) { throw 'tar.exe is required' }
if ([string]::IsNullOrWhiteSpace($env:COREPACK_HOME)) {
  $env:COREPACK_HOME = Join-Path $BundleRoot 'cache/corepack'
}

function Invoke-Checked {
  param(
    [Parameter(Mandatory = $true)][string]$FilePath,
    [Parameter(Mandatory = $false)][string[]]$ArgumentList = @()
  )
  & $FilePath @ArgumentList
  if ($LASTEXITCODE -ne 0) { throw "$FilePath failed with exit code $LASTEXITCODE" }
}

$yarnArchive = Join-Path $BundleRoot 'cache/yarn-cache.tgz'
if (-not (Test-Path $yarnArchive)) { throw "Missing $yarnArchive" }
if (-not (Test-Path (Join-Path $BundleRoot 'cache/pnpm-store.tgz'))) { throw 'Missing PNPM store cache' }
if (-not (Test-Path (Join-Path $BundleRoot 'cache/corepack.tgz'))) { throw 'Missing Corepack cache' }
New-Item -ItemType Directory -Force -Path (Split-Path $env:COREPACK_HOME) | Out-Null
Invoke-Checked -FilePath 'tar.exe' -ArgumentList @('-xzf', (Join-Path $BundleRoot 'cache/corepack.tgz'), '-C', (Split-Path $env:COREPACK_HOME))
Invoke-Checked -FilePath 'tar.exe' -ArgumentList @('-xzf', $yarnArchive, '-C', $RepoRoot)

Push-Location (Join-Path $RepoRoot 'deepseek-harness')
try { $pnpmStore = (& corepack pnpm store path).Trim() }
finally { Pop-Location }
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($pnpmStore)) { throw 'Unable to locate PNPM store' }
New-Item -ItemType Directory -Force -Path (Split-Path $pnpmStore) | Out-Null
Invoke-Checked -FilePath 'tar.exe' -ArgumentList @('-xzf', (Join-Path $BundleRoot 'cache/pnpm-store.tgz'), '-C', (Split-Path $pnpmStore))

$electronParent = Join-Path $env:LOCALAPPDATA 'electron'
$builderParent = Join-Path $env:LOCALAPPDATA 'electron-builder'
New-Item -ItemType Directory -Force -Path $electronParent, $builderParent | Out-Null
Invoke-Checked -FilePath 'tar.exe' -ArgumentList @('-xzf', (Join-Path $BundleRoot 'cache/electron-cache.tgz'), '-C', $electronParent)
Invoke-Checked -FilePath 'tar.exe' -ArgumentList @('-xzf', (Join-Path $BundleRoot 'cache/electron-builder-cache.tgz'), '-C', $builderParent)
Write-Host 'Restored Yarn, Electron, and electron-builder caches.'
