package main

import (
	"database/sql"
	"encoding/json"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
)

type server struct {
	db *sql.DB
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// /api/stocks —— GET 列表, POST 新增
func (s *server) handleStocks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		stocks, err := listStocks(s.db)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, stocks)

	case http.MethodPost:
		var req struct {
			Symbol string  `json:"symbol"`
			Name   string  `json:"name"`
			Price  float64 `json:"price"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "无效的请求体")
			return
		}
		req.Symbol = strings.ToUpper(strings.TrimSpace(req.Symbol))
		if req.Symbol == "" {
			writeErr(w, http.StatusBadRequest, "代码不能为空")
			return
		}
		if req.Price < 0 {
			writeErr(w, http.StatusBadRequest, "价格不能为负")
			return
		}
		stock, err := addStock(s.db, req.Symbol, strings.TrimSpace(req.Name), req.Price)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				writeErr(w, http.StatusConflict, "该代码已在自选中")
				return
			}
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, stock)

	default:
		writeErr(w, http.StatusMethodNotAllowed, "不支持的方法")
	}
}

// /api/stocks/{id} —— PATCH 改价, DELETE 删除
func (s *server) handleStock(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/stocks/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效的 id")
		return
	}

	switch r.Method {
	case http.MethodPatch:
		var req struct {
			Price float64 `json:"price"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "无效的请求体")
			return
		}
		if req.Price < 0 {
			writeErr(w, http.StatusBadRequest, "价格不能为负")
			return
		}
		if err := updatePrice(s.db, id, req.Price); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	case http.MethodDelete:
		if err := deleteStock(s.db, id); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		writeErr(w, http.StatusMethodNotAllowed, "不支持的方法")
	}
}

// /api/tick —— 模拟行情：对所有自选股做一次随机游走。
func (s *server) handleTick(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "不支持的方法")
		return
	}
	stocks, err := listStocks(s.db)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, st := range stocks {
		if st.Price <= 0 {
			continue
		}
		// ±2% 以内的随机波动
		delta := (rand.Float64()*2 - 1) * 0.02 * st.Price
		newPrice := st.Price + delta
		if newPrice < 0.01 {
			newPrice = 0.01
		}
		// 保留两位小数
		newPrice = float64(int(newPrice*100+0.5)) / 100
		if err := updatePrice(s.db, st.ID, newPrice); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	stocks, _ = listStocks(s.db)
	writeJSON(w, http.StatusOK, stocks)
}
