#!/bin/bash
# 卸载 macOS 开机自启。
set -euo pipefail
LABEL="com.stocktrade.server"
PLIST="$HOME/Library/LaunchAgents/${LABEL}.plist"
if [[ -f "$PLIST" ]]; then
  launchctl unload "$PLIST" 2>/dev/null || true
  rm -f "$PLIST"
  echo "已卸载 $LABEL"
else
  echo "未安装"
fi
