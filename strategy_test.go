package main

import "testing"

// 构造一段收盘价序列，末端形成金叉：短均线由下方上穿长均线。
func TestGoldenCross(t *testing.T) {
	closes := []float64{}
	// 先下跌 20 天让短均线压在长均线下方。
	for i := 0; i < 20; i++ {
		closes = append(closes, 100-float64(i))
	}
	// 再急拉 6 天，制造短均线上穿。
	for i := 1; i <= 6; i++ {
		closes = append(closes, 80+float64(i)*6)
	}

	bars := make([]BarRow, len(closes))
	for i, c := range closes {
		bars[i] = BarRow{Time: int64(i) * 86400, O: c, H: c, L: c, C: c, V: 1000}
	}

	// 金叉只在发生的那一天触发；逐前缀评估，断言过程中出现过金叉。
	found := false
	for i := 21; i <= len(bars); i++ {
		for _, s := range evaluate("TEST", bars[:i], bars[i-1].C) {
			if s.Kind == "golden_cross" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("预期在上涨过程中检测到金叉，但未触发")
	}
}

func TestRSIOverbought(t *testing.T) {
	// 连续上涨 → RSI 应接近 100（超买）。
	closes := []float64{}
	for i := 0; i < 30; i++ {
		closes = append(closes, 100+float64(i)*2)
	}
	if r, ok := rsi(closes, 14); !ok || r < 70 {
		t.Fatalf("预期 RSI>=70，实际 r=%.2f ok=%v", r, ok)
	}
}

func TestSMA(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5}
	if m, ok := sma(vals, 5); !ok || m != 3 {
		t.Fatalf("SMA5 预期 3，实际 %.2f ok=%v", m, ok)
	}
	if _, ok := sma(vals, 6); ok {
		t.Fatalf("数据不足时应返回 false")
	}
}
