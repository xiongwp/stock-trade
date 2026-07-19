#!/bin/bash
# 构建 macOS 图形安装包 dist/StockTrade-<版本>.pkg
# 特性：universal 二进制、开机自启、数据与程序分离、重复安装即升级且数据兼容。
set -euo pipefail
cd "$(dirname "$0")/.."
APPDIR="$(pwd)"
ID="com.stocktrade.server"
VERSION="$(tr -d ' \n' < VERSION 2>/dev/null || echo 1.0.0)"
BIN_INSTALL="/usr/local/stocktrade/stocktrade"

BUILD="$APPDIR/build-pkg"
ROOT="$BUILD/root"
SCRIPTS="$BUILD/scripts"
rm -rf "$BUILD"
mkdir -p "$ROOT/usr/local/stocktrade" "$SCRIPTS" "$APPDIR/dist"

echo "==> 编译 universal 二进制 (arm64 + amd64)…"
GOOS=darwin GOARCH=arm64 go build -ldflags "-X main.version=$VERSION" -o "$BUILD/arm64" .
GOOS=darwin GOARCH=amd64 go build -ldflags "-X main.version=$VERSION" -o "$BUILD/amd64" .
lipo -create -output "$ROOT/usr/local/stocktrade/stocktrade" "$BUILD/arm64" "$BUILD/amd64"
chmod +x "$ROOT/usr/local/stocktrade/stocktrade"
echo "    $(lipo -info "$ROOT/usr/local/stocktrade/stocktrade")"

echo "==> 生成 postinstall（安装/升级时执行：迁移数据、装自启、重载）…"
cat > "$SCRIPTS/postinstall" <<'POST'
#!/bin/bash
set -e
ID="com.stocktrade.server"
BIN="/usr/local/stocktrade/stocktrade"

# 目标用户（安装器以 root 运行，需定位当前登录用户）
CU="${USER:-}"
[ -z "$CU" ] || [ "$CU" = "root" ] && CU="$(stat -f%Su /dev/console)"
UID_="$(id -u "$CU")"
UH="$(dscl . -read "/Users/$CU" NFSHomeDirectory 2>/dev/null | awk '{print $2}')"
[ -z "$UH" ] && UH="/Users/$CU"

DATA="$UH/Library/Application Support/StockTrade"
LA="$UH/Library/LaunchAgents"
PLIST="$LA/${ID}.plist"

mkdir -p "$DATA" "$LA"

# 数据迁移：数据目录若还没有 db/config，从旧安装(旧 plist 的 WorkingDirectory)拷贝，保证升级/迁移数据兼容
if [ -f "$PLIST" ]; then
  OLD_WD="$(/usr/libexec/PlistBuddy -c 'Print :WorkingDirectory' "$PLIST" 2>/dev/null || true)"
  if [ -n "$OLD_WD" ] && [ -d "$OLD_WD" ] && [ "$OLD_WD" != "$DATA" ]; then
    [ -f "$DATA/stock-trade.db" ] || { [ -f "$OLD_WD/stock-trade.db" ] && cp -f "$OLD_WD/stock-trade.db" "$DATA/"; }
    [ -f "$DATA/config.json" ]    || { [ -f "$OLD_WD/config.json" ]    && cp -f "$OLD_WD/config.json" "$DATA/"; }
  fi
fi

# 写 LaunchAgent：运行固定路径二进制，工作目录=数据目录（db/config 落在这里，升级不动）
cat > "$PLIST" <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>${ID}</string>
  <key>ProgramArguments</key><array><string>${BIN}</string></array>
  <key>WorkingDirectory</key><string>${DATA}</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>5</integer>
  <key>StandardOutPath</key><string>${DATA}/server.log</string>
  <key>StandardErrorPath</key><string>${DATA}/server.log</string>
</dict></plist>
PL

chown "$CU" "$PLIST"
chown -R "$CU" "$DATA"

# 在用户上下文重载（升级时先卸旧再装新）
launchctl bootout "gui/$UID_/${ID}" 2>/dev/null || launchctl asuser "$UID_" launchctl unload "$PLIST" 2>/dev/null || true
launchctl bootstrap "gui/$UID_" "$PLIST" 2>/dev/null || launchctl asuser "$UID_" launchctl load "$PLIST" 2>/dev/null || true
exit 0
POST
chmod +x "$SCRIPTS/postinstall"

echo "==> pkgbuild + productbuild…"
pkgbuild --identifier "$ID" --version "$VERSION" \
  --root "$ROOT" --scripts "$SCRIPTS" --install-location "/" \
  "$BUILD/component.pkg" >/dev/null

productbuild --identifier "$ID" --version "$VERSION" \
  --package "$BUILD/component.pkg" \
  "$APPDIR/dist/StockTrade-$VERSION.pkg" >/dev/null

echo "✓ 已生成: dist/StockTrade-$VERSION.pkg"
echo "  双击安装（未签名首次需 右键→打开）。再次双击更高版本即升级，数据自动保留。"
