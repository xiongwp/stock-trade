"use strict";

// ---------- 全局状态 ----------
const state = {
  selected: null,      // 当前选中的代码
  symbols: [],         // 自选列表（含报价）
  chart: null,
  series: {},          // candle / vol / ma5 / ma20
};

const $ = (id) => document.getElementById(id);

function fmtUsd(n) {
  const s = n < 0 ? "-" : "";
  return s + "$" + Math.abs(n).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}
function fmtNum(n, d = 2) {
  return Number(n).toLocaleString("en-US", { minimumFractionDigits: d, maximumFractionDigits: d });
}
function signCls(n) { return n > 0 ? "up" : n < 0 ? "down" : "flat"; }

// ---------- 图表 ----------
function initChart() {
  const el = $("chart");
  const chart = LightweightCharts.createChart(el, {
    autoSize: true,
    layout: { background: { color: "#171a21" }, textColor: "#8b93a4", fontFamily: "inherit" },
    grid: { vertLines: { color: "#262b36" }, horzLines: { color: "#262b36" } },
    rightPriceScale: { borderColor: "#262b36" },
    timeScale: { borderColor: "#262b36" },
    crosshair: { mode: LightweightCharts.CrosshairMode.Normal },
  });
  const candle = chart.addCandlestickSeries({
    upColor: "#ef4444", wickUpColor: "#ef4444", borderUpColor: "#ef4444",
    downColor: "#22c55e", wickDownColor: "#22c55e", borderDownColor: "#22c55e",
  });
  const vol = chart.addHistogramSeries({ priceFormat: { type: "volume" }, priceScaleId: "volume" });
  chart.priceScale("volume").applyOptions({ scaleMargins: { top: 0.82, bottom: 0 } });
  const ma5 = chart.addLineSeries({ color: "#f59e0b", lineWidth: 1, priceLineVisible: false, lastValueVisible: false });
  const ma20 = chart.addLineSeries({ color: "#a855f7", lineWidth: 1, priceLineVisible: false, lastValueVisible: false });

  state.chart = chart;
  state.series = { candle, vol, ma5, ma20 };
}

function toChartTime(unixSec) {
  const d = new Date(unixSec * 1000);
  return { year: d.getUTCFullYear(), month: d.getUTCMonth() + 1, day: d.getUTCDate() };
}

function smaSeries(bars, period) {
  const out = [];
  let sum = 0;
  for (let i = 0; i < bars.length; i++) {
    sum += bars[i].close;
    if (i >= period) sum -= bars[i - period].close;
    if (i >= period - 1) out.push({ time: toChartTime(bars[i].time), value: sum / period });
  }
  return out;
}

async function loadChart(symbol) {
  const bars = await (await fetch(`/api/bars/${symbol}`)).json();
  const candles = bars.map((b) => ({ time: toChartTime(b.time), open: b.open, high: b.high, low: b.low, close: b.close }));
  const vols = bars.map((b) => ({ time: toChartTime(b.time), value: b.volume, color: b.close >= b.open ? "rgba(239,68,68,.4)" : "rgba(34,197,94,.4)" }));
  state.series.candle.setData(candles);
  state.series.vol.setData(vols);
  state.series.ma5.setData(smaSeries(bars, 5));
  state.series.ma20.setData(smaSeries(bars, 20));
  state.chart.timeScale().fitContent();
}

// ---------- 自选列表 ----------
async function loadSymbols() {
  state.symbols = await (await fetch("/api/symbols")).json();
  renderWatchlist();
  $("watchEmpty").hidden = state.symbols.length > 0;
  if (!state.selected && state.symbols.length) selectSymbol(state.symbols[0].symbol);
  if (state.selected) updateChartHead();
}

function renderWatchlist() {
  const ul = $("watchlist");
  ul.innerHTML = "";
  for (const s of state.symbols) {
    const chg = s.lastPrice - s.prevClose;
    const pct = s.prevClose > 0 ? (chg / s.prevClose) * 100 : 0;
    const cls = signCls(chg);
    const li = document.createElement("li");
    li.className = "watch-item" + (s.symbol === state.selected ? " active" : "");
    li.innerHTML = `
      <div class="w-left">
        <div class="w-sym">${s.symbol}</div>
        <div class="w-name">${s.name || ""}</div>
      </div>
      <div class="w-right">
        <div class="w-price">${s.lastPrice ? fmtNum(s.lastPrice) : "—"}</div>
        <div class="w-chg ${cls}">${chg >= 0 ? "+" : ""}${fmtNum(pct)}%</div>
      </div>
      <button class="watch-del" title="移除">✕</button>`;
    li.querySelector(".w-left").addEventListener("click", () => selectSymbol(s.symbol));
    li.querySelector(".w-right").addEventListener("click", () => selectSymbol(s.symbol));
    li.querySelector(".watch-del").addEventListener("click", (e) => { e.stopPropagation(); removeSymbol(s.symbol); });
    ul.appendChild(li);
  }
}

function updateChartHead() {
  const s = state.symbols.find((x) => x.symbol === state.selected);
  if (!s) return;
  const chg = s.lastPrice - s.prevClose;
  const pct = s.prevClose > 0 ? (chg / s.prevClose) * 100 : 0;
  const cls = signCls(chg);
  $("chartSymbol").textContent = s.symbol;
  $("chartName").textContent = s.name || "";
  $("chartPrice").textContent = s.lastPrice ? fmtNum(s.lastPrice) : "—";
  const chgEl = $("chartChange");
  chgEl.className = "chart-change " + cls;
  chgEl.textContent = s.lastPrice ? `${chg >= 0 ? "+" : ""}${fmtNum(chg)} (${chg >= 0 ? "+" : ""}${fmtNum(pct)}%)` : "";
}

async function selectSymbol(symbol) {
  state.selected = symbol;
  renderWatchlist();
  updateChartHead();
  $("posSymbol").textContent = symbol;
  $("posForm").buyTime.value = new Date().toISOString().slice(0, 10);
  await loadChart(symbol);
  await loadPositions();
  await loadStrategies();
}

// ---------- 策略排行 ----------
const sigBadge = { 1: ["买入", "up"], "-1": ["卖出", "down"], 0: ["观望", "flat"] };

async function loadStrategies() {
  const sym = state.selected;
  $("stratSymbol").textContent = sym || "—";
  const stats = await fetch(`/api/strategies?symbol=${encodeURIComponent(sym || "")}`).then((r) => r.json());
  const tbody = $("stratRows");
  tbody.innerHTML = "";
  stats.forEach((s, i) => {
    const [label, cls] = sigBadge[s.currentSignal] || sigBadge[0];
    const wr = s.trades > 0 ? fmtNum(s.winRate) + "%" : "—";
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td class="rank">${i + 1}</td>
      <td><div class="strat-name">${s.name}</div><div class="strat-desc">${s.desc}</div></td>
      <td class="num strat-wr">${wr}</td>
      <td class="num flat">${s.trades}</td>
      <td class="num ${signCls(s.avgReturn)}">${s.avgReturn >= 0 ? "+" : ""}${fmtNum(s.avgReturn)}%</td>
      <td class="num ${signCls(s.totalReturn)}">${s.totalReturn >= 0 ? "+" : ""}${fmtNum(s.totalReturn)}%</td>
      <td><span class="sig-badge ${cls}">${label}</span>${s.symbolTrades ? ` <span class="flat" style="font-size:11px">(该股胜率 ${fmtNum(s.symbolWinRate)}%)</span>` : ""}</td>`;
    tbody.appendChild(tr);
  });
}

async function removeSymbol(symbol) {
  if (!confirm(`移除 ${symbol}？（同时删除本地历史 K 线）`)) return;
  await fetch(`/api/symbols/${symbol}`, { method: "DELETE" });
  if (state.selected === symbol) state.selected = null;
  await loadSymbols();
  await loadPnl();
}

// ---------- 检索 ----------
let searchTimer = null;
$("searchInput").addEventListener("input", (e) => {
  clearTimeout(searchTimer);
  const q = e.target.value.trim();
  if (!q) { $("searchResults").hidden = true; return; }
  searchTimer = setTimeout(() => runSearch(q), 250);
});
document.addEventListener("click", (e) => {
  if (!e.target.closest(".search-box")) $("searchResults").hidden = true;
});

async function runSearch(q) {
  const results = await (await fetch(`/api/search?q=${encodeURIComponent(q)}`)).json();
  const box = $("searchResults");
  box.innerHTML = "";
  if (!results.length) { box.hidden = true; return; }
  for (const a of results) {
    const div = document.createElement("div");
    div.className = "search-item";
    div.innerHTML = `<span class="s-sym">${a.symbol}</span><span class="s-name">${a.name || ""}</span>`;
    div.addEventListener("click", () => addSymbol(a.symbol));
    box.appendChild(div);
  }
  box.hidden = false;
}

async function addSymbol(symbol) {
  $("searchResults").hidden = true;
  $("searchInput").value = "";
  const res = await fetch("/api/symbols", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ symbol }),
  });
  if (!res.ok) { const e = await res.json().catch(() => ({})); alert(e.error || "添加失败"); return; }
  await loadSymbols();
  selectSymbol(symbol);
}

// ---------- 持仓 & 盈亏 ----------
let currentPnl = { rows: [], totals: {} };

async function loadPositions() {
  const [positions, pnlRes] = await Promise.all([
    fetch("/api/positions").then((r) => r.json()),
    fetch("/api/pnl").then((r) => r.json()),
  ]);
  currentPnl = pnlRes;
  const sym = state.selected;
  const mine = positions.filter((p) => p.symbol === sym);
  const priceRow = pnlRes.rows.find((r) => r.symbol === sym);
  const lastPrice = priceRow ? priceRow.lastPrice : (state.symbols.find((s) => s.symbol === sym)?.lastPrice || 0);

  const tbody = $("posRows");
  tbody.innerHTML = "";
  $("posEmpty").hidden = mine.length > 0;
  for (const p of mine) {
    const closed = !!p.sellTime;
    // 已卖出：用卖出价算已实现盈亏；持仓中：用现价算浮动盈亏。
    const exitPrice = closed ? p.sellPrice : lastPrice;
    const pnl = (exitPrice - p.buyPrice) * p.quantity;
    const pct = p.buyPrice > 0 ? (exitPrice - p.buyPrice) / p.buyPrice * 100 : 0;
    const cls = signCls(pnl);
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${p.buyTime}${p.note ? ` <span class="flat">· ${p.note}</span>` : ""}</td>
      <td class="num">${fmtNum(p.quantity, 4)}</td>
      <td class="num">${fmtNum(p.buyPrice)}</td>
      <td>${closed ? p.sellTime : '<span class="flat">持仓中</span>'}</td>
      <td class="num">${exitPrice ? fmtNum(exitPrice) : "—"}</td>
      <td class="num ${cls}">${pnl >= 0 ? "+" : ""}${fmtUsd(pnl)}${closed ? '' : ' <span class="flat" style="font-size:10px">浮</span>'}</td>
      <td class="num ${cls}">${pct >= 0 ? "+" : ""}${fmtNum(pct)}%</td>
      <td class="num pos-actions"></td>`;
    const act = tr.querySelector(".pos-actions");
    if (closed) {
      const undo = document.createElement("button");
      undo.className = "pos-link"; undo.textContent = "撤销卖出"; undo.title = "改回持仓";
      undo.addEventListener("click", () => undoSell(p.id));
      act.appendChild(undo);
    } else {
      const sell = document.createElement("button");
      sell.className = "pos-link sell"; sell.textContent = "卖出";
      sell.addEventListener("click", () => openSellModal(p, lastPrice));
      act.appendChild(sell);
    }
    const del = document.createElement("button");
    del.className = "pos-del"; del.title = "删除"; del.textContent = "✕";
    del.addEventListener("click", () => deletePosition(p.id));
    act.appendChild(del);
    tbody.appendChild(tr);
  }

  const posPnl = $("posPnl");
  if (priceRow) {
    const cls = signCls(priceRow.unrealizedUsd);
    posPnl.className = "pos-pnl " + cls;
    posPnl.textContent = `${priceRow.unrealizedUsd >= 0 ? "+" : ""}${fmtUsd(priceRow.unrealizedUsd)} (${fmtNum(priceRow.unrealizedPct)}%)`;
  } else {
    posPnl.textContent = "";
  }
  renderTotals(pnlRes.totals);
}

function renderTotals(totals) {
  const usd = totals.unrealizedUsd || 0;
  const el = $("totalPnl");
  el.className = "totals-value " + signCls(usd);
  el.textContent = `${usd >= 0 ? "+" : ""}${fmtUsd(usd)} (${fmtNum(totals.unrealizedPct || 0)}%)`;

  const rz = totals.realizedUsd || 0;
  const rel = $("totalRealized");
  rel.className = "totals-value " + signCls(rz);
  rel.textContent = `${rz >= 0 ? "+" : ""}${fmtUsd(rz)}`;
}

// ---------- 卖出弹窗 ----------
let sellingId = null;
function openSellModal(p, lastPrice) {
  sellingId = p.id;
  $("sellModalSym").textContent = p.symbol;
  $("sellModalInfo").textContent = `买入 ${fmtNum(p.quantity, 4)} 股 @ $${fmtNum(p.buyPrice)}（${p.buyTime}）`;
  $("sellPriceInput").value = lastPrice ? lastPrice.toFixed(2) : p.buyPrice.toFixed(2);
  $("sellTimeInput").value = new Date().toISOString().slice(0, 10);
  $("sellModal").hidden = false;
}
function closeSellModal() { $("sellModal").hidden = true; sellingId = null; }
$("sellCancel").addEventListener("click", closeSellModal);
$("sellModal").addEventListener("click", (e) => { if (e.target.id === "sellModal") closeSellModal(); });
$("sellConfirm").addEventListener("click", async () => {
  if (sellingId == null) return;
  const price = parseFloat($("sellPriceInput").value);
  const time = $("sellTimeInput").value;
  if (isNaN(price) || price < 0 || !time) { alert("请填写有效的卖出价与时间"); return; }
  const res = await fetch(`/api/positions/${sellingId}/sell`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ sellPrice: price, sellTime: time }),
  });
  if (!res.ok) { const e = await res.json().catch(() => ({})); alert(e.error || "卖出失败"); return; }
  closeSellModal();
  await loadPositions();
  await loadStats();
}

async function undoSell(id) {
  if (!confirm("撤销这笔卖出，改回持仓？")) return;
  await fetch(`/api/positions/${id}/sell`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ sellTime: "" }),
  });
  await loadPositions();
  await loadStats();
}

// ---------- 已实现盈亏统计（周/月） ----------
let statsData = { weekly: [], monthly: [] };
let statsPeriod = "monthly";

async function loadStats() {
  statsData = await fetch("/api/stats").then((r) => r.json());
  renderStats();
}

function renderStats() {
  const rows = statsData[statsPeriod] || [];
  const tbody = $("statRows");
  tbody.innerHTML = "";
  $("statEmpty").hidden = rows.length > 0;
  for (const s of rows) {
    const cls = signCls(s.realizedUsd);
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${s.label}</td>
      <td class="num ${cls}">${s.realizedUsd >= 0 ? "+" : ""}${fmtUsd(s.realizedUsd)}</td>
      <td class="num flat">${s.trades}</td>
      <td class="num flat">${s.wins}</td>
      <td class="num">${fmtNum(s.winRate)}%</td>`;
    tbody.appendChild(tr);
  }
}

document.querySelectorAll(".stats-tabs .tab").forEach((btn) => {
  btn.addEventListener("click", () => {
    document.querySelectorAll(".stats-tabs .tab").forEach((b) => b.classList.remove("active"));
    btn.classList.add("active");
    statsPeriod = btn.dataset.period;
    renderStats();
  });
});

async function loadPnl() {
  const pnlRes = await fetch("/api/pnl").then((r) => r.json());
  currentPnl = pnlRes;
  renderTotals(pnlRes.totals);
}

$("posForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  if (!state.selected) { alert("请先在左侧选择一只股票"); return; }
  const f = e.target;
  const body = {
    symbol: state.selected,
    quantity: parseFloat(f.quantity.value),
    buyPrice: parseFloat(f.buyPrice.value),
    buyTime: f.buyTime.value,
    note: f.note.value,
  };
  const res = await fetch("/api/positions", {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
  });
  if (!res.ok) { const err = await res.json().catch(() => ({})); alert(err.error || "记录失败"); return; }
  f.quantity.value = ""; f.buyPrice.value = ""; f.note.value = "";
  await loadPositions();
});

async function deletePosition(id) {
  if (!confirm("删除这条买入记录？")) return;
  await fetch(`/api/positions/${id}`, { method: "DELETE" });
  await loadPositions();
}

// ---------- 提醒 ----------
const kindLabel = { golden_cross: "金叉·买", death_cross: "死叉·卖", overbought: "超买", oversold: "超卖" };

async function loadAlerts() {
  const alerts = await fetch("/api/alerts").then((r) => r.json());
  const ul = $("alertList");
  ul.innerHTML = "";
  $("alertEmpty").hidden = alerts.length > 0;
  for (const a of alerts) {
    const li = document.createElement("li");
    li.className = "alert-item" + (a.acknowledged ? " ack" : "");
    li.innerHTML = `
      <span class="alert-kind k-${a.kind}">${kindLabel[a.kind] || a.kind}</span>
      <div class="alert-msg">${a.message}</div>
      <div class="alert-meta">
        <span>${new Date(a.createdAt).toLocaleString("zh-CN")}</span>
        ${a.acknowledged ? "" : `<button class="alert-ack-btn">已读</button>`}
      </div>`;
    const btn = li.querySelector(".alert-ack-btn");
    if (btn) btn.addEventListener("click", async () => { await fetch(`/api/alerts/${a.id}/ack`, { method: "POST" }); loadAlerts(); });
    li.querySelector(".alert-msg").addEventListener("click", () => {
      if (state.symbols.find((s) => s.symbol === a.symbol)) selectSymbol(a.symbol);
    });
    ul.appendChild(li);
  }
}

// ---------- 刷新 & 轮询 ----------
$("refreshBtn").addEventListener("click", async () => {
  await fetch("/api/refresh", { method: "POST" });
  await refreshLive();
});

async function refreshLive() {
  await loadSymbols();
  await loadPnl();
  await loadAlerts();
  await loadStats();
  if (state.selected) { updateChartHead(); await loadStrategies(); }
}

// ---------- 启动 ----------
initChart();
loadSymbols().then(() => { loadPnl(); loadAlerts(); loadStats(); });
setInterval(refreshLive, 30000);
