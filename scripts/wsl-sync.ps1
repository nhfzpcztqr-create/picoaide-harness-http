# PowerShell 包装器：从 Windows 侧触发 WSL 同步脚本
# 用法:
#   .\scripts\wsl-sync.ps1              # 双向同步
#   .\scripts\wsl-sync.ps1 -Push        # Windows -> WSL
#   .\scripts\wsl-sync.ps1 -Pull        # WSL -> Windows
#   .\scripts\wsl-sync.ps1 -DryRun      # 预览
#   .\scripts\wsl-sync.ps1 -Watch       # 持续监控

param(
    [switch]$Push,
    [switch]$Pull,
    [switch]$DryRun,
    [switch]$Watch,
    [switch]$Both
)

$scriptPath = "D:\picoaide-harness\picoaide-harness\scripts\wsl-sync.sh"

$args = @()
if ($Push)  { $args += "--push" }
if ($Pull)  { $args += "--pull" }
if ($Both)  { $args += "--both" }
if ($DryRun) { $args += "--dry-run" }
if ($Watch)  { $args += "--watch" }

if ($args.Count -eq 0) { $args += "--both" }

wsl -d Ubuntu-24.04 -- bash -c "bash /mnt/d/picoaide-harness/picoaide-harness/scripts/wsl-sync.sh $($args -join ' ')"
