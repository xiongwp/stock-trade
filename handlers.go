package main

import (
	"encoding/json"
	"net/http"
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

// GET /api/bars/{symbol}
func (h *api) bars(w http.ResponseWriter, r *http.Request) {
	sym := strings.ToUpper(strings.TrimPrefix(r.URL.Path, "/api/bars/"))
	if sym == "" {
		writeErr(w, http.StatusBadRequest, "缺少代码")
		return
	}
	bars, err := getBars(h.svc.db, sym)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bars)
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

// DELETE /api/positions/{id}
func (h *api) position(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/positions/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效的 id")
		return
	}
	if r.Method != http.MethodDelete {
		writeErr(w, http.StatusMethodNotAllowed, "不支持的方法")
		return
	}
	if err := deletePosition(h.svc.db, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// PnLRow 是按股票聚合后的盈亏。
type PnLRow struct {
	Symbol        string  `json:"symbol"`
	Quantity      float64 `json:"quantity"`
	AvgCost       float64 `json:"avgCost"`
	LastPrice     float64 `json:"lastPrice"`
	CostBasis     float64 `json:"costBasis"`
	MarketValue   float64 `json:"marketValue"`
	UnrealizedUSD float64 `json:"unrealizedUsd"`
	UnrealizedPct float64 `json:"unrealizedPct"`
}

// GET /api/pnl —— 按股票聚合持仓，用最新价算美金盈亏。
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
		row.Quantity += p.Quantity
		row.CostBasis += p.Quantity * p.BuyPrice
	}

	rows := []PnLRow{}
	var totalCost, totalMV float64
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
		},
	})
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

// POST /api/refresh —— 立即刷新报价与策略。
func (h *api) refresh(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RefreshAll(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
