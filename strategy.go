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

// rocSeq n 日变动率（%）。
func rocSeq(vals []float64, n int) []float64 {
	out := make([]float64, len(vals))
	for i := range vals {
		if i < n || vals[i-n] == 0 {
			out[i] = math.NaN()
		} else {
			out[i] = (vals[i] - vals[i-n]) / vals[i-n] * 100
		}
	}
	return out
}

// stochKSeq 随机指标 %K。
func stochKSeq(bars []BarRow, n int) []float64 {
	h, l, c := highs(bars), lows(bars), closes(bars)
	out := make([]float64, len(bars))
	for i := range bars {
		if i < n-1 {
			out[i] = math.NaN()
			continue
		}
		hh, ll := h[i], l[i]
		for j := i - n + 1; j <= i; j++ {
			if h[j] > hh {
				hh = h[j]
			}
			if l[j] < ll {
				ll = l[j]
			}
		}
		if hh == ll {
			out[i] = 50
		} else {
			out[i] = (c[i] - ll) / (hh - ll) * 100
		}
	}
	return out
}

// williamsRSeq 威廉指标 %R（-100..0）。
func williamsRSeq(bars []BarRow, n int) []float64 {
	h, l, c := highs(bars), lows(bars), closes(bars)
	out := make([]float64, len(bars))
	for i := range bars {
		if i < n-1 {
			out[i] = math.NaN()
			continue
		}
		hh, ll := h[i], l[i]
		for j := i - n + 1; j <= i; j++ {
			if h[j] > hh {
				hh = h[j]
			}
			if l[j] < ll {
				ll = l[j]
			}
		}
		if hh == ll {
			out[i] = -50
		} else {
			out[i] = (hh - c[i]) / (hh - ll) * -100
		}
	}
	return out
}

// cciSeq 顺势指标 CCI。
func cciSeq(bars []BarRow, n int) []float64 {
	tp := make([]float64, len(bars))
	for i, b := range bars {
		tp[i] = (b.H + b.L + b.C) / 3
	}
	smaTP := smaSeq(tp, n)
	out := make([]float64, len(bars))
	for i := range bars {
		if math.IsNaN(smaTP[i]) {
			out[i] = math.NaN()
			continue
		}
		md := 0.0
		for j := i - n + 1; j <= i; j++ {
			md += math.Abs(tp[j] - smaTP[i])
		}
		md /= float64(n)
		if md == 0 {
			out[i] = 0
		} else {
			out[i] = (tp[i] - smaTP[i]) / (0.015 * md)
		}
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

// ---------- 策略生成器（参数化，便于批量扩展到 Top50） ----------

func maCross(f, s int) Strategy {
	return Strategy{
		Key: fmt.Sprintf("ma_%d_%d", f, s), Name: fmt.Sprintf("均线 MA%d/MA%d", f, s),
		Desc: fmt.Sprintf("MA%d 上穿 MA%d 金叉买入，下穿死叉卖出", f, s),
		Signals: crossStrategy(
			func(b []BarRow) []float64 { return smaSeq(closes(b), f) },
			func(b []BarRow) []float64 { return smaSeq(closes(b), s) }),
	}
}

func emaCross(f, s int) Strategy {
	return Strategy{
		Key: fmt.Sprintf("ema_%d_%d", f, s), Name: fmt.Sprintf("EMA %d/%d", f, s),
		Desc: fmt.Sprintf("EMA%d 上穿 EMA%d 买入，下穿卖出", f, s),
		Signals: crossStrategy(
			func(b []BarRow) []float64 { return emaSeq(closes(b), f) },
			func(b []BarRow) []float64 { return emaSeq(closes(b), s) }),
	}
}

func macdStrat(f, s, sg int) Strategy {
	return Strategy{
		Key: fmt.Sprintf("macd_%d_%d_%d", f, s, sg), Name: fmt.Sprintf("MACD(%d,%d,%d)", f, s, sg),
		Desc: "MACD 线上穿信号线买入，下穿卖出",
		Signals: func(b []BarRow) []int {
			c := closes(b)
			fast, slow := emaSeq(c, f), emaSeq(c, s)
			macd := make([]float64, len(c))
			for i := range c {
				macd[i] = fast[i] - slow[i]
			}
			signal := emaSeq(macd, sg)
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
	}
}

func rsiStrat(p int, lo, hi float64) Strategy {
	return Strategy{
		Key: fmt.Sprintf("rsi_%d_%.0f_%.0f", p, lo, hi), Name: fmt.Sprintf("RSI%d(%.0f/%.0f)", p, lo, hi),
		Desc: fmt.Sprintf("RSI 上穿 %.0f 买入，下穿 %.0f 卖出", lo, hi),
		Signals: func(b []BarRow) []int {
			r := rsiSeq(closes(b), p)
			loS, hiS := constSeq(r, lo), constSeq(r, hi)
			sig := make([]int, len(b))
			for i := range b {
				if crossUp(r, loS, i) {
					sig[i] = 1
				} else if crossDown(r, hiS, i) {
					sig[i] = -1
				}
			}
			return sig
		},
	}
}

func bollStrat(p int, k float64) Strategy {
	return Strategy{
		Key: fmt.Sprintf("boll_%d_%.1f", p, k), Name: fmt.Sprintf("布林带(%d,%.1f)", p, k),
		Desc: "触及下轨买入，触及上轨卖出（均值回归）",
		Signals: func(b []BarRow) []int {
			c := closes(b)
			mid, sd := smaSeq(c, p), stddevSeq(c, p)
			sig := make([]int, len(b))
			for i := range b {
				if math.IsNaN(mid[i]) || math.IsNaN(sd[i]) {
					continue
				}
				if c[i] <= mid[i]-k*sd[i] {
					sig[i] = 1
				} else if c[i] >= mid[i]+k*sd[i] {
					sig[i] = -1
				}
			}
			return sig
		},
	}
}

func donchianStrat(n int) Strategy {
	return Strategy{
		Key: fmt.Sprintf("donchian_%d", n), Name: fmt.Sprintf("唐奇安通道(%d)", n),
		Desc: fmt.Sprintf("突破 %d 日新高买入，跌破 %d 日新低卖出", n, n),
		Signals: func(b []BarRow) []int {
			h, l := highs(b), lows(b)
			sig := make([]int, len(b))
			for i := n; i < len(b); i++ {
				hh, ll := h[i-1], l[i-1]
				for j := i - n; j < i; j++ {
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
	}
}

func rocStrat(n int) Strategy {
	return Strategy{
		Key: fmt.Sprintf("roc_%d", n), Name: fmt.Sprintf("动量 ROC(%d)", n),
		Desc: fmt.Sprintf("%d 日变动率上穿 0 买入，下穿 0 卖出", n),
		Signals: func(b []BarRow) []int {
			roc := rocSeq(closes(b), n)
			zero := constSeq(roc, 0)
			sig := make([]int, len(b))
			for i := range b {
				if crossUp(roc, zero, i) {
					sig[i] = 1
				} else if crossDown(roc, zero, i) {
					sig[i] = -1
				}
			}
			return sig
		},
	}
}

func stochStrat(n int, loZone, hiZone float64) Strategy {
	return Strategy{
		Key: fmt.Sprintf("stoch_%d", n), Name: fmt.Sprintf("随机指标 KD(%d)", n),
		Desc: "低位 %K 上穿 %D 买入，高位下穿卖出",
		Signals: func(b []BarRow) []int {
			k := stochKSeq(b, n)
			d := smaSeq(k, 3)
			sig := make([]int, len(b))
			for i := range b {
				if math.IsNaN(k[i]) || math.IsNaN(d[i]) {
					continue
				}
				if crossUp(k, d, i) && k[i] < loZone {
					sig[i] = 1
				} else if crossDown(k, d, i) && k[i] > hiZone {
					sig[i] = -1
				}
			}
			return sig
		},
	}
}

func meanRevStrat(p int, band float64) Strategy {
	return Strategy{
		Key: fmt.Sprintf("meanrev_%d_%.0f", p, band*100), Name: fmt.Sprintf("均值回归 SMA%d±%.0f%%", p, band*100),
		Desc: fmt.Sprintf("偏离 %d 日均线 -%.0f%% 买入，+%.0f%% 卖出", p, band*100, band*100),
		Signals: func(b []BarRow) []int {
			c := closes(b)
			mid := smaSeq(c, p)
			sig := make([]int, len(b))
			for i := range b {
				if math.IsNaN(mid[i]) {
					continue
				}
				if c[i] < mid[i]*(1-band) {
					sig[i] = 1
				} else if c[i] > mid[i]*(1+band) {
					sig[i] = -1
				}
			}
			return sig
		},
	}
}

func williamsStrat(p int) Strategy {
	return Strategy{
		Key: fmt.Sprintf("wr_%d", p), Name: fmt.Sprintf("威廉指标 %%R(%d)", p),
		Desc: "上穿 -80 买入，下穿 -20 卖出",
		Signals: func(b []BarRow) []int {
			wr := williamsRSeq(b, p)
			loS, hiS := constSeq(wr, -80), constSeq(wr, -20)
			sig := make([]int, len(b))
			for i := range b {
				if crossUp(wr, loS, i) {
					sig[i] = 1
				} else if crossDown(wr, hiS, i) {
					sig[i] = -1
				}
			}
			return sig
		},
	}
}

func cciStrat(n int, level float64) Strategy {
	return Strategy{
		Key: fmt.Sprintf("cci_%d_%.0f", n, level), Name: fmt.Sprintf("CCI(%d,±%.0f)", n, level),
		Desc: fmt.Sprintf("CCI 上穿 -%.0f 买入，下穿 +%.0f 卖出", level, level),
		Signals: func(b []BarRow) []int {
			cci := cciSeq(b, n)
			loS, hiS := constSeq(cci, -level), constSeq(cci, level)
			sig := make([]int, len(b))
			for i := range b {
				if crossUp(cci, loS, i) {
					sig[i] = 1
				} else if crossDown(cci, hiS, i) {
					sig[i] = -1
				}
			}
			return sig
		},
	}
}

func emaTrendStrat(f, s, t int) Strategy {
	return Strategy{
		Key: fmt.Sprintf("ematrend_%d_%d_%d", f, s, t), Name: fmt.Sprintf("EMA%d/%d + MA%d 趋势", f, s, t),
		Desc: fmt.Sprintf("金叉且价在 MA%d 上方才买入，死叉即卖出", t),
		Signals: func(b []BarRow) []int {
			c := closes(b)
			fast, slow, trend := emaSeq(c, f), emaSeq(c, s), smaSeq(c, t)
			sig := make([]int, len(b))
			for i := range b {
				if crossUp(fast, slow, i) && !math.IsNaN(trend[i]) && c[i] > trend[i] {
					sig[i] = 1
				} else if crossDown(fast, slow, i) {
					sig[i] = -1
				}
			}
			return sig
		},
	}
}

// strategies 返回全部内置策略（约 50 个经典技术策略，用于胜率排行 Top50）。
func strategies() []Strategy {
	return []Strategy{
		// 均线金叉死叉（10）
		maCross(5, 10), maCross(5, 20), maCross(8, 21), maCross(10, 20), maCross(10, 30),
		maCross(10, 50), maCross(20, 50), maCross(20, 60), maCross(50, 100), maCross(50, 200),
		// 指数均线（6）
		emaCross(5, 13), emaCross(8, 21), emaCross(9, 21), emaCross(12, 26), emaCross(20, 50), emaCross(21, 55),
		// MACD（2）
		macdStrat(12, 26, 9), macdStrat(5, 35, 5),
		// RSI（6）
		rsiStrat(6, 30, 70), rsiStrat(6, 20, 80), rsiStrat(14, 30, 70), rsiStrat(14, 20, 80), rsiStrat(21, 30, 70), rsiStrat(9, 25, 75),
		// 布林带（4）
		bollStrat(20, 2.0), bollStrat(20, 2.5), bollStrat(10, 1.5), bollStrat(20, 1.5),
		// 唐奇安通道（4）
		donchianStrat(10), donchianStrat(20), donchianStrat(30), donchianStrat(55),
		// 动量 ROC（5）
		rocStrat(5), rocStrat(10), rocStrat(12), rocStrat(20), rocStrat(25),
		// 随机指标 KD（3）
		stochStrat(9, 20, 80), stochStrat(14, 30, 70), stochStrat(21, 20, 80),
		// 均值回归（4）
		meanRevStrat(20, 0.03), meanRevStrat(20, 0.05), meanRevStrat(20, 0.08), meanRevStrat(50, 0.05),
		// 威廉指标（2）
		williamsStrat(14), williamsStrat(21),
		// CCI（2）
		cciStrat(20, 100), cciStrat(20, 150),
		// 趋势过滤（2）
		emaTrendStrat(5, 20, 100), emaTrendStrat(10, 50, 200),
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

// consensus 统计最新一根 K 线上有多少策略看多/看空。
func consensus(bars []BarRow, lastPrice float64) (buy, sell, total int) {
	all := strategies()
	total = len(all)
	for _, s := range all {
		switch currentSignal(s, bars, lastPrice) {
		case 1:
			buy++
		case -1:
			sell++
		}
	}
	return
}

// consensusSignals 当同向策略占比达阈值（约 30%）时产出共振提醒。
func consensusSignals(symbol string, bars []BarRow, lastPrice float64) []Signal {
	if len(bars) < 61 {
		return nil
	}
	buy, sell, total := consensus(bars, lastPrice)
	threshold := total * 3 / 10
	if threshold < 5 {
		threshold = 5
	}
	day := time.Unix(bars[len(bars)-1].Time, 0).UTC().Format("2006-01-02")
	price := lastPrice
	if price == 0 {
		price = bars[len(bars)-1].C
	}
	var out []Signal
	if buy >= threshold && buy > sell {
		out = append(out, Signal{
			Kind:     "consensus_buy",
			Message:  fmt.Sprintf("%s 多策略共振看多（%d/%d 买入信号），建议关注买入", symbol, buy, total),
			Price:    price,
			DedupKey: fmt.Sprintf("%s|consensus_buy|%s", symbol, day),
		})
	} else if sell >= threshold && sell > buy {
		out = append(out, Signal{
			Kind:     "consensus_sell",
			Message:  fmt.Sprintf("%s 多策略共振看空（%d/%d 卖出信号），建议关注卖出", symbol, sell, total),
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
