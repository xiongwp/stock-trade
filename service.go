package main

import (
	"database/sql"
	"fmt"
	"log"
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
	for _, sig := range evaluate(symbol, bars, price) {
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

// Run 启动后台轮询：每 interval 刷新报价+策略，每天补拉一次历史 K 线。
func (s *Service) Run(interval time.Duration) {
	// 启动后先刷一次。
	if err := s.RefreshAll(); err != nil {
		log.Printf("首次刷新失败: %v", err)
	}
	quoteTick := time.NewTicker(interval)
	barTick := time.NewTicker(12 * time.Hour)
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

func mustList(db *sql.DB) []Symbol {
	l, _ := listSymbols(db)
	return l
}

func mustNames(db *sql.DB) []string {
	n, _ := listSymbolNames(db)
	return n
}
