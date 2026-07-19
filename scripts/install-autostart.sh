#!/bin/bash
# 安装 macOS 开机自启（LaunchAgent）：登录即运行，崩溃自动重启。
# 用法： ./scripts/install-autostart.sh
set -euo pipefail

APPDIR="$(cd "$(dirname "$0")/.." && pwd)"
LABEL="com.stocktrade.server"
PLIST="$HOME/Library/LaunchAgents/${LABEL}.plist"

echo "==> 项目目录: $APPDIR"

# 1) 检查密钥
if [[ ! -f "$APPDIR/config.json" && -z "${APCA_API_KEY_ID:-}" ]]; then
  echo "!! 未找到 config.json，请先 cp config.example.json config.json 并填入 Alpaca 密钥"
  exit 1
fi

# 2) 编译二进制
echo "==> 编译 stocktrade ..."
( cd "$APPDIR" && go build -o stocktrade . )

# 3) 生成并安装 plist
echo "==> 安装 LaunchAgent -> $PLIST"
mkdir -p "$HOME/Library/LaunchAgents"
sed "s#__APPDIR__#$APPDIR#g" "$APPDIR/scripts/${LABEL}.plist.template" > "$PLIST"

# 4) 重新加载
if launchctl list | grep -q "$LABEL"; then
  launchctl unload "$PLIST" 2>/dev/null || true
fi
launchctl load "$PLIST"

echo "==> 已安装并启动。访问 http://localhost:9010"
echo "    查看状态: launchctl list | grep $LABEL"
echo "    看日志:   tail -f \"$APPDIR/server.log\""
echo "    卸载:     ./scripts/uninstall-autostart.sh"
