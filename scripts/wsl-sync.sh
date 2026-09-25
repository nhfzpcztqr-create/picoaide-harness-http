#!/usr/bin/env bash
# WSL <-> Windows 代码同步脚本
# 在 WSL 中运行，通过 /mnt/d 访问 Windows 侧文件
#
# 用法:
#   ./scripts/wsl-sync.sh              # 双向同步（默认，--update 模式：仅覆盖更旧的文件）
#   ./scripts/wsl-sync.sh --push       # Windows -> WSL（客户端代码改动推到 WSL）
#   ./scripts/wsl-sync.sh --pull       # WSL -> Windows（服务端代码改动拉到 Windows）
#   ./scripts/wsl-sync.sh --dry-run    # 预览，不实际写入
#   ./scripts/wsl-sync.sh --watch      # 持续监控，自动同步
#
# 路径:
#   WSL:   /home/linny/picoaide-harness
#   WIN:   /mnt/d/picoaide-harness/picoaide-harness
set -euo pipefail

WSL_ROOT="/home/linny/picoaide-harness"
WIN_ROOT="/mnt/d/picoaide-harness/picoaide-harness"

# 排除模式：构建产物、运行时数据、平台特定文件、密钥
EXCLUDES=(
  --exclude=".git/"              # 各自维护独立 git 状态
  --exclude="node_modules/"      # 依赖在各平台独立安装
  --exclude="**/dist/"           # 构建输出
  --exclude="**/lib/"            # 编译产物（TS）
  --exclude="**/*.tsbuildinfo"
  --exclude=".build/"
  --exclude=".yarn/cache"
  --exclude=".yarn-cache/"
  --exclude=".yarn-home/"
  --exclude=".go-cache/"
  --exclude=".go-path/"
  --exclude=".npm-cache/"
  --exclude=".audit/"
  --exclude=".scan/"
  --exclude=".codeql-dbs/"
  --exclude=".research/"
  --exclude=".smoke/"
  --exclude=".dsh-home/"
  --exclude=".browser-store/"
  --exclude=".glitchtip-recon/"
  --exclude="temp/"
  --exclude=".agents/skills/"
  # server 构建产物 & 运行时数据
  --exclude="server/bin/"
  --exclude="server/data/"
  --exclude="server/pg-data/"
  --exclude="server/.dev-data"
  --exclude="server/.dev-data-*/"
  --exclude="server/picoaide-data/"
  --exclude="server/.picoaide-data/"
  --exclude="server/.worktrees/"
  --exclude="server/certs/"
  --exclude="server/caddy-data/"
  --exclude="server/caddy-config/"
  --exclude="server/deploy-backup/"
  --exclude="server/sockprobe"
  --exclude="server/.scan-staticcheck.txt"
  --exclude="server/webadmin/node_modules/"
  --exclude="server/webadmin/dist/"
  --exclude="server/webadmin/tsconfig.tsbuildinfo"
  --exclude="server/*.db"
  --exclude="server/*.db-wal"
  --exclude="server/*.db-shm"
  --exclude="server/dev-data"
  # 不同步 .env（包含环境特定配置与密钥，手动管理）
  --exclude="server/.env"
  # 渠道上下文 & 客户端资产（CI 产物）
  --exclude="channels-context/"
  --exclude="client-assets/"
  --exclude="channels/*"
  --exclude="!channels/README.md"
  --exclude="release-bundle/"
  --exclude="release-artifacts/"
  --exclude="transfer-out/"
  --exclude="image.tar"
  --exclude="*.tar.gz"
  --exclude="*.log"
  # E2E 运行时产物
  --exclude=".e2e-report.md"
  --exclude=".e2e-shots/"
  --exclude=".e2e-sidebar/"
  --exclude=".e2e-terminal/"
  --exclude=".e2e-foot-lane/"
  --exclude=".real-env-report.md"
  --exclude=".real-env-shots/"
  --exclude=".real-env-cron-report.md"
  --exclude=".real-env-cron-shots/"
  --exclude=".real-env-cron-flow-report.md"
  --exclude=".real-env-cron-flow-shots/"
  --exclude=".real-env-browser-report.md"
  --exclude=".real-env-browser-shots/"
  # deepseek-harness 子模块由 git submodule 管理，不同步
  --exclude="deepseek-harness/"
)

RSYNC_FLAGS=(-a --info=stats2,progress2 "${EXCLUDES[@]}")

DRY_RUN=false
MODE="both"  # both | push | pull
WATCH=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)  DRY_RUN=true; RSYNC_FLAGS+=(--dry-run); shift ;;
    --push)     MODE="push"; shift ;;
    --pull)     MODE="pull"; shift ;;
    --watch)    WATCH=true; shift ;;
    -h|--help)
      echo "用法: $0 [--push|--pull|--both] [--dry-run] [--watch]"
      echo "  --push      Windows -> WSL"
      echo "  --pull      WSL -> Windows"
      echo "  --both      双向同步（默认）"
      echo "  --dry-run   仅预览"
      echo "  --watch     持续监控自动同步"
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

do_sync() {
  local src="$1" dst="$2" label="$3"
  echo ""
  echo "=== $label ==="
  rsync "${RSYNC_FLAGS[@]}" "$src" "$dst"
}

if $WATCH; then
  # 持续监控模式：每 3 秒检查一次
  echo "[watch] 持续监控模式，每 3 秒同步一次（Ctrl-C 退出）"
  echo "[watch] 双向 --update 模式：仅覆盖更旧的文件"
  while true; do
    rsync -a --update --quiet "${EXCLUDES[@]}" "$WSL_ROOT/" "$WIN_ROOT/"
    rsync -a --update --quiet "${EXCLUDES[@]}" "$WIN_ROOT/" "$WSL_ROOT/"
    sleep 3
  done
  exit 0
fi

echo "源代码同步"
echo "  WSL:  $WSL_ROOT"
echo "  WIN:  $WIN_ROOT"
echo "  模式: $MODE"
$DRY_RUN && echo "  预览: 是（不实际写入）"

case "$MODE" in
  push)
    do_sync "$WIN_ROOT/" "$WSL_ROOT/" "Windows -> WSL"
    ;;
  pull)
    do_sync "$WSL_ROOT/" "$WIN_ROOT/" "WSL -> Windows"
    ;;
  both)
    # 双向：先 WSL->Win，再 Win->WSL，都用 --update 只覆盖更旧的
    RSYNC_FLAGS+=(-u)  # --update: skip files that are newer on receiver
    do_sync "$WSL_ROOT/" "$WIN_ROOT/" "WSL -> Windows (--update)"
    do_sync "$WIN_ROOT/" "$WSL_ROOT/" "Windows -> WSL (--update)"
    ;;
esac

echo ""
echo "同步完成。"
if $DRY_RUN; then
  echo "（以上为预览，未实际写入。去掉 --dry-run 执行同步。）"
fi
