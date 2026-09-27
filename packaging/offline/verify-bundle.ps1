param(
  [Parameter(Mandatory = $true)][string]$BundleRoot
)

$ErrorActionPreference = 'Stop'
$sumFile = Join-Path $BundleRoot 'SHA256SUMS'
if (-not (Test-Path $sumFile)) { throw "Missing $sumFile" }

foreach ($line in Get-Content $sumFile) {
  if ([string]::IsNullOrWhiteSpace($line)) { continue }
  $parts = $line -split '\s+', 2
  if ($parts.Count -ne 2) { throw "Invalid checksum line: $line" }
  $path = Join-Path $BundleRoot $parts[1]
  if (-not (Test-Path $path -PathType Leaf)) { throw "Missing bundle file: $($parts[1])" }
  $actual = (Get-FileHash $path -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne $parts[0].ToLowerInvariant()) { throw "Checksum mismatch: $($parts[1])" }
}
Write-Host "Bundle verified: $BundleRoot"
