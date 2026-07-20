package main

import (
	"database/sql"
	"fmt"
	"log"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

// Service 聚合数据库、Alpaca 客户端与内存缓存，是业务逻辑的核心。
type Service struct {
	db *sql.DB
	ap *Alpaca

	mu       sync.Mutex
	assets   []Asset   // 全部可交易资产的内存缓存（用于检索）
	assetsAt time.Time // 缓存时间

	barCache map[string]barCacheEntry // 按 symbol|tf 缓存分钟/小时/周线（日线走数据库）

	statMu    sync.Mutex     // 策略聚合排行缓存（与个股无关的重计算部分）
	statCache []StrategyStat // 已按胜率排序的聚合结果
	statAt    time.Time
}

type barCacheEntry struct {
	bars []BarRow
	at   time.Time
}

func newService(db *sql.DB, ap *Alpaca) *Service {
	return &Service{db: db, ap: ap, barCache: map[string]barCacheEntry{}}
}

// 支持的 K 线周期 → Alpaca timeframe 及回溯窗口。
var timeframes = map[string]struct {
	alpaca   string
	lookback time.Duration
}{
	"1Min":  {"1Min", 5 * 24 * time.Hour},
	"5Min":  {"5Min", 20 * 24 * time.Hour},
	"15Min": {"15Min", 40 * 24 * time.Hour},
	"30Min": {"30Min", 60 * 24 * time.Hour},
	"1Hour": {"1Hour", 120 * 24 * time.Hour},
	"1Day":  {"1Day", 365 * 24 * time.Hour},
	"1Week": {"1Week", 3 * 365 * 24 * time.Hour},
}

// Bars 返回某只股票指定周期的 K 线：日线走本地库，其它周期实时拉取并缓存 60 秒。
// extended 仅对分钟图生效：true 时叠加夜盘/盘前/盘后，拼出 24 小时全时段。
func (s *Service) Bars(symbol, tf string, extended bool) ([]BarRow, error) {
	if _, ok := timeframes[tf]; !ok {
		tf = "1Day"
	}
	if tf == "1Day" {
		bars, err := getBars(s.db, symbol)
		if err != nil {
			return nil, err
		}
		if len(bars) == 0 {
			if err := s.ensureBars(symbol); err == nil {
				bars, _ = getBars(s.db, symbol)
			}
		}
		return bars, nil
	}

	// 延长时段与常规时段的分钟数据不同，缓存需分开。
	key := symbol + "|" + tf
	if extended {
		key += "|ext"
	}
	s.mu.Lock()
	if e, ok := s.barCache[key]; ok && time.Since(e.at) < 60*time.Second {
		s.mu.Unlock()
		return e.bars, nil
	}
	s.mu.Unlock()

	spec := timeframes[tf]
	end := time.Now()
	start := end.Add(-spec.lookback)
	raw, err := s.ap.GetBars(symbol, spec.alpaca, start, end, extended)
	if err != nil {
		return nil, err
	}
	bars := make([]BarRow, len(raw))
	for i, b := range raw {
		bars[i] = BarRow{Time: b.T.Unix(), O: b.O, H: b.H, L: b.L, C: b.C, V: b.V}
	}
	s.mu.Lock()
	s.barCache[key] = barCacheEntry{bars: bars, at: time.Now()}
	s.mu.Unlock()
	return bars, nil
}

// SignalPoint 是策略在某时间点的买/卖信号。
type SignalPoint struct {
	Time int64 `json:"time"` // unix 秒
	Side int   `json:"side"` // +1 买 / -1 卖
}

// Signals 用指定策略在指定周期的 K 线上算出所有买卖点，供图表标注。
// extended 与图表保持一致，确保标记落在同一批蜡烛上。
func (s *Service) Signals(symbol, stratKey, tf string, extended bool) ([]SignalPoint, error) {
	var strat Strategy
	found := false
	for _, st := range strategies() {
		if st.Key == stratKey {
			strat, found = st, true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("未知策略: %s", stratKey)
	}
	bars, err := s.Bars(symbol, tf, extended)
	if err != nil {
		return nil, err
	}
	sig := strat.Signals(bars)
	out := []SignalPoint{}
	for i := range bars {
		if sig[i] != 0 {
			out = append(out, SignalPoint{Time: bars[i].Time, Side: sig[i]})
		}
	}
	return out, nil
}

// assetsCache 返回可交易资产列表，缓存 12 小时。
func (s *Service) assetsCache() ([]Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.assets != nil && time.Since(s.assetsAt) < 12*time.Hour {
		return s.assets, nil
	}
	assets, err := s.ap.ListAssets()
	if err != nil {
		return nil, err
	}
	s.assets = assets
	s.assetsAt = time.Now()
	return assets, nil
}

// searchAssets 按代码/名称模糊检索，优先代码前缀匹配。
func (s *Service) searchAssets(query string, limit int) ([]Asset, error) {
	assets, err := s.assetsCache()
	if err != nil {
		return nil, err
	}
	q := strings.ToUpper(strings.TrimSpace(query))
	if q == "" {
		return nil, nil
	}
	var prefix, contains []Asset
	for _, a := range assets {
		if !a.Tradable {
			continue
		}
		sym := strings.ToUpper(a.Symbol)
		name := strings.ToUpper(a.Name)
		switch {
		case strings.HasPrefix(sym, q):
			prefix = append(prefix, a)
		case strings.Contains(sym, q) || strings.Contains(name, q):
			contains = append(contains, a)
		}
		if len(prefix) >= limit {
			break
		}
	}
	out := append(prefix, contains...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Service) resolveName(symbol string) string {
	assets, err := s.assetsCache()
	if err != nil {
		return ""
	}
	for _, a := range assets {
		if strings.EqualFold(a.Symbol, symbol) {
			return a.Name
		}
	}
	return ""
}

// AddSymbol 加入自选：校验代码、写库、拉取近 1 年日线、刷新一次报价。
func (s *Service) AddSymbol(symbol string) (Symbol, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return Symbol{}, fmt.Errorf("代码不能为空")
	}
	name := s.resolveName(symbol)
	if err := addSymbol(s.db, symbol, name); err != nil {
		return Symbol{}, err
	}
	if err := s.ensureBars(symbol); err != nil {
		// 拉取失败且本地无该股票 K 线：回滚，避免留下空壳条目。
		if bars, _ := getBars(s.db, symbol); len(bars) == 0 {
			_ = deleteSymbol(s.db, symbol)
			return Symbol{}, fmt.Errorf("无法获取 %s 的行情，请检查代码或密钥: %w", symbol, err)
		}
		log.Printf("拉取 %s 历史 K 线失败: %v", symbol, err)
	}
	s.refreshOne(symbol)

	for _, sym := range mustList(s.db) {
		if sym.Symbol == symbol {
			return sym, nil
		}
	}
	return Symbol{Symbol: symbol, Name: name}, nil
}

// ensureBars 确保本地已有近 1 年日线；缺失或过期（>4 天）时补拉。
func (s *Service) ensureBars(symbol string) error {
	existing, err := getBars(s.db, symbol)
	if err != nil {
		return err
	}
	fresh := len(existing) > 0 &&
		time.Since(time.Unix(existing[len(existing)-1].Time, 0)) < 4*24*time.Hour
	if fresh {
		return nil
	}
	end := time.Now()
	start := end.AddDate(-1, 0, 0)
	bars, err := s.ap.GetBars(symbol, "1Day", start, end, false)
	if err != nil {
		return err
	}
	return upsertBars(s.db, symbol, bars)
}

// refreshOne 刷新单只股票的最新报价并评估策略。
func (s *Service) refreshOne(symbol string) {
	snaps, err := s.ap.GetSnapshots([]string{symbol})
	if err != nil {
		log.Printf("刷新 %s 报价失败: %v", symbol, err)
		return
	}
	s.applySnapshots(snaps)
}

// RefreshAll 批量刷新所有自选股报价，并运行策略生成提醒。
func (s *Service) RefreshAll() error {
	symbols, err := listSymbolNames(s.db)
	if err != nil {
		return err
	}
	if len(symbols) == 0 {
		return nil
	}
	snaps, err := s.ap.GetSnapshots(symbols)
	if err != nil {
		return err
	}
	s.applySnapshots(snaps)
	return nil
}

// applySnapshots 落库最新报价并对每只股票评估策略。
func (s *Service) applySnapshots(snaps map[string]Snapshot) {
	for sym, snap := range snaps {
		price := snap.Price
		prev := snap.PrevClose
		if prev == 0 {
			prev = latestClose(s.db, sym) // 兜底：用本地最近收盘
		}
		if price == 0 {
			continue
		}
		if err := updateQuote(s.db, sym, price, prev); err != nil {
			log.Printf("更新 %s 报价失败: %v", sym, err)
			continue
		}
		s.runStrategy(sym, price)
	}
}

// runStrategy 对单只股票评估策略并写入提醒（去重）。
func (s *Service) runStrategy(symbol string, price float64) {
	bars, err := getBars(s.db, symbol)
	if err != nil {
		return
	}
	for _, sig := range consensusSignals(symbol, bars, price) {
		inserted, err := insertAlert(s.db, Alert{
			Symbol:  symbol,
			Kind:    sig.Kind,
			Message: sig.Message,
			Price:   sig.Price,
		}, sig.DedupKey)
		if err != nil {
			log.Printf("写入 %s 提醒失败: %v", symbol, err)
		} else if inserted {
			log.Printf("提醒: %s", sig.Message)
		}
	}
}

// RunForever 启动后台轮询，并在 panic 后自动重启循环，保证永不退出。
func (s *Service) RunForever(interval time.Duration) {
	for {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("[recover] 后台轮询 panic: %v\n%s，2 秒后重启", rec, debug.Stack())
					time.Sleep(2 * time.Second)
				}
			}()
			s.run(interval)
		}()
	}
}

// run 是轮询主循环：每 interval 刷新报价+策略，每天补拉一次历史 K 线。
// 单次网络错误只记录不中断，保证「永不死寂」。
func (s *Service) run(interval time.Duration) {
	// 启动即补拉历史 K 线（后台进行，不阻塞报价刷新）。
	go func() {
		for _, sym := range mustNames(s.db) {
			if err := s.ensureBars(sym); err != nil {
				log.Printf("启动补拉 %s K 线失败: %v", sym, err)
			}
		}
	}()
	if err := s.RefreshAll(); err != nil {
		log.Printf("首次刷新失败: %v", err)
	}
	quoteTick := time.NewTicker(interval)
	barTick := time.NewTicker(12 * time.Hour)
	defer quoteTick.Stop()
	defer barTick.Stop()
	for {
		select {
		case <-quoteTick.C:
			if err := s.RefreshAll(); err != nil {
				log.Printf("定时刷新失败: %v", err)
			}
		case <-barTick.C:
			for _, sym := range mustNames(s.db) {
				if err := s.ensureBars(sym); err != nil {
					log.Printf("补拉 %s K 线失败: %v", sym, err)
				}
			}
		}
	}
}

// StrategyStat 是单个策略的回测排名 + 对某只股票的当前信号。
type StrategyStat struct {
	Key           string  `json:"key"`
	Name          string  `json:"name"`
	Desc          string  `json:"desc"`
	Trades        int     `json:"trades"`        // 全部自选股聚合的回测交易数
	WinRate       float64 `json:"winRate"`       // 聚合胜率 %
	AvgReturn     float64 `json:"avgReturn"`     // 每笔平均收益 %
	TotalReturn   float64 `json:"totalReturn"`   // 平均累计收益 %
	CurrentSignal int     `json:"currentSignal"` // 对所选股票：+1 买 / -1 卖 / 0 持有
	SymbolWinRate float64 `json:"symbolWinRate"` // 在所选股票上的胜率 %
	SymbolTrades  int     `json:"symbolTrades"`
}

// strategyAggregate 计算「全自选股聚合」的策略排行（与所选股票无关的重计算部分），缓存 60 秒。
// 50 个策略 × 上百只股票的回测较重，缓存后 /api/strategies 频繁调用也很轻。
func (s *Service) strategyAggregate() []StrategyStat {
	s.statMu.Lock()
	defer s.statMu.Unlock()
	if s.statCache != nil && time.Since(s.statAt) < 60*time.Second {
		return s.statCache
	}

	names, _ := listSymbolNames(s.db)
	barsBy := map[string][]BarRow{}
	for _, n := range names {
		if b, err := getBars(s.db, n); err == nil && len(b) >= 61 {
			barsBy[n] = b
		}
	}

	var stats []StrategyStat
	for _, strat := range strategies() {
		st := StrategyStat{Key: strat.Key, Name: strat.Name, Desc: strat.Desc}
		var totWins, totTrades, symCount int
		var wRetSum, totalRetSum float64
		for _, bars := range barsBy {
			bt := backtest(bars, strat.Signals(bars))
			totWins += bt.Wins
			totTrades += bt.Trades
			wRetSum += bt.AvgReturn * float64(bt.Trades)
			totalRetSum += bt.TotalReturn
			symCount++
		}
		st.Trades = totTrades
		if totTrades > 0 {
			st.WinRate = float64(totWins) / float64(totTrades) * 100
			st.AvgReturn = wRetSum / float64(totTrades)
		}
		if symCount > 0 {
			st.TotalReturn = totalRetSum / float64(symCount)
		}
		stats = append(stats, st)
	}
	sort.SliceStable(stats, func(i, j int) bool {
		if (stats[i].Trades == 0) != (stats[j].Trades == 0) {
			return stats[j].Trades == 0
		}
		return stats[i].WinRate > stats[j].WinRate
	})

	s.statCache = stats
	s.statAt = time.Now()
	return stats
}

// StrategyStats 返回胜率排行（缓存的聚合），并为 focusSymbol 填入当前信号与该股胜率（实时、廉价）。
func (s *Service) StrategyStats(focusSymbol string) ([]StrategyStat, error) {
	agg := s.strategyAggregate()
	out := make([]StrategyStat, len(agg))
	copy(out, agg)

	if focusSymbol != "" {
		bars, _ := getBars(s.db, focusSymbol)
		if len(bars) >= 61 {
			var price float64
			for _, sy := range mustList(s.db) {
				if sy.Symbol == focusSymbol {
					price = sy.LastPrice
					break
				}
			}
			byKey := map[string]Strategy{}
			for _, st := range strategies() {
				byKey[st.Key] = st
			}
			for i := range out {
				strat, ok := byKey[out[i].Key]
				if !ok {
					continue
				}
				out[i].CurrentSignal = currentSignal(strat, bars, price)
				bt := backtest(bars, strat.Signals(bars))
				out[i].SymbolWinRate = bt.WinRate
				out[i].SymbolTrades = bt.Trades
			}
		}
	}
	return out, nil
}

func mustList(db *sql.DB) []Symbol {
	l, _ := listSymbols(db)
	return l
}

func mustNames(db *sql.DB) []string {
	n, _ := listSymbolNames(db)
	return n
}
