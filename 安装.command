#!/bin/bash
# 股票交易终端 · macOS 一键安装
# 在 Finder 里双击本文件即可（会在「终端」中运行）。
set -euo pipefail
cd "$(dirname "$0")"
APPDIR="$(pwd)"
LABEL="com.stocktrade.server"
PLIST="$HOME/Library/LaunchAgents/${LABEL}.plist"
PORT=9010

echo "==================================================="
echo "   股票交易终端 · 一键安装 (macOS)"
echo "==================================================="

# 1) 检查 Go（仅编译需要；运行不依赖 Go）
if ! command -v go >/dev/null 2>&1; then
  for p in /usr/local/go/bin /opt/homebrew/bin /usr/local/bin; do
    [ -x "$p/go" ] && export PATH="$p:$PATH"
  done
fi
if ! command -v go >/dev/null 2>&1; then
  echo "❌ 未检测到 Go（编译需要）。请先安装其一："
  echo "     • 官网下载:  https://go.dev/dl/"
  echo "     • 或 Homebrew:  brew install go"
  [ -t 0 ] && read -r -p "安装完 Go 后重新双击本文件。按回车退出…" _
  exit 1
fi
echo "✓ Go: $(go version)"

# 2) 配置 Alpaca 密钥（已存在则跳过）
if [ ! -f config.json ] && [ -z "${APCA_API_KEY_ID:-}" ]; then
  if [ -t 0 ]; then
    echo
    echo "需要 Alpaca 密钥（Paper 模拟盘 · https://alpaca.markets ）。直接回车可跳过，稍后手动填 config.json。"
    read -r -p "  API Key ID: " KID
    read -r -s -p "  API Secret Key（输入时不显示）: " KSEC; echo
    if [ -n "${KID:-}" ] && [ -n "${KSEC:-}" ]; then
      printf '{\n  "keyId": "%s",\n  "secretKey": "%s",\n  "feed": "iex"\n}\n' "$KID" "$KSEC" > config.json
      chmod 600 config.json
      echo "✓ 已写入 config.json（权限 600）"
    else
      echo "⚠ 已跳过。稍后： cp config.example.json config.json 并填入密钥"
    fi
  else
    echo "⚠ 未配置密钥。请手动： cp config.example.json config.json 并填入密钥"
  fi
else
  echo "✓ 已存在密钥配置，跳过"
fi

# 3) 编译
echo "==> 编译中…"
go build -o stocktrade .
echo "✓ 编译完成"

# 4) 安装并加载 LaunchAgent（登录即启动、崩溃自动重启）
echo "==> 设置开机自启…"
mkdir -p "$HOME/Library/LaunchAgents"
sed "s#__APPDIR__#$APPDIR#g" "scripts/${LABEL}.plist.template" > "$PLIST"
launchctl unload "$PLIST" 2>/dev/null || true
launchctl load "$PLIST"
echo "✓ 已设为开机自启并启动"

# 5) 打印访问地址
sleep 2
IP="$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || true)"
HOST="$(scutil --get LocalHostName 2>/dev/null || hostname -s)"
echo
echo "==================================================="
echo "   ✅ 安装完成！在浏览器打开："
echo
echo "     本机：      http://localhost:$PORT"
[ -n "${HOST:-}" ] && echo "     主机名：    http://${HOST}.local:$PORT   （推荐·IP变了也不变）"
[ -n "${IP:-}" ]   && echo "     手机同WiFi：http://${IP}:$PORT"
echo
echo "   卸载：双击「卸载.command」"
echo "   看日志：tail -f \"$APPDIR/server.log\""
echo "==================================================="
[ -t 0 ] && read -r -p "按回车关闭此窗口…" _
exit 0
