#!/bin/bash
# 股票交易终端 · 卸载开机自启（在 Finder 里双击运行）
# 只移除自启与运行中的服务；本地数据 stock-trade.db 与 config.json 保留。
set -euo pipefail
cd "$(dirname "$0")"
LABEL="com.stocktrade.server"
PLIST="$HOME/Library/LaunchAgents/${LABEL}.plist"

if [ -f "$PLIST" ]; then
  launchctl unload "$PLIST" 2>/dev/null || true
  rm -f "$PLIST"
  echo "✓ 已卸载开机自启并停止服务。"
else
  echo "（未发现已安装的自启项）"
fi
echo "  数据与密钥仍保留：$(pwd)/stock-trade.db , config.json"
echo "  重新安装：双击「安装.command」"
[ -t 0 ] && read -r -p "按回车关闭此窗口…" _
exit 0
