# 股票交易终端（本地网页版）

基于 Go 标准库 + SQLite 的本机股票分析程序，行情来自 [Alpaca](https://alpaca.markets)。
浏览器打开 **http://localhost:9010** 使用。24 小时运行、开机自启、崩溃自动重启。

## 功能

- **真实行情**：通过 Alpaca 行情 API 拉取美股数据（近 1 年日线本地长期保存）
- **专业 K 线图**：TradingView lightweight-charts，含 MA5/MA20 均线与成交量（离线可用）
- **手动记录买入**：为每只股票录入买入价、时间、数量、备注（支持多笔）
- **美金盈亏**：按最新价实时计算每笔 / 每股 / 总浮动盈亏（$ 和 %）
- **10 大策略胜率排行**：内置 10 个经典技术策略，用近 1 年真实数据回测排名
  （均值回归、RSI、布林带、随机指标 KD、MACD、均线、EMA、唐奇安通道、动量 ROC）
- **实时信号 + 共振提醒**：后台每 30 秒评估所有策略，≥3 个策略同向即生成买/卖共振提醒
- **检索**：按代码 / 名称检索全部可交易美股，支持 100+ 只同时跟踪
- **永不死机**：进程崩溃由 launchd 自动拉起；HTTP/后台循环 panic 自动恢复；网络错误自动重试

> ⚠️ 策略胜率来自历史回测（不含手续费/滑点），仅供分析参考，**非投资建议**。
> 程序只生成买/卖信号并展示，**不自动下单**。

## 准备 Alpaca 密钥

1. 注册并登录 Alpaca，进入 Paper Trading（模拟盘）控制台，生成 API Key（Key ID + Secret Key）
2. 提供密钥（**不要提交到 git**）：
   ```bash
   cp config.example.json config.json   # 然后编辑填入密钥
   ```
   或用环境变量 `APCA_API_KEY_ID` / `APCA_API_SECRET_KEY`（可选 `APCA_FEED=iex`）。
   免费账户用 `iex` 数据源。

## 安装 / 运行

**方式一：一键安装（推荐）**

在 Finder 里双击 **`安装.command`** —— 会自动检测 Go、（首次）引导填入 Alpaca 密钥、编译、设为开机自启并启动，最后打印本机 / 主机名 / 手机同 WiFi 三个访问地址。
卸载：双击 **`卸载.command`**（保留本地数据与密钥）。

> 若双击提示「无法打开（未验证的开发者）」，右键点该文件 → 打开 → 打开；或在终端 `bash 安装.command`。

**方式二：图形安装包 `.pkg`（像装普通 Mac 软件）**

```bash
./scripts/build-pkg.sh          # 生成 dist/StockTrade-<版本>.pkg（universal 二进制）
```
双击 `dist/StockTrade-*.pkg`，一路「继续」即可。它会把程序装到 `/usr/local/stocktrade/`、
数据放到 `~/Library/Application Support/StockTrade/`（config.json、stock-trade.db），并设为开机自启。

- **版本升级**：改 `VERSION` 里的版本号 → 重新 `build-pkg.sh` → 双击新 `.pkg` 即自动升级（同一包标识）。
- **数据兼容**：升级只替换程序、不动数据目录；表结构变更由程序启动时自动迁移。首次从旧安装切换时，
  会自动把旧的 `stock-trade.db` / `config.json` 迁移到数据目录，历史记录不丢。
- 未签名，首次双击若被拦：右键该 `.pkg` → 打开。

**方式三：命令行**
```bash
go run .                            # 前台运行
# 或开机自启：
./scripts/install-autostart.sh      # 安装 LaunchAgent
./scripts/uninstall-autostart.sh    # 卸载
```

服务监听 `0.0.0.0:9010`：本机开 http://localhost:9010 ，同一 WiFi 的手机开 `http://<主机名>.local:9010` 或 `http://<局域网IP>:9010`（页面顶栏和启动日志都会显示这些地址）。

## 测试

```bash
go test ./...
```

## 说明

- 数据存于本地 `stock-trade.db`（SQLite，WAL 模式），重启不丢，开机自动补拉历史。
- 端口 9010，仅监听 localhost。
