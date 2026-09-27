param(
  [Parameter(Mandatory = $true)][string]$BundleRoot,
  [Parameter(Mandatory = $true)][string]$RepoRoot
)

$ErrorActionPreference = 'Stop'
$env:YARN_ENABLE_NETWORK = '0'
$env:npm_config_offline = 'true'
$env:COREPACK_ENABLE_NETWORK = '0'
$env:COREPACK_DEFAULT_TO_LATEST = '0'
$env:COREPACK_HOME = Join-Path $BundleRoot 'cache/corepack'

if (-not (Get-Command node -ErrorAction SilentlyContinue)) { throw 'Node.js 22.19+ is required' }
if (-not (Get-Command corepack -ErrorAction SilentlyContinue)) { throw 'Corepack is required' }
if (-not (Test-Path (Join-Path $BundleRoot 'SHA256SUMS'))) { throw 'Bundle has no SHA256SUMS' }
if (-not (Test-Path (Join-Path $BundleRoot 'toolchain-windows.txt'))) { throw 'Bundle has no toolchain-windows.txt' }
if (-not (Test-Path (Join-Path $BundleRoot 'restore-windows-cache.ps1'))) { throw 'Bundle has no Windows cache restore script' }
& (Join-Path $PSScriptRoot 'verify-bundle.ps1') -BundleRoot $BundleRoot
if (-not (Test-Path (Join-Path $RepoRoot 'yarn.lock'))) { throw 'Repository has no yarn.lock' }
$expectedToolchain = @(Get-Content (Join-Path $BundleRoot 'toolchain-windows.txt') | ForEach-Object { $_.Trim() })
if ($expectedToolchain.Count -ne 3) { throw 'Windows toolchain manifest is malformed' }
Push-Location $RepoRoot
try {
  $nodeVersion = node --version
  if ($LASTEXITCODE -ne 0) { throw "node --version failed with exit code $LASTEXITCODE" }
  $yarnVersion = corepack yarn --version
  if ($LASTEXITCODE -ne 0) { throw "corepack yarn --version failed with exit code $LASTEXITCODE" }
  Push-Location (Join-Path $RepoRoot 'deepseek-harness')
  try {
    $pnpmVersion = corepack pnpm --version
    if ($LASTEXITCODE -ne 0) { throw "corepack pnpm --version failed with exit code $LASTEXITCODE" }
  }
  finally { Pop-Location }
}
finally { Pop-Location }
$actualToolchain = @($nodeVersion, $yarnVersion, $pnpmVersion)
for ($index = 0; $index -lt $expectedToolchain.Count; $index++) {
  if ($actualToolchain[$index] -ne $expectedToolchain[$index]) {
    throw "Windows toolchain mismatch at line $($index + 1): expected $($expectedToolchain[$index]), got $($actualToolchain[$index])"
  }
}

Push-Location $RepoRoot
try {
  & (Join-Path $BundleRoot 'restore-windows-cache.ps1') -BundleRoot $BundleRoot -RepoRoot $RepoRoot
  corepack yarn install --immutable --immutable-cache
  if ($LASTEXITCODE -ne 0) { throw "offline Yarn install failed with exit code $LASTEXITCODE" }
  Push-Location (Join-Path $RepoRoot 'deepseek-harness')
  try {
    corepack pnpm install --frozen-lockfile --offline
    if ($LASTEXITCODE -ne 0) { throw "offline PNPM install failed with exit code $LASTEXITCODE" }
  }
  finally { Pop-Location }
  corepack yarn build
  if ($LASTEXITCODE -ne 0) { throw "Yarn build failed with exit code $LASTEXITCODE" }
  $packageStartedAt = Get-Date
  corepack yarn workspace dsh-plugin-desktop dist:win --no-prebuild --no-gates
  if ($LASTEXITCODE -ne 0) { throw "Windows NSIS packaging failed with exit code $LASTEXITCODE" }
  $artifact = Get-ChildItem 'packages/host/desktop/dist' -Filter '*Setup*.exe' |
    Where-Object { $_.LastWriteTime -ge $packageStartedAt } |
    Sort-Object LastWriteTime -Descending | Select-Object -First 1
  if ($null -eq $artifact) { throw 'Windows NSIS installer was not produced' }
  $hash = (Get-FileHash $artifact.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
  "$($artifact.Name)`t$hash`t$($artifact.Length)" |
    Set-Content 'packages/host/desktop/dist/WINDOWS-SHA256.txt'
  Write-Host "Windows installer: $($artifact.FullName)"
  Write-Host "SHA256: $hash"
}
finally {
  Pop-Location
}
