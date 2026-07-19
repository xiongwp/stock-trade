package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// GetBars 拉取某只股票在 [start, end] 区间内的日线（或指定周期）K 线，自动翻页。
func (a *Alpaca) GetBars(symbol, timeframe string, start, end time.Time) ([]Bar, error) {
	var all []Bar
	pageToken := ""
	for {
		q := url.Values{}
		q.Set("timeframe", timeframe)
		q.Set("start", start.Format(time.RFC3339))
		q.Set("end", end.Format(time.RFC3339))
		q.Set("limit", "10000")
		q.Set("adjustment", "split")
		q.Set("feed", a.cfg.Feed)
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
	LatestTrade  *struct{ P float64 `json:"p"` } `json:"latestTrade"`
	DailyBar     *struct{ C float64 `json:"c"` } `json:"dailyBar"`
	PrevDailyBar *struct{ C float64 `json:"c"` } `json:"prevDailyBar"`
}

// GetSnapshots 批量获取多只股票的最新快照（按 100 个一批）。
func (a *Alpaca) GetSnapshots(symbols []string) (map[string]Snapshot, error) {
	result := make(map[string]Snapshot)
	for i := 0; i < len(symbols); i += 100 {
		end := i + 100
		if end > len(symbols) {
			end = len(symbols)
		}
		batch := symbols[i:end]
		q := url.Values{}
		q.Set("symbols", strings.Join(batch, ","))
		q.Set("feed", a.cfg.Feed)
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
			if s.LatestTrade != nil {
				snap.Price = s.LatestTrade.P
			} else if s.DailyBar != nil {
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
