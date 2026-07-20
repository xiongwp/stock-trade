"use strict";

const state = {
  selected: null,
  tf: "1Day",
  ext: false, // 延长时段（夜盘+盘前+盘后），仅分钟图有效
  selectedStrategy: null,
  symbols: [],
  chart: null,
  series: {},
  strategies: [],
  entrySide: "long",
};

// 方向感知的单笔盈亏。做空：开仓=卖出价，平仓=买入价，浮动=(卖出价-现价)。
function lotPnl(p, lastPrice) {
  const short = p.side === "short";
  const closed = short ? !!p.buyTime : !!p.sellTime;
  const entry = short ? p.sellPrice : p.buyPrice;
  const exit = closed ? (short ? p.buyPrice : p.sellPrice) : lastPrice;
  const pnl = short ? (p.sellPrice - exit) * p.quantity : (exit - p.buyPrice) * p.quantity;
  const pct = entry > 0 ? (short ? (p.sellPrice - exit) : (exit - p.buyPrice)) / entry * 100 : 0;
  return { short, closed, entry, exit, entryTime: short ? p.sellTime : p.buyTime, closeTime: short ? p.buyTime : p.sellTime, pnl, pct };
}
const sideBadge = (short) => `<span class="side-badge ${short ? "short" : "long"}">${short ? "空" : "多"}</span>`;

const $ = (id) => document.getElementById(id);
const fmtUsd = (n) => (n < 0 ? "-" : "") + "$" + Math.abs(n).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const fmtNum = (n, d = 2) => Number(n).toLocaleString("en-US", { minimumFractionDigits: d, maximumFractionDigits: d });
const signCls = (n) => (n > 0 ? "up" : n < 0 ? "down" : "flat");
const isIntraday = (tf) => tf.endsWith("Min") || tf.endsWith("Hour");
const isMinuteTf = (tf) => tf.endsWith("Min");
// 延长时段仅对分钟图有效：拼接查询串。
const extQuery = () => (state.ext && isMinuteTf(state.tf) ? "&ext=1" : "");
const signStr = (n) => (n >= 0 ? "+" : "");

// ---------- 视图切换 ----------
function showView(name) {
  document.querySelectorAll(".view").forEach((v) => (v.hidden = v.id !== `view-${name}`));
  document.querySelectorAll(".nav-tab").forEach((t) => t.classList.toggle("active", t.dataset.view === name));
  if (name === "returns") loadReturns();
  if (name === "records") loadRecords();
  if (name === "trade" && state.chart) state.chart.timeScale().fitContent();
}
document.querySelectorAll(".nav-tab").forEach((t) => t.addEventListener("click", () => showView(t.dataset.view)));

// ---------- 图表 ----------
function initChart() {
  const chart = LightweightCharts.createChart($("chart"), {
    autoSize: true,
    layout: { background: { color: "#171a21" }, textColor: "#8b93a4", fontFamily: "inherit" },
    grid: { vertLines: { color: "#262b36" }, horzLines: { color: "#262b36" } },
    rightPriceScale: { borderColor: "#262b36" },
    timeScale: { borderColor: "#262b36", timeVisible: false, secondsVisible: false },
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

function smaSeries(bars, period) {
  const out = [];
  let sum = 0;
  for (let i = 0; i < bars.length; i++) {
    sum += bars[i].close;
    if (i >= period) sum -= bars[i - period].close;
    if (i >= period - 1) out.push({ time: bars[i].time, value: sum / period });
  }
  return out;
}

async function loadChart() {
  const symbol = state.selected;
  if (!symbol) return;
  let bars;
  try {
    bars = await (await fetch(`/api/bars/${symbol}?tf=${state.tf}${extQuery()}`)).json();
  } catch { bars = []; }
  if (!Array.isArray(bars)) bars = [];
  state.chart.applyOptions({ timeScale: { timeVisible: isIntraday(state.tf), secondsVisible: false } });
  state.series.candle.setData(bars.map((b) => ({ time: b.time, open: b.open, high: b.high, low: b.low, close: b.close })));
  state.series.vol.setData(bars.map((b) => ({ time: b.time, value: b.volume, color: b.close >= b.open ? "rgba(239,68,68,.4)" : "rgba(34,197,94,.4)" })));
  state.series.ma5.setData(smaSeries(bars, 5));
  state.series.ma20.setData(smaSeries(bars, 20));
  state.chart.timeScale().fitContent();
  await applyMarkers();
}

async function applyMarkers() {
  if (!state.selectedStrategy || !state.selected) {
    state.series.candle.setMarkers([]);
    $("markerLegend").hidden = true;
    return;
  }
  let pts = [];
  try {
    pts = await (await fetch(`/api/signals/${state.selected}?strategy=${state.selectedStrategy}&tf=${state.tf}${extQuery()}`)).json();
  } catch { pts = []; }
  if (!Array.isArray(pts)) pts = [];
  const markers = pts.map((p) =>
    p.side === 1
      ? { time: p.time, position: "belowBar", color: "#ef4444", shape: "arrowUp", text: "买" }
      : { time: p.time, position: "aboveBar", color: "#22c55e", shape: "arrowDown", text: "卖" }
  );
  markers.sort((a, b) => a.time - b.time);
  state.series.candle.setMarkers(markers);
  $("markerLegend").hidden = markers.length === 0;
}

// 延长时段开关随周期启用/禁用：仅分钟图可用。
function syncExtToggle() {
  const t = $("extToggle");
  if (!t) return;
  const usable = isMinuteTf(state.tf);
  t.disabled = !usable;
  t.classList.toggle("active", usable && state.ext);
}

// 周期切换（data-tf 才是周期按钮，排除延长时段开关）
document.querySelectorAll("#tfBar .tf[data-tf]").forEach((btn) => {
  btn.addEventListener("click", () => {
    document.querySelectorAll("#tfBar .tf[data-tf]").forEach((b) => b.classList.remove("active"));
    btn.classList.add("active");
    state.tf = btn.dataset.tf;
    syncExtToggle();
    loadChart();
  });
});

// 延长时段开关
$("extToggle").addEventListener("click", () => {
  if (!isMinuteTf(state.tf)) return;
  state.ext = !state.ext;
  syncExtToggle();
  loadChart();
});

// ---------- 自选列表 ----------
async function loadSymbols() {
  state.symbols = await (await fetch("/api/symbols")).json();
  renderWatchlist();
  $("watchEmpty").hidden = state.symbols.length > 0;
  if (!state.selected && state.symbols.length) await selectSymbol(state.symbols[0].symbol);
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
      <div class="w-left"><div class="w-sym">${s.symbol}</div><div class="w-name">${s.name || ""}</div></div>
      <div class="w-right"><div class="w-price">${s.lastPrice ? fmtNum(s.lastPrice) : "—"}</div>
        <div class="w-chg ${cls}">${signStr(chg)}${fmtNum(pct)}%</div></div>
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
  chgEl.textContent = s.lastPrice ? `${signStr(chg)}${fmtNum(chg)} (${signStr(chg)}${fmtNum(pct)}%)` : "";
}

async function selectSymbol(symbol) {
  state.selected = symbol;
  renderWatchlist();
  updateChartHead();
  $("posSymbol").textContent = symbol;
  $("posForm").buyTime.value = new Date().toISOString().slice(0, 10);
  await loadChart();
  await loadPositions();
  await loadStrategyList();
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
document.addEventListener("click", (e) => { if (!e.target.closest(".search-box")) $("searchResults").hidden = true; });

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
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ symbol }),
  });
  if (!res.ok) { const e = await res.json().catch(() => ({})); alert(e.error || "添加失败"); return; }
  await loadSymbols();
  await selectSymbol(symbol);
}

// ---------- 持仓 ----------
async function loadPositions() {
  const [positions, pnlRes] = await Promise.all([
    fetch("/api/positions").then((r) => r.json()),
    fetch("/api/pnl").then((r) => r.json()),
  ]);
  const sym = state.selected;
  const mine = positions.filter((p) => p.symbol === sym);
  const priceRow = pnlRes.rows.find((r) => r.symbol === sym);
  const lastPrice = priceRow ? priceRow.lastPrice : (state.symbols.find((s) => s.symbol === sym)?.lastPrice || 0);

  const tbody = $("posRows");
  tbody.innerHTML = "";
  $("posEmpty").hidden = mine.length > 0;
  for (const p of mine) {
    const L = lotPnl(p, lastPrice);
    const cls = signCls(L.pnl);
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${sideBadge(L.short)}</td>
      <td>${L.entryTime}${p.note ? ` <span class="flat">· ${p.note}</span>` : ""}</td>
      <td class="num">${fmtNum(p.quantity, 4)}</td>
      <td class="num">${fmtNum(L.entry)}</td>
      <td>${L.closed ? L.closeTime : '<span class="flat">持仓中</span>'}</td>
      <td class="num">${L.exit ? fmtNum(L.exit) : "—"}</td>
      <td class="num ${cls}">${signStr(L.pnl)}${fmtUsd(L.pnl)}${L.closed ? "" : ' <span class="flat" style="font-size:10px">浮</span>'}</td>
      <td class="num ${cls}">${signStr(L.pct)}${fmtNum(L.pct)}%</td>
      <td class="num pos-actions"></td>`;
    const act = tr.querySelector(".pos-actions");
    if (L.closed) {
      act.appendChild(mkBtn("pos-link", L.short ? "撤销买入" : "撤销卖出", () => undoClose(p.id)));
    } else {
      act.appendChild(mkBtn("pos-link sell", L.short ? "买入平仓" : "卖出平仓", () => openSellModal(p, lastPrice)));
    }
    act.appendChild(mkBtn("pos-del", "✕", () => deletePosition(p.id), "删除"));
    tbody.appendChild(tr);
  }

  const posPnl = $("posPnl");
  if (priceRow) {
    const cls = signCls(priceRow.unrealizedUsd);
    posPnl.className = "pos-pnl " + cls;
    posPnl.textContent = `浮动 ${signStr(priceRow.unrealizedUsd)}${fmtUsd(priceRow.unrealizedUsd)} (${fmtNum(priceRow.unrealizedPct)}%)` +
      (priceRow.realizedUsd ? ` · 已实现 ${signStr(priceRow.realizedUsd)}${fmtUsd(priceRow.realizedUsd)}` : "");
  } else posPnl.textContent = "";
  renderTotals(pnlRes.totals);
}

function mkBtn(cls, text, onClick, title) {
  const b = document.createElement("button");
  b.className = cls; b.textContent = text; if (title) b.title = title;
  b.addEventListener("click", onClick);
  return b;
}

function renderTotals(totals) {
  const usd = totals.unrealizedUsd || 0, rz = totals.realizedUsd || 0;
  const el = $("totalPnl");
  el.className = "totals-value " + signCls(usd);
  el.textContent = `${signStr(usd)}${fmtUsd(usd)}`;
  const rel = $("totalRealized");
  rel.className = "totals-value " + signCls(rz);
  rel.textContent = `${signStr(rz)}${fmtUsd(rz)}`;
}

async function loadPnl() {
  const pnlRes = await fetch("/api/pnl").then((r) => r.json());
  renderTotals(pnlRes.totals);
}

// 做多/做空切换
document.querySelectorAll("#sideToggle .side-btn").forEach((btn) => {
  btn.addEventListener("click", () => {
    document.querySelectorAll("#sideToggle .side-btn").forEach((b) => b.classList.remove("active"));
    btn.classList.add("active");
    state.entrySide = btn.dataset.side;
    const short = state.entrySide === "short";
    $("posForm").price.placeholder = short ? "卖出价 $" : "买入价 $";
    $("posSubmit").textContent = short ? "记录卖出（做空开仓）" : "记录买入";
  });
});

$("posForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  if (!state.selected) { alert("请先在左侧选择一只股票"); return; }
  const f = e.target;
  const short = state.entrySide === "short";
  const price = parseFloat(f.price.value), time = f.time.value;
  const body = { symbol: state.selected, quantity: parseFloat(f.quantity.value), side: state.entrySide, note: f.note.value };
  if (short) { body.sellPrice = price; body.sellTime = time; }
  else { body.buyPrice = price; body.buyTime = time; }
  const res = await fetch("/api/positions", {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
  });
  if (!res.ok) { const err = await res.json().catch(() => ({})); alert(err.error || "记录失败"); return; }
  f.quantity.value = ""; f.price.value = ""; f.note.value = "";
  await loadPositions();
});

async function deletePosition(id) {
  if (!confirm("删除这条记录？")) return;
  await fetch(`/api/positions/${id}`, { method: "DELETE" });
  await loadPositions();
}

// ---------- 平仓弹窗 ----------
let sellingId = null;
function openSellModal(p, lastPrice) {
  sellingId = p.id;
  const short = p.side === "short";
  const entryPrice = short ? p.sellPrice : p.buyPrice;
  const entryTime = short ? p.sellTime : p.buyTime;
  $("sellModalTitle").innerHTML = `${short ? "买入平仓" : "卖出平仓"} · <span id="sellModalSym">${p.symbol}</span>`;
  $("sellModalInfo").textContent = `${short ? "做空开仓" : "做多开仓"} ${fmtNum(p.quantity, 4)} 股 @ $${fmtNum(entryPrice)}（${entryTime}）`;
  $("sellPriceLabel").firstChild.textContent = short ? "买入平仓单价 $" : "卖出平仓单价 $";
  $("sellPriceInput").value = (lastPrice || entryPrice).toFixed(2);
  $("sellTimeInput").value = new Date().toISOString().slice(0, 10);
  $("sellModal").hidden = false;
}
function closeSellModal() { $("sellModal").hidden = true; sellingId = null; }
$("sellCancel").addEventListener("click", closeSellModal);
$("sellModal").addEventListener("click", (e) => { if (e.target.id === "sellModal") closeSellModal(); });
$("sellConfirm").addEventListener("click", async () => {
  if (sellingId == null) return;
  const price = parseFloat($("sellPriceInput").value), time = $("sellTimeInput").value;
  if (isNaN(price) || price < 0 || !time) { alert("请填写有效的平仓价与时间"); return; }
  const res = await fetch(`/api/positions/${sellingId}/close`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ price, time }),
  });
  if (!res.ok) { const e = await res.json().catch(() => ({})); alert(e.error || "平仓失败"); return; }
  closeSellModal();
  await loadPositions();
}
);
async function undoClose(id) {
  if (!confirm("撤销平仓，改回持仓？")) return;
  await fetch(`/api/positions/${id}/close`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ time: "" }) });
  await loadPositions();
}

// ---------- 策略列表（右侧） ----------
const sigBadge = { 1: ["买入", "up"], "-1": ["卖出", "down"], 0: ["观望", "flat"] };

async function loadStrategyList() {
  const sym = state.selected || "";
  state.strategies = await fetch(`/api/strategies?symbol=${encodeURIComponent(sym)}`).then((r) => r.json());
  const ul = $("stratList");
  ul.innerHTML = "";
  state.strategies.forEach((s, i) => {
    const [label, cls] = sigBadge[s.currentSignal] || sigBadge[0];
    const li = document.createElement("li");
    li.className = "strat-li" + (s.key === state.selectedStrategy ? " active" : "");
    li.innerHTML = `
      <div class="strat-li-head">
        <span class="strat-rank">${i + 1}</span>
        <span class="strat-li-name">${s.name}</span>
        <span class="sig-badge ${cls}">${label}</span>
      </div>
      <div class="strat-li-meta">胜率 ${s.trades ? fmtNum(s.winRate) + "%" : "—"} · ${s.trades} 笔 · 累计 ${signStr(s.totalReturn)}${fmtNum(s.totalReturn)}%</div>`;
    li.addEventListener("click", () => {
      state.selectedStrategy = state.selectedStrategy === s.key ? null : s.key;
      loadStrategyList();
      applyMarkers();
    });
    ul.appendChild(li);
  });
}

// ---------- 提醒 ----------
const kindLabel = { consensus_buy: "共振·买", consensus_sell: "共振·卖", golden_cross: "金叉", death_cross: "死叉", overbought: "超买", oversold: "超卖" };
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
      <div class="alert-meta"><span>${new Date(a.createdAt).toLocaleString("zh-CN")}</span>
        ${a.acknowledged ? "" : `<button class="alert-ack-btn">已读</button>`}</div>`;
    const btn = li.querySelector(".alert-ack-btn");
    if (btn) btn.addEventListener("click", async () => { await fetch(`/api/alerts/${a.id}/ack`, { method: "POST" }); loadAlerts(); });
    li.querySelector(".alert-msg").addEventListener("click", () => {
      if (state.symbols.find((s) => s.symbol === a.symbol)) { showView("trade"); selectSymbol(a.symbol); }
    });
    ul.appendChild(li);
  }
}

// ---------- 收益视图 ----------
async function loadReturns() {
  const [pnl, stats] = await Promise.all([
    fetch("/api/pnl").then((r) => r.json()),
    fetch("/api/stats").then((r) => r.json()),
  ]);
  const t = pnl.totals;
  const total = (t.unrealizedUsd || 0) + (t.realizedUsd || 0);
  setCard("rCardUnrl", t.unrealizedUsd || 0, true);
  setCard("rCardReal", t.realizedUsd || 0, true);
  setCard("rCardTotal", total, true);
  $("rCardMV").textContent = fmtUsd(t.marketValue || 0);

  const tbody = $("returnsRows");
  tbody.innerHTML = "";
  $("returnsEmpty").hidden = pnl.rows.length > 0;
  for (const r of pnl.rows) {
    const combined = r.unrealizedUsd + r.realizedUsd;
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td class="sym">${r.symbol}</td>
      <td class="num">${fmtNum(r.quantity, 4)}</td>
      <td class="num">${r.avgCost ? fmtNum(r.avgCost) : "—"}</td>
      <td class="num">${r.lastPrice ? fmtNum(r.lastPrice) : "—"}</td>
      <td class="num ${signCls(r.unrealizedUsd)}">${signStr(r.unrealizedUsd)}${fmtUsd(r.unrealizedUsd)}</td>
      <td class="num ${signCls(r.realizedUsd)}">${signStr(r.realizedUsd)}${fmtUsd(r.realizedUsd)}</td>
      <td class="num ${signCls(combined)}">${signStr(combined)}${fmtUsd(combined)}</td>`;
    tbody.appendChild(tr);
  }
  renderPeriodTable("monthlyRows", "monthlyEmpty", stats.monthly);
  renderPeriodTable("weeklyRows", "weeklyEmpty", stats.weekly);
}

function setCard(id, v, color) {
  const el = $(id);
  el.className = "card-value " + (color ? signCls(v) : "");
  el.textContent = `${signStr(v)}${fmtUsd(v)}`;
}

function renderPeriodTable(rowsId, emptyId, rows) {
  rows = rows || [];
  const tbody = $(rowsId);
  tbody.innerHTML = "";
  $(emptyId).hidden = rows.length > 0;
  for (const s of rows) {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${s.label}</td>
      <td class="num ${signCls(s.realizedUsd)}">${signStr(s.realizedUsd)}${fmtUsd(s.realizedUsd)}</td>
      <td class="num flat">${s.trades}</td>
      <td class="num">${fmtNum(s.winRate)}%</td>`;
    tbody.appendChild(tr);
  }
}

// ---------- 记录视图 ----------
let allRecords = [];
$("recFilter").addEventListener("input", renderRecords);

async function loadRecords() {
  allRecords = await fetch("/api/positions").then((r) => r.json());
  renderRecords();
}

function renderRecords() {
  const filter = $("recFilter").value.trim().toUpperCase();
  const rows = allRecords.filter((p) => !filter || p.symbol.includes(filter));
  const priceOf = {};
  state.symbols.forEach((s) => (priceOf[s.symbol] = s.lastPrice));
  const tbody = $("recordRows");
  tbody.innerHTML = "";
  $("recordEmpty").hidden = rows.length > 0;
  for (const p of rows) {
    const L = lotPnl(p, priceOf[p.symbol] || 0);
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td class="sym">${p.symbol}</td>
      <td>${sideBadge(L.short)}</td>
      <td class="num">${fmtNum(p.quantity, 4)}</td>
      <td class="num">${p.buyTime ? fmtNum(p.buyPrice) : "—"}</td>
      <td>${p.buyTime || '<span class="flat">—</span>'}</td>
      <td class="num">${p.sellTime ? fmtNum(p.sellPrice) : "—"}</td>
      <td>${p.sellTime || '<span class="flat">—</span>'}</td>
      <td>${p.note || ""}</td>
      <td class="num ${signCls(L.pnl)}">${signStr(L.pnl)}${fmtUsd(L.pnl)}${L.closed ? "" : ' <span class="flat" style="font-size:10px">浮</span>'}</td>
      <td class="rec-actions"></td>`;
    const act = tr.querySelector(".rec-actions");
    act.appendChild(mkBtn("pos-link", "编辑", () => openEdit(p)));
    act.appendChild(mkBtn("pos-del", "✕", () => deleteRecord(p.id), "删除"));
    tbody.appendChild(tr);
  }
}

async function deleteRecord(id) {
  if (!confirm("删除这条记录？盈亏统计将自动更新。")) return;
  await fetch(`/api/positions/${id}`, { method: "DELETE" });
  await loadRecords();
  await loadPnl();
}

// ---------- 编辑弹窗 ----------
let editingId = null;
function openEdit(p) {
  editingId = p.id;
  $("editSymbol").value = p.symbol;
  $("editSide").value = p.side === "short" ? "short" : "long";
  $("editQty").value = p.quantity;
  $("editBuyPrice").value = p.buyPrice;
  $("editBuyTime").value = p.buyTime ? p.buyTime.slice(0, 10) : "";
  $("editSellPrice").value = p.sellPrice || "";
  $("editSellTime").value = p.sellTime ? p.sellTime.slice(0, 10) : "";
  $("editNote").value = p.note || "";
  $("editModal").hidden = false;
}
function closeEdit() { $("editModal").hidden = true; editingId = null; }
$("editCancel").addEventListener("click", closeEdit);
$("editModal").addEventListener("click", (e) => { if (e.target.id === "editModal") closeEdit(); });
$("editConfirm").addEventListener("click", async () => {
  if (editingId == null) return;
  const body = {
    symbol: $("editSymbol").value.trim().toUpperCase(),
    side: $("editSide").value,
    quantity: parseFloat($("editQty").value),
    buyPrice: parseFloat($("editBuyPrice").value) || 0,
    buyTime: $("editBuyTime").value,
    sellPrice: parseFloat($("editSellPrice").value) || 0,
    sellTime: $("editSellTime").value,
    note: $("editNote").value,
  };
  if (!body.symbol || !(body.quantity > 0)) { alert("代码/数量不合法"); return; }
  const res = await fetch(`/api/positions/${editingId}`, {
    method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
  });
  if (!res.ok) { const e = await res.json().catch(() => ({})); alert(e.error || "保存失败"); return; }
  closeEdit();
  await loadRecords();
  await loadPnl();
}
);

// ---------- 刷新 & 轮询 ----------
$("refreshBtn").addEventListener("click", async () => { await fetch("/api/refresh", { method: "POST" }); await refreshLive(); });

async function refreshLive() {
  await loadSymbols();
  await loadPnl();
  await loadAlerts();
  if (state.selected) { updateChartHead(); await loadStrategyList(); }
  if (!$("view-returns").hidden) loadReturns();
  if (!$("view-records").hidden) loadRecords();
}

// ---------- 服务地址 ----------
async function loadServerInfo() {
  let info;
  try { info = await fetch("/api/server-info").then((r) => r.json()); } catch { return; }
  const el = $("serverAddr");
  const addrs = info.addresses || [];
  if (!addrs.length) { el.textContent = ""; return; }
  el.innerHTML = `📱 手机访问：<b>${addrs[0].url}</b>`;
  el.onclick = () => alert("可用访问地址（手机与本机在同一 WiFi 下）：\n\n" +
    addrs.map((a) => `${a.label}\n${a.url}`).join("\n\n"));
}

// 美股交易时段（美东时间）：盘前/盘中/盘后/休市
function renderMarketSession() {
  const el = $("marketSession");
  if (!el) return;
  const parts = new Intl.DateTimeFormat("en-US", { timeZone: "America/New_York", weekday: "short", hour: "2-digit", minute: "2-digit", hour12: false }).formatToParts(new Date());
  const get = (t) => parts.find((p) => p.type === t).value;
  const wd = get("weekday");
  let hh = parseInt(get("hour"), 10); if (hh === 24) hh = 0;
  const mins = hh * 60 + parseInt(get("minute"), 10);
  let label = "休市", cls = "flat";
  if (wd === "Sat" || wd === "Sun") { label = "休市·周末"; }
  else if (mins >= 240 && mins < 570) { label = "盘前"; cls = "pre"; }
  else if (mins >= 570 && mins < 960) { label = "盘中"; cls = "live"; }
  else if (mins >= 960 && mins < 1200) { label = "盘后"; cls = "post"; }
  else { label = "休市·夜间"; }
  el.className = "mkt-session " + cls;
  el.textContent = "● " + label;
}

async function loadVersion() {
  try {
    const v = await fetch("/api/version").then((r) => r.json());
    if (v.version) $("appVer").textContent = "v" + v.version;
  } catch { /* 旧版本无此接口，忽略 */ }
}

// ---------- 启动 ----------
initChart();
loadVersion();
loadServerInfo();
renderMarketSession();
setInterval(renderMarketSession, 30000);
loadSymbols().then(() => { loadPnl(); loadAlerts(); loadStats(); });
setInterval(refreshLive, 30000);
