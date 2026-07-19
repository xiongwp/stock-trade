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

## 运行

**手动运行：**
```bash
go run .            # 或 go build -o stocktrade . && ./stocktrade
```

**开机自启（macOS，24 小时运行）：**
```bash
./scripts/install-autostart.sh     # 安装 LaunchAgent，登录即启动、崩溃自动重启
./scripts/uninstall-autostart.sh   # 卸载
launchctl list | grep com.stocktrade.server   # 查看状态
tail -f server.log                             # 查看日志
```

打开 http://localhost:9010

## 测试

```bash
go test ./...
```

## 说明

- 数据存于本地 `stock-trade.db`（SQLite，WAL 模式），重启不丢，开机自动补拉历史。
- 端口 9010，仅监听 localhost。
