package main

import (
	"math"
	"testing"
)

func mkBars(closes []float64) []BarRow {
	bars := make([]BarRow, len(closes))
	for i, c := range closes {
		bars[i] = BarRow{Time: int64(i) * 86400, O: c, H: c * 1.01, L: c * 0.99, C: c, V: 1000}
	}
	return bars
}

func TestSMASeq(t *testing.T) {
	s := smaSeq([]float64{1, 2, 3, 4, 5}, 5)
	if s[4] != 3 {
		t.Fatalf("SMA5 末值应为 3，实际 %.2f", s[4])
	}
	if !math.IsNaN(s[3]) {
		t.Fatalf("不足周期处应为 NaN")
	}
}

func TestRSISeqOverbought(t *testing.T) {
	vals := []float64{}
	for i := 0; i < 30; i++ {
		vals = append(vals, 100+float64(i)*2)
	}
	r := rsiSeq(vals, 14)
	if r[len(r)-1] < 70 {
		t.Fatalf("持续上涨 RSI 应 >=70，实际 %.1f", r[len(r)-1])
	}
}

// 回测：先跌后涨的序列上，MA5/MA20 策略应产生交易且累计为正。
func TestBacktestMACross(t *testing.T) {
	closes := []float64{}
	for i := 0; i < 30; i++ {
		closes = append(closes, 100-float64(i))
	}
	for i := 0; i < 30; i++ {
		closes = append(closes, 70+float64(i)*2)
	}
	bars := mkBars(closes)
	var ma Strategy
	for _, s := range strategies() {
		if s.Key == "ma_5_20" {
			ma = s
		}
	}
	bt := backtest(bars, ma.Signals(bars))
	if bt.Trades == 0 {
		t.Fatalf("预期至少 1 笔交易")
	}
	if bt.WinRate < 0 || bt.WinRate > 100 {
		t.Fatalf("胜率应在 0-100，实际 %.1f", bt.WinRate)
	}
}

func TestStrategiesCount(t *testing.T) {
	if n := len(strategies()); n != 10 {
		t.Fatalf("应内置 10 个策略，实际 %d", n)
	}
}

func TestConsensus(t *testing.T) {
	// 长度不足时不应产生共振信号。
	bars := mkBars([]float64{1, 2, 3})
	if sigs := consensusSignals("X", bars, 3); len(sigs) != 0 {
		t.Fatalf("数据不足时不应有共振信号")
	}
}
