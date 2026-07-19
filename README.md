# 股票交易终端（本地网页版）

基于 Go 标准库 + SQLite 的本机股票交易分析程序，行情来自 [Alpaca](https://alpaca.markets)。
浏览器打开 `http://localhost:8080` 使用。

## 功能

- **真实行情**：通过 Alpaca 行情 API 拉取美股数据
- **专业 K 线图**：TradingView lightweight-charts，含 MA5/MA20 均线与成交量（离线可用）
- **手动记录买入**：为每只股票录入买入价、时间、数量、备注（支持多笔）
- **美金盈亏**：按最新价实时计算每只股票 / 每笔买入的浮动盈亏与总盈亏
- **本地保存近 1 年日线**：首次添加即拉取并落地 SQLite，长期保存
- **策略实时提醒**：后台每 30 秒评估 MA5/MA20 金叉死叉、RSI 超买超卖，生成提醒
- **检索**：按代码 / 名称检索全部可交易美股
- 支持 100+ 只股票同时跟踪

## 准备 Alpaca 密钥

1. 注册并登录 Alpaca，进入 Paper Trading（模拟盘）控制台
2. 生成 API Key（会得到 **Key ID** 与 **Secret Key**）
3. 用以下任一方式提供密钥（**不要提交到 git**）：

**方式 A：环境变量**
```bash
export APCA_API_KEY_ID=你的KeyID
export APCA_API_SECRET_KEY=你的SecretKey
export APCA_FEED=iex   # 免费档用 iex；有订阅可用 sip
```

**方式 B：config.json**（在项目根目录）
```bash
cp config.example.json config.json
# 编辑 config.json 填入密钥
```

> 免费账户请用 `iex` 数据源。K 线为日线，覆盖近 1 年。

## 运行

```bash
go run .
# 或编译后运行
go build -o stocktrade . && ./stocktrade
```

打开 http://localhost:8080

## 测试

```bash
go test ./...
```

## 说明

- 数据存于本地 `stock-trade.db`（SQLite，WAL 模式），重启不丢。
- 策略提醒为分析辅助，非投资建议。
- 本程序仅做行情分析与手动记账，不代下单。
