package main

import (
	"fmt"
	"math"
	"time"
)

// ===================== 指标计算 =====================
// 所有指标返回与 bars 等长的序列，未定义处为 NaN。

func closes(bars []BarRow) []float64 {
	out := make([]float64, len(bars))
	for i, b := range bars {
		out[i] = b.C
	}
	return out
}

// smaSeq 计算简单均线，NaN 感知：窗口内含 NaN 处输出 NaN，不污染后续。
func smaSeq(vals []float64, n int) []float64 {
	out := make([]float64, len(vals))
	for i := range out {
		out[i] = math.NaN()
	}
	for i := n - 1; i < len(vals); i++ {
		sum, ok := 0.0, true
		for j := i - n + 1; j <= i; j++ {
			if math.IsNaN(vals[j]) {
				ok = false
				break
			}
			sum += vals[j]
		}
		if ok {
			out[i] = sum / float64(n)
		}
	}
	return out
}

// emaSeq 计算指数均线，NaN 感知：对每段连续非 NaN 区间分别用 SMA 播种后递推。
func emaSeq(vals []float64, n int) []float64 {
	out := make([]float64, len(vals))
	for i := range out {
		out[i] = math.NaN()
	}
	k := 2.0 / float64(n+1)
	i := 0
	for i < len(vals) {
		if math.IsNaN(vals[i]) {
			i++
			continue
		}
		// 找到从 i 起的连续非 NaN 区间 [i, j)。
		j := i
		for j < len(vals) && !math.IsNaN(vals[j]) {
			j++
		}
		if j-i >= n {
			seed := 0.0
			for x := i; x < i+n; x++ {
				seed += vals[x]
			}
			prev := seed / float64(n)
			out[i+n-1] = prev
			for x := i + n; x < j; x++ {
				prev = vals[x]*k + prev*(1-k)
				out[x] = prev
			}
		}
		i = j
	}
	return out
}

func rsiSeq(vals []float64, n int) []float64 {
	out := make([]float64, len(vals))
	for i := range out {
		out[i] = math.NaN()
	}
	if len(vals) <= n {
		return out
	}
	var gain, loss float64
	for i := 1; i <= n; i++ {
		d := vals[i] - vals[i-1]
		if d >= 0 {
			gain += d
		} else {
			loss -= d
		}
	}
	avgG, avgL := gain/float64(n), loss/float64(n)
	rsiAt := func(g, l float64) float64 {
		if l == 0 {
			return 100
		}
		return 100 - 100/(1+g/l)
	}
	out[n] = rsiAt(avgG, avgL)
	for i := n + 1; i < len(vals); i++ {
		d := vals[i] - vals[i-1]
		g, l := 0.0, 0.0
		if d >= 0 {
			g = d
		} else {
			l = -d
		}
		avgG = (avgG*float64(n-1) + g) / float64(n)
		avgL = (avgL*float64(n-1) + l) / float64(n)
		out[i] = rsiAt(avgG, avgL)
	}
	return out
}

func stddevSeq(vals []float64, n int) []float64 {
	out := make([]float64, len(vals))
	for i := range vals {
		if i < n-1 {
			out[i] = math.NaN()
			continue
		}
		mean := 0.0
		for j := i - n + 1; j <= i; j++ {
			mean += vals[j]
		}
		mean /= float64(n)
		v := 0.0
		for j := i - n + 1; j <= i; j++ {
			v += (vals[j] - mean) * (vals[j] - mean)
		}
		out[i] = math.Sqrt(v / float64(n))
	}
	return out
}

// crossUp 判断序列 a 在 i 处是否上穿 b。
func crossUp(a, b []float64, i int) bool {
	if i == 0 || math.IsNaN(a[i]) || math.IsNaN(b[i]) || math.IsNaN(a[i-1]) || math.IsNaN(b[i-1]) {
		return false
	}
	return a[i-1] <= b[i-1] && a[i] > b[i]
}
func crossDown(a, b []float64, i int) bool {
	if i == 0 || math.IsNaN(a[i]) || math.IsNaN(b[i]) || math.IsNaN(a[i-1]) || math.IsNaN(b[i-1]) {
		return false
	}
	return a[i-1] >= b[i-1] && a[i] < b[i]
}

func constSeq(vals []float64, c float64) []float64 {
	out := make([]float64, len(vals))
	for i := range out {
		out[i] = c
	}
	return out
}

// ===================== 策略库 =====================

// Strategy 是一个策略：给定 K 线序列，产出每根 K 线的信号（+1 买 / -1 卖 / 0 持有）。
type Strategy struct {
	Key     string
	Name    string
	Desc    string
	Signals func(bars []BarRow) []int
}

// crossStrategy 用「快线上穿慢线买、下穿卖」构造策略。
func crossStrategy(fast, slow func([]BarRow) []float64) func([]BarRow) []int {
	return func(bars []BarRow) []int {
		a, b := fast(bars), slow(bars)
		sig := make([]int, len(bars))
		for i := range bars {
			if crossUp(a, b, i) {
				sig[i] = 1
			} else if crossDown(a, b, i) {
				sig[i] = -1
			}
		}
		return sig
	}
}

func highs(bars []BarRow) []float64 {
	o := make([]float64, len(bars))
	for i, b := range bars {
		o[i] = b.H
	}
	return o
}
func lows(bars []BarRow) []float64 {
	o := make([]float64, len(bars))
	for i, b := range bars {
		o[i] = b.L
	}
	return o
}

// strategies 返回全部内置策略（10 个经典技术策略）。
func strategies() []Strategy {
	cl := func(b []BarRow) []float64 { return closes(b) }
	return []Strategy{
		{
			Key: "ma_5_20", Name: "均线 MA5/MA20", Desc: "短均线上穿长均线金叉买入，下穿死叉卖出",
			Signals: crossStrategy(
				func(b []BarRow) []float64 { return smaSeq(cl(b), 5) },
				func(b []BarRow) []float64 { return smaSeq(cl(b), 20) }),
		},
		{
			Key: "ma_20_60", Name: "均线 MA20/MA60", Desc: "中长均线金叉死叉，跟踪中期趋势",
			Signals: crossStrategy(
				func(b []BarRow) []float64 { return smaSeq(cl(b), 20) },
				func(b []BarRow) []float64 { return smaSeq(cl(b), 60) }),
		},
		{
			Key: "ema_12_26", Name: "EMA 12/26", Desc: "指数均线金叉死叉，比 SMA 更灵敏",
			Signals: crossStrategy(
				func(b []BarRow) []float64 { return emaSeq(cl(b), 12) },
				func(b []BarRow) []float64 { return emaSeq(cl(b), 26) }),
		},
		{
			Key: "macd", Name: "MACD(12,26,9)", Desc: "MACD 线上穿信号线买入，下穿卖出",
			Signals: func(b []BarRow) []int {
				c := cl(b)
				macd := make([]float64, len(c))
				fast, slow := emaSeq(c, 12), emaSeq(c, 26)
				for i := range c {
					macd[i] = fast[i] - slow[i]
				}
				signal := emaSeq(macd, 9)
				sig := make([]int, len(c))
				for i := range c {
					if crossUp(macd, signal, i) {
						sig[i] = 1
					} else if crossDown(macd, signal, i) {
						sig[i] = -1
					}
				}
				return sig
			},
		},
		{
			Key: "rsi_14", Name: "RSI(14) 超买超卖", Desc: "RSI 上穿 30 买入，下穿 70 卖出",
			Signals: func(b []BarRow) []int {
				r := rsiSeq(cl(b), 14)
				lo, hi := constSeq(r, 30), constSeq(r, 70)
				sig := make([]int, len(b))
				for i := range b {
					if crossUp(r, lo, i) {
						sig[i] = 1
					} else if crossDown(r, hi, i) {
						sig[i] = -1
					}
				}
				return sig
			},
		},
		{
			Key: "boll_20_2", Name: "布林带(20,2)", Desc: "触及下轨买入，触及上轨卖出（均值回归）",
			Signals: func(b []BarRow) []int {
				c := cl(b)
				mid := smaSeq(c, 20)
				sd := stddevSeq(c, 20)
				sig := make([]int, len(b))
				for i := range b {
					if math.IsNaN(mid[i]) || math.IsNaN(sd[i]) {
						continue
					}
					lower := mid[i] - 2*sd[i]
					upper := mid[i] + 2*sd[i]
					if c[i] <= lower {
						sig[i] = 1
					} else if c[i] >= upper {
						sig[i] = -1
					}
				}
				return sig
			},
		},
		{
			Key: "donchian_20", Name: "唐奇安通道(20)", Desc: "突破 20 日新高买入，跌破 20 日新低卖出",
			Signals: func(b []BarRow) []int {
				h, l := highs(b), lows(b)
				sig := make([]int, len(b))
				for i := 20; i < len(b); i++ {
					hh, ll := h[i-1], l[i-1]
					for j := i - 20; j < i; j++ {
						if h[j] > hh {
							hh = h[j]
						}
						if l[j] < ll {
							ll = l[j]
						}
					}
					if b[i].C > hh {
						sig[i] = 1
					} else if b[i].C < ll {
						sig[i] = -1
					}
				}
				return sig
			},
		},
		{
			Key: "roc_12", Name: "动量 ROC(12)", Desc: "12 日变动率上穿 0 买入，下穿 0 卖出",
			Signals: func(b []BarRow) []int {
				c := cl(b)
				roc := make([]float64, len(c))
				for i := range c {
					if i < 12 {
						roc[i] = math.NaN()
					} else {
						roc[i] = (c[i] - c[i-12]) / c[i-12] * 100
					}
				}
				zero := constSeq(roc, 0)
				sig := make([]int, len(c))
				for i := range c {
					if crossUp(roc, zero, i) {
						sig[i] = 1
					} else if crossDown(roc, zero, i) {
						sig[i] = -1
					}
				}
				return sig
			},
		},
		{
			Key: "stoch_14", Name: "随机指标 KD(14)", Desc: "低位 %K 上穿 %D 买入，高位下穿卖出",
			Signals: func(b []BarRow) []int {
				h, l, c := highs(b), lows(b), cl(b)
				k := make([]float64, len(b))
				for i := range b {
					if i < 13 {
						k[i] = math.NaN()
						continue
					}
					hh, ll := h[i], l[i]
					for j := i - 13; j <= i; j++ {
						if h[j] > hh {
							hh = h[j]
						}
						if l[j] < ll {
							ll = l[j]
						}
					}
					if hh == ll {
						k[i] = 50
					} else {
						k[i] = (c[i] - ll) / (hh - ll) * 100
					}
				}
				d := smaSeq(k, 3)
				sig := make([]int, len(b))
				for i := range b {
					if math.IsNaN(k[i]) || math.IsNaN(d[i]) {
						continue
					}
					if crossUp(k, d, i) && k[i] < 30 {
						sig[i] = 1
					} else if crossDown(k, d, i) && k[i] > 70 {
						sig[i] = -1
					}
				}
				return sig
			},
		},
		{
			Key: "meanrev_sma20", Name: "均值回归 SMA20±5%", Desc: "偏离 20 日均线 -5% 买入，+5% 卖出",
			Signals: func(b []BarRow) []int {
				c := cl(b)
				mid := smaSeq(c, 20)
				sig := make([]int, len(b))
				for i := range b {
					if math.IsNaN(mid[i]) {
						continue
					}
					if c[i] < mid[i]*0.95 {
						sig[i] = 1
					} else if c[i] > mid[i]*1.05 {
						sig[i] = -1
					}
				}
				return sig
			},
		},
	}
}

// ===================== 回测 =====================

// Backtest 是一个策略在一段 K 线上的回测结果。
type Backtest struct {
	Trades      int     `json:"trades"`
	Wins        int     `json:"wins"`
	WinRate     float64 `json:"winRate"`     // 胜率 %
	AvgReturn   float64 `json:"avgReturn"`   // 每笔平均收益 %
	TotalReturn float64 `json:"totalReturn"` // 复利累计收益 %
}

// backtest 以「信号买入、反向信号卖出」做多回测（不含手续费/滑点）。
func backtest(bars []BarRow, sig []int) Backtest {
	var bt Backtest
	pos := false
	entry := 0.0
	equity := 1.0
	var sumRet float64
	closeTrade := func(exit float64) {
		ret := (exit - entry) / entry
		sumRet += ret
		equity *= (1 + ret)
		bt.Trades++
		if ret > 0 {
			bt.Wins++
		}
	}
	for i := range bars {
		if !pos && sig[i] == 1 {
			pos = true
			entry = bars[i].C
		} else if pos && sig[i] == -1 {
			pos = false
			closeTrade(bars[i].C)
		}
	}
	// 期末仍持仓：按最后一根收盘平掉，计入统计。
	if pos && len(bars) > 0 {
		closeTrade(bars[len(bars)-1].C)
	}
	if bt.Trades > 0 {
		bt.WinRate = float64(bt.Wins) / float64(bt.Trades) * 100
		bt.AvgReturn = sumRet / float64(bt.Trades) * 100
	}
	bt.TotalReturn = (equity - 1) * 100
	return bt
}

// currentSignal 返回策略在最新一根 K 线上的信号（用实时价替换末根收盘）。
func currentSignal(strat Strategy, bars []BarRow, lastPrice float64) int {
	if len(bars) == 0 {
		return 0
	}
	if lastPrice > 0 {
		b := make([]BarRow, len(bars))
		copy(b, bars)
		b[len(b)-1].C = lastPrice
		bars = b
	}
	sig := strat.Signals(bars)
	return sig[len(sig)-1]
}

// ===================== 实时共振提醒 =====================

// consensus 统计最新一根 K 线上有多少策略看多/看空，用于生成共振提醒。
func consensus(bars []BarRow, lastPrice float64) (buy, sell int) {
	for _, s := range strategies() {
		switch currentSignal(s, bars, lastPrice) {
		case 1:
			buy++
		case -1:
			sell++
		}
	}
	return
}

// consensusSignals 若达到阈值则产出共振提醒。
func consensusSignals(symbol string, bars []BarRow, lastPrice float64) []Signal {
	const threshold = 3
	if len(bars) < 61 {
		return nil
	}
	buy, sell := consensus(bars, lastPrice)
	day := time.Unix(bars[len(bars)-1].Time, 0).UTC().Format("2006-01-02")
	price := lastPrice
	if price == 0 {
		price = bars[len(bars)-1].C
	}
	var out []Signal
	if buy >= threshold && buy > sell {
		out = append(out, Signal{
			Kind:     "consensus_buy",
			Message:  fmt.Sprintf("%s 多策略共振看多（%d/10 买入信号），建议关注买入", symbol, buy),
			Price:    price,
			DedupKey: fmt.Sprintf("%s|consensus_buy|%s", symbol, day),
		})
	} else if sell >= threshold && sell > buy {
		out = append(out, Signal{
			Kind:     "consensus_sell",
			Message:  fmt.Sprintf("%s 多策略共振看空（%d/10 卖出信号），建议关注卖出", symbol, sell),
			Price:    price,
			DedupKey: fmt.Sprintf("%s|consensus_sell|%s", symbol, day),
		})
	}
	return out
}

// Signal 是一条要落库的提醒。
type Signal struct {
	Kind     string
	Message  string
	Price    float64
	DedupKey string
}
