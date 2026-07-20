package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	tradingBase = "https://paper-api.alpaca.markets/v2"
	dataBase    = "https://data.alpaca.markets/v2"
)

// Alpaca 是对 Alpaca 交易 API 与行情 API 的轻量封装。
type Alpaca struct {
	cfg    Config
	client *http.Client
}

func newAlpaca(cfg Config) *Alpaca {
	return &Alpaca{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second}}
}

// etLoc 缓存美东时区，用于判断当前处于哪个交易时段。
var etLoc, _ = time.LoadLocation("America/New_York")

// isOvernightSession 判断给定时刻是否处于夜盘时段（美东 20:00–次日 04:00）。
// 夜盘由 Blue Ocean ATS 支撑，此时段 IEX 无成交，需切换到 overnight feed。
func isOvernightSession(t time.Time) bool {
	if etLoc == nil {
		return false
	}
	h := t.In(etLoc).Hour()
	return h >= 20 || h < 4
}

// liveFeed 根据当前美东时段返回实时报价应使用的 feed。
//   - sip（付费）覆盖全时段，直接沿用用户配置；
//   - 免费 iex 已覆盖盘前(04:00–09:30)、正常盘、盘后(16:00–20:00)，
//     仅在夜盘(20:00–次日04:00)切换到 overnight。
func (a *Alpaca) liveFeed() string {
	if a.cfg.Feed != "" && a.cfg.Feed != "iex" {
		return a.cfg.Feed
	}
	if isOvernightSession(time.Now()) {
		return "overnight"
	}
	return "iex"
}

func (a *Alpaca) do(method, rawURL string) ([]byte, error) {
	if a.cfg.KeyID == "" || a.cfg.SecretKey == "" {
		return nil, fmt.Errorf("尚未配置 Alpaca 密钥，请在数据目录的 config.json 填入 keyId/secretKey 后重启")
	}
	var lastErr error
	// 对网络错误 / 429 / 5xx 做最多 3 次指数退避重试，提升长时间运行的韧性。
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 800 * time.Millisecond)
		}
		req, err := http.NewRequest(method, rawURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("APCA-API-KEY-ID", a.cfg.KeyID)
		req.Header.Set("APCA-API-SECRET-KEY", a.cfg.SecretKey)
		req.Header.Set("Accept", "application/json")

		resp, err := a.client.Do(req)
		if err != nil {
			lastErr = err
			continue // 网络错误，重试
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("alpaca %s 返回 %d: %s", rawURL, resp.StatusCode, strings.TrimSpace(string(body)))
			continue // 限流/服务端错误，重试
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("alpaca %s 返回 %d: %s", rawURL, resp.StatusCode, strings.TrimSpace(string(body)))
		}
		return body, nil
	}
	return nil, lastErr
}

// Bar 是一根 K 线（OHLCV）。
type Bar struct {
	T time.Time `json:"t"`
	O float64   `json:"o"`
	H float64   `json:"h"`
	L float64   `json:"l"`
	C float64   `json:"c"`
	V int64     `json:"v"`
}

// isMinuteTF 判断是否为分钟级周期（延长时段/夜盘只对分钟图有意义）。
func isMinuteTF(tf string) bool { return strings.HasSuffix(tf, "Min") }

// inRegularSession 判断某时刻是否落在美股常规时段（美东 09:30–16:00，周一至周五）。
func inRegularSession(t time.Time) bool {
	if etLoc == nil {
		return true
	}
	et := t.In(etLoc)
	if wd := et.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	m := et.Hour()*60 + et.Minute()
	return m >= 9*60+30 && m < 16*60
}

// GetBars 拉取某只股票在 [start, end] 区间内指定周期的 K 线。
//
// extended 仅对分钟图生效：
//   - false（默认）：只保留常规时段(09:30–16:00)蜡烛，图面干净，不与日间数据混淆；
//   - true：叠加盘前(04:00–09:30)/盘后(16:00–20:00，均来自 iex)与夜盘(20:00–次日04:00，
//     来自 overnight/Blue Ocean ATS)，拼出 24 小时全时段。
//
// 付费 sip feed 本身已含全时段延长数据，直接单次拉取。
func (a *Alpaca) GetBars(symbol, timeframe string, start, end time.Time, extended bool) ([]Bar, error) {
	base := a.cfg.Feed
	if base == "" {
		base = "iex"
	}
	// 非分钟周期，或 sip（已覆盖全时段），单次拉取即可。
	if !isMinuteTF(timeframe) || base == "sip" {
		return a.getBarsFeed(symbol, timeframe, base, start, end)
	}

	// 免费 iex：先拉日间（含盘前/盘中/盘后）。
	day, err := a.getBarsFeed(symbol, timeframe, "iex", start, end)
	if err != nil {
		return nil, err
	}
	if !extended {
		return filterBars(day, inRegularSession), nil
	}
	// 延长时段：再叠加夜盘蜡烛，拼出 24 小时。夜盘不可用时退回日间延长，不致命。
	night, err := a.getBarsFeed(symbol, timeframe, "overnight", start, end)
	if err != nil {
		return day, nil
	}
	return mergeBars(day, night), nil
}

// filterBars 原地保留满足 keep 的蜡烛。
func filterBars(bars []Bar, keep func(time.Time) bool) []Bar {
	out := bars[:0]
	for _, b := range bars {
		if keep(b.T) {
			out = append(out, b)
		}
	}
	return out
}

// mergeBars 合并两组蜡烛，按时间升序并去除重复时间戳。
func mergeBars(a, b []Bar) []Bar {
	all := make([]Bar, 0, len(a)+len(b))
	all = append(all, a...)
	all = append(all, b...)
	sort.Slice(all, func(i, j int) bool { return all[i].T.Before(all[j].T) })
	out := all[:0]
	var last time.Time
	for _, x := range all {
		if len(out) == 0 || !x.T.Equal(last) {
			out = append(out, x)
			last = x.T
		}
	}
	return out
}

// getBarsFeed 用指定 feed 拉取某只股票在 [start, end] 区间的 K 线，自动翻页。
func (a *Alpaca) getBarsFeed(symbol, timeframe, feed string, start, end time.Time) ([]Bar, error) {
	var all []Bar
	pageToken := ""
	for {
		q := url.Values{}
		q.Set("timeframe", timeframe)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		q.Set("limit", "10000")
		q.Set("adjustment", "split")
		q.Set("feed", feed)
		if pageToken != "" {
			q.Set("page_token", pageToken)
		}
		u := fmt.Sprintf("%s/stocks/%s/bars?%s", dataBase, url.PathEscape(symbol), q.Encode())
		body, err := a.do(http.MethodGet, u)
		if err != nil {
			return nil, err
		}
		var out struct {
			Bars          []Bar   `json:"bars"`
			NextPageToken *string `json:"next_page_token"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Bars...)
		if out.NextPageToken == nil || *out.NextPageToken == "" {
			break
		}
		pageToken = *out.NextPageToken
	}
	return all, nil
}

// Snapshot 是某只股票的最新快照。
type Snapshot struct {
	Symbol    string
	Price     float64 // 最新成交价
	PrevClose float64 // 昨日收盘
}

type rawSnapshot struct {
	LatestTrade *struct {
		P float64 `json:"p"`
	} `json:"latestTrade"`
	LatestQuote *struct {
		Bp float64 `json:"bp"` // 买价
		Ap float64 `json:"ap"` // 卖价
	} `json:"latestQuote"`
	DailyBar     *struct{ C float64 `json:"c"` } `json:"dailyBar"`
	PrevDailyBar *struct{ C float64 `json:"c"` } `json:"prevDailyBar"`
}

// GetSnapshots 批量获取多只股票的最新快照（按 100 个一批）。
// feed 按当前美东时段自动选择：夜盘走 overnight，其余走 iex/sip。
func (a *Alpaca) GetSnapshots(symbols []string) (map[string]Snapshot, error) {
	result := make(map[string]Snapshot)
	feed := a.liveFeed()
	overnight := feed == "overnight"
	for i := 0; i < len(symbols); i += 100 {
		end := i + 100
		if end > len(symbols) {
			end = len(symbols)
		}
		batch := symbols[i:end]
		q := url.Values{}
		q.Set("symbols", strings.Join(batch, ","))
		q.Set("feed", feed)
		u := fmt.Sprintf("%s/stocks/snapshots?%s", dataBase, q.Encode())
		body, err := a.do(http.MethodGet, u)
		if err != nil {
			return nil, err
		}
		var raw map[string]rawSnapshot
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, err
		}
		for sym, s := range raw {
			snap := Snapshot{Symbol: sym}
			// 夜盘时 latestTrade 延迟 15 分钟，latestQuote 是实时指示性报价，
			// 故夜盘优先用买卖中间价；其余时段优先用最新成交价。
			trade := 0.0
			if s.LatestTrade != nil {
				trade = s.LatestTrade.P
			}
			mid := 0.0
			if s.LatestQuote != nil && s.LatestQuote.Bp > 0 && s.LatestQuote.Ap > 0 {
				mid = (s.LatestQuote.Bp + s.LatestQuote.Ap) / 2
			}
			switch {
			case overnight && mid > 0:
				snap.Price = mid
			case trade > 0:
				snap.Price = trade
			case mid > 0:
				snap.Price = mid
			case s.DailyBar != nil:
				snap.Price = s.DailyBar.C
			}
			if s.PrevDailyBar != nil {
				snap.PrevClose = s.PrevDailyBar.C
			}
			result[sym] = snap
		}
	}
	return result, nil
}

// Asset 是一只可交易资产（用于检索）。
type Asset struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Exchange string `json:"exchange"`
	Tradable bool   `json:"tradable"`
}

// ListAssets 拉取全部活跃美股资产（结果较大，调用方应缓存）。
func (a *Alpaca) ListAssets() ([]Asset, error) {
	u := fmt.Sprintf("%s/assets?status=active&asset_class=us_equity", tradingBase)
	body, err := a.do(http.MethodGet, u)
	if err != nil {
		return nil, err
	}
	var assets []Asset
	if err := json.Unmarshal(body, &assets); err != nil {
		return nil, err
	}
	return assets, nil
}
