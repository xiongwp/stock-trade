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
}

func newService(db *sql.DB, ap *Alpaca) *Service {
	return &Service{db: db, ap: ap}
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
	bars, err := s.ap.GetBars(symbol, "1Day", start, end)
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

// StrategyStats 对全部自选股回测每个策略并按胜率排名，
// 同时给出对 focusSymbol 的当前信号与该股票上的胜率。
func (s *Service) StrategyStats(focusSymbol string) ([]StrategyStat, error) {
	names, err := listSymbolNames(s.db)
	if err != nil {
		return nil, err
	}
	// 预取各股票的 K 线与最新价。
	barsBy := map[string][]BarRow{}
	priceBy := map[string]float64{}
	syms, _ := listSymbols(s.db)
	for _, sy := range syms {
		priceBy[sy.Symbol] = sy.LastPrice
	}
	for _, n := range names {
		if b, err := getBars(s.db, n); err == nil {
			barsBy[n] = b
		}
	}

	var stats []StrategyStat
	for _, strat := range strategies() {
		st := StrategyStat{Key: strat.Key, Name: strat.Name, Desc: strat.Desc}
		var totWins, totTrades int
		var wRetSum, totalRetSum float64
		var symCount int
		for _, n := range names {
			bars := barsBy[n]
			if len(bars) < 61 {
				continue
			}
			bt := backtest(bars, strat.Signals(bars))
			totWins += bt.Wins
			totTrades += bt.Trades
			wRetSum += bt.AvgReturn * float64(bt.Trades)
			totalRetSum += bt.TotalReturn
			symCount++
			if n == focusSymbol {
				st.CurrentSignal = currentSignal(strat, bars, priceBy[n])
				st.SymbolWinRate = bt.WinRate
				st.SymbolTrades = bt.Trades
			}
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

	// 按胜率降序排名（无交易的沉底）。
	sort.SliceStable(stats, func(i, j int) bool {
		if (stats[i].Trades == 0) != (stats[j].Trades == 0) {
			return stats[j].Trades == 0
		}
		return stats[i].WinRate > stats[j].WinRate
	})
	return stats, nil
}

func mustList(db *sql.DB) []Symbol {
	l, _ := listSymbols(db)
	return l
}

func mustNames(db *sql.DB) []string {
	n, _ := listSymbolNames(db)
	return n
}
