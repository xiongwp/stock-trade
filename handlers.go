package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type api struct {
	svc *Service
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// GET /api/symbols | POST /api/symbols
func (h *api) symbols(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := listSymbols(h.svc.db)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var req struct {
			Symbol string `json:"symbol"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "无效的请求体")
			return
		}
		sym, err := h.svc.AddSymbol(req.Symbol)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, sym)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "不支持的方法")
	}
}

// DELETE /api/symbols/{symbol}
func (h *api) symbol(w http.ResponseWriter, r *http.Request) {
	sym := strings.ToUpper(strings.TrimPrefix(r.URL.Path, "/api/symbols/"))
	if sym == "" {
		writeErr(w, http.StatusBadRequest, "缺少代码")
		return
	}
	if r.Method != http.MethodDelete {
		writeErr(w, http.StatusMethodNotAllowed, "不支持的方法")
		return
	}
	if err := deleteSymbol(h.svc.db, sym); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GET /api/search?q=
func (h *api) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	assets, err := h.svc.searchAssets(q, 30)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if assets == nil {
		assets = []Asset{}
	}
	writeJSON(w, http.StatusOK, assets)
}

// GET /api/bars/{symbol}?tf=1Day —— 支持 1Min/5Min/15Min/30Min/1Hour/1Day/1Week
func (h *api) bars(w http.ResponseWriter, r *http.Request) {
	sym := strings.ToUpper(strings.TrimPrefix(r.URL.Path, "/api/bars/"))
	if sym == "" {
		writeErr(w, http.StatusBadRequest, "缺少代码")
		return
	}
	tf := r.URL.Query().Get("tf")
	if tf == "" {
		tf = "1Day"
	}
	bars, err := h.svc.Bars(sym, tf)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bars)
}

// GET /api/signals/{symbol}?strategy=ma_5_20&tf=1Day —— 策略买卖点
func (h *api) signals(w http.ResponseWriter, r *http.Request) {
	sym := strings.ToUpper(strings.TrimPrefix(r.URL.Path, "/api/signals/"))
	if sym == "" {
		writeErr(w, http.StatusBadRequest, "缺少代码")
		return
	}
	strat := r.URL.Query().Get("strategy")
	tf := r.URL.Query().Get("tf")
	if tf == "" {
		tf = "1Day"
	}
	points, err := h.svc.Signals(sym, strat, tf)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, points)
}

// GET /api/positions | POST /api/positions
func (h *api) positions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := listPositions(h.svc.db)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var p Position
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeErr(w, http.StatusBadRequest, "无效的请求体")
			return
		}
		p.Symbol = strings.ToUpper(strings.TrimSpace(p.Symbol))
		if p.Symbol == "" || p.Quantity <= 0 || p.BuyPrice < 0 {
			writeErr(w, http.StatusBadRequest, "代码/数量/买入价不合法")
			return
		}
		if p.BuyTime == "" {
			p.BuyTime = time.Now().Format("2006-01-02")
		}
		id, err := addPosition(h.svc.db, p)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		p.ID = id
		writeJSON(w, http.StatusCreated, p)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "不支持的方法")
	}
}

// DELETE /api/positions/{id}  ·  POST /api/positions/{id}/sell
func (h *api) position(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/positions/")
	isSell := strings.HasSuffix(rest, "/sell")
	idStr := strings.TrimSuffix(rest, "/sell")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效的 id")
		return
	}

	if isSell {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "不支持的方法")
			return
		}
		var req struct {
			SellPrice float64 `json:"sellPrice"`
			SellTime  string  `json:"sellTime"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "无效的请求体")
			return
		}
		// sellTime 为空表示撤销卖出（改回持仓）；非空则必须校验价格。
		if req.SellTime != "" && req.SellPrice < 0 {
			writeErr(w, http.StatusBadRequest, "卖出价不合法")
			return
		}
		if req.SellTime == "" {
			req.SellPrice = 0
		}
		if err := sellPosition(h.svc.db, id, req.SellPrice, req.SellTime); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	switch r.Method {
	case http.MethodPatch:
		var p Position
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeErr(w, http.StatusBadRequest, "无效的请求体")
			return
		}
		p.ID = id
		p.Symbol = strings.ToUpper(strings.TrimSpace(p.Symbol))
		if p.Symbol == "" || p.Quantity <= 0 || p.BuyPrice < 0 {
			writeErr(w, http.StatusBadRequest, "代码/数量/买入价不合法")
			return
		}
		if p.SellTime == "" { // 未卖出则清空卖出价
			p.SellPrice = 0
		}
		if err := updatePosition(h.svc.db, p); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case http.MethodDelete:
		if err := deletePosition(h.svc.db, id); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "不支持的方法")
	}
}

// PnLRow 是按股票聚合后的盈亏（持仓中用最新价算浮动盈亏，已卖出算已实现盈亏）。
type PnLRow struct {
	Symbol        string  `json:"symbol"`
	Quantity      float64 `json:"quantity"`      // 当前持仓数量（未卖出）
	AvgCost       float64 `json:"avgCost"`       // 持仓均价
	LastPrice     float64 `json:"lastPrice"`
	CostBasis     float64 `json:"costBasis"`     // 持仓成本
	MarketValue   float64 `json:"marketValue"`   // 持仓市值
	UnrealizedUSD float64 `json:"unrealizedUsd"` // 浮动盈亏
	UnrealizedPct float64 `json:"unrealizedPct"`
	RealizedUSD   float64 `json:"realizedUsd"`   // 已实现盈亏（该股票所有已卖出笔）
}

// GET /api/pnl —— 按股票聚合：持仓算浮动盈亏，已卖出算已实现盈亏。
func (h *api) pnl(w http.ResponseWriter, r *http.Request) {
	positions, err := listPositions(h.svc.db)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	syms, err := listSymbols(h.svc.db)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	priceOf := map[string]float64{}
	for _, s := range syms {
		priceOf[s.Symbol] = s.LastPrice
	}

	agg := map[string]*PnLRow{}
	order := []string{}
	for _, p := range positions {
		row, ok := agg[p.Symbol]
		if !ok {
			row = &PnLRow{Symbol: p.Symbol, LastPrice: priceOf[p.Symbol]}
			agg[p.Symbol] = row
			order = append(order, p.Symbol)
		}
		if p.Closed() {
			row.RealizedUSD += p.RealizedPnL()
		} else {
			row.Quantity += p.Quantity
			row.CostBasis += p.Quantity * p.BuyPrice
		}
	}

	rows := []PnLRow{}
	var totalCost, totalMV, totalRealized float64
	for _, sym := range order {
		row := agg[sym]
		if row.Quantity > 0 {
			row.AvgCost = row.CostBasis / row.Quantity
		}
		row.MarketValue = row.Quantity * row.LastPrice
		row.UnrealizedUSD = row.MarketValue - row.CostBasis
		if row.CostBasis > 0 {
			row.UnrealizedPct = row.UnrealizedUSD / row.CostBasis * 100
		}
		totalCost += row.CostBasis
		totalMV += row.MarketValue
		totalRealized += row.RealizedUSD
		rows = append(rows, *row)
	}

	totalPnL := totalMV - totalCost
	totalPct := 0.0
	if totalCost > 0 {
		totalPct = totalPnL / totalCost * 100
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rows": rows,
		"totals": map[string]float64{
			"costBasis":     totalCost,
			"marketValue":   totalMV,
			"unrealizedUsd": totalPnL,
			"unrealizedPct": totalPct,
			"realizedUsd":   totalRealized,
		},
	})
}

// PeriodStat 是某个周期（周/月）的已实现盈亏统计。
type PeriodStat struct {
	Period      string  `json:"period"`      // 周: 2026-W29；月: 2026-07
	Label       string  `json:"label"`       // 展示用中文标签
	RealizedUSD float64 `json:"realizedUsd"` // 该周期已实现盈亏（按卖出时间归属）
	Trades      int     `json:"trades"`      // 该周期卖出笔数
	Wins        int     `json:"wins"`        // 其中盈利笔数
	WinRate     float64 `json:"winRate"`
}

// parseDay 宽松解析日期/时间字符串。
func parseDay(s string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// GET /api/stats —— 已实现盈亏按周 / 按月统计。
func (h *api) stats(w http.ResponseWriter, r *http.Request) {
	positions, err := listPositions(h.svc.db)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	weekly := map[string]*PeriodStat{}
	monthly := map[string]*PeriodStat{}
	for _, p := range positions {
		if !p.Closed() {
			continue
		}
		t, ok := parseDay(p.SellTime)
		if !ok {
			continue
		}
		pnl := p.RealizedPnL()

		iy, iw := t.ISOWeek()
		wk := fmt.Sprintf("%d-W%02d", iy, iw)
		accumulate(weekly, wk, fmt.Sprintf("%d 年第 %d 周", iy, iw), pnl)

		mk := t.Format("2006-01")
		accumulate(monthly, mk, fmt.Sprintf("%d 年 %d 月", t.Year(), int(t.Month())), pnl)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"weekly":  sortedStats(weekly),
		"monthly": sortedStats(monthly),
	})
}

func accumulate(m map[string]*PeriodStat, key, label string, pnl float64) {
	s, ok := m[key]
	if !ok {
		s = &PeriodStat{Period: key, Label: label}
		m[key] = s
	}
	s.RealizedUSD += pnl
	s.Trades++
	if pnl > 0 {
		s.Wins++
	}
}

func sortedStats(m map[string]*PeriodStat) []PeriodStat {
	out := make([]PeriodStat, 0, len(m))
	for _, s := range m {
		if s.Trades > 0 {
			s.WinRate = float64(s.Wins) / float64(s.Trades) * 100
		}
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Period > out[j].Period }) // 最近在前
	return out
}

// GET /api/alerts
func (h *api) alerts(w http.ResponseWriter, r *http.Request) {
	list, err := listAlerts(h.svc.db, 100)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /api/alerts/{id}/ack
func (h *api) alertAck(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/alerts/")
	idStr := strings.TrimSuffix(rest, "/ack")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效的 id")
		return
	}
	if err := ackAlert(h.svc.db, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GET /api/strategies?symbol=AAPL —— 策略胜率排行 + 对该股票的当前信号。
func (h *api) strategies(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("symbol")))
	stats, err := h.svc.StrategyStats(symbol)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// POST /api/refresh —— 立即刷新报价与策略。
func (h *api) refresh(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RefreshAll(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
