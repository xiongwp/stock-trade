package main

import (
	"fmt"
	"time"
)

// Signal 是策略产生的一条交易信号。
type Signal struct {
	Kind     string  // golden_cross / death_cross / overbought / oversold
	Message  string  // 中文提示
	Price    float64 // 触发时价格
	DedupKey string  // 去重键，避免同一信号重复提醒
}

// sma 返回 values 末尾 period 个值的简单均线；不足则返回 (0,false)。
func sma(values []float64, period int) (float64, bool) {
	if len(values) < period || period <= 0 {
		return 0, false
	}
	sum := 0.0
	for _, v := range values[len(values)-period:] {
		sum += v
	}
	return sum / float64(period), true
}

// rsi 返回末尾 period 周期的 RSI（0-100）；不足则返回 (0,false)。
func rsi(closes []float64, period int) (float64, bool) {
	if len(closes) < period+1 {
		return 0, false
	}
	var gain, loss float64
	for i := len(closes) - period; i < len(closes); i++ {
		diff := closes[i] - closes[i-1]
		if diff >= 0 {
			gain += diff
		} else {
			loss -= diff
		}
	}
	if loss == 0 {
		return 100, true
	}
	rs := (gain / float64(period)) / (loss / float64(period))
	return 100 - 100/(1+rs), true
}

// evaluate 基于日线序列 + 最新价，评估策略并返回触发的信号。
// shortP/longP 为均线周期（如 5 / 20），rsiP 为 RSI 周期（如 14）。
func evaluate(symbol string, bars []BarRow, lastPrice float64) []Signal {
	const shortP, longP, rsiP = 5, 20, 14
	if len(bars) < longP+1 {
		return nil
	}

	closes := make([]float64, len(bars))
	for i, b := range bars {
		closes[i] = b.C
	}
	// 用实时价替换最后一根的收盘，让信号更贴近当前。
	if lastPrice > 0 {
		closes[len(closes)-1] = lastPrice
	}

	// 最近一根 K 线的交易日，作为去重键的一部分（同一天同类信号只提醒一次）。
	day := time.Unix(bars[len(bars)-1].Time, 0).UTC().Format("2006-01-02")
	price := closes[len(closes)-1]

	var signals []Signal

	// 均线金叉/死叉：比较当前与前一日的短/长均线相对位置。
	shortNow, ok1 := sma(closes, shortP)
	longNow, ok2 := sma(closes, longP)
	shortPrev, ok3 := sma(closes[:len(closes)-1], shortP)
	longPrev, ok4 := sma(closes[:len(closes)-1], longP)
	if ok1 && ok2 && ok3 && ok4 {
		if shortPrev <= longPrev && shortNow > longNow {
			signals = append(signals, Signal{
				Kind:     "golden_cross",
				Message:  fmt.Sprintf("%s 金叉：MA%d 上穿 MA%d，考虑买入", symbol, shortP, longP),
				Price:    price,
				DedupKey: fmt.Sprintf("%s|golden_cross|%s", symbol, day),
			})
		} else if shortPrev >= longPrev && shortNow < longNow {
			signals = append(signals, Signal{
				Kind:     "death_cross",
				Message:  fmt.Sprintf("%s 死叉：MA%d 下穿 MA%d，考虑卖出", symbol, shortP, longP),
				Price:    price,
				DedupKey: fmt.Sprintf("%s|death_cross|%s", symbol, day),
			})
		}
	}

	// RSI 超买/超卖。
	if r, ok := rsi(closes, rsiP); ok {
		if r >= 70 {
			signals = append(signals, Signal{
				Kind:     "overbought",
				Message:  fmt.Sprintf("%s 超买：RSI%d=%.1f，注意回调风险", symbol, rsiP, r),
				Price:    price,
				DedupKey: fmt.Sprintf("%s|overbought|%s", symbol, day),
			})
		} else if r <= 30 {
			signals = append(signals, Signal{
				Kind:     "oversold",
				Message:  fmt.Sprintf("%s 超卖：RSI%d=%.1f，可能超跌反弹", symbol, rsiP, r),
				Price:    price,
				DedupKey: fmt.Sprintf("%s|oversold|%s", symbol, day),
			})
		}
	}

	return signals
}
