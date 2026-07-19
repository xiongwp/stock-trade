const rowsEl = document.getElementById("rows");
const emptyEl = document.getElementById("empty");
const msgEl = document.getElementById("msg");

function showMsg(text, isError = false) {
  msgEl.textContent = text;
  msgEl.classList.toggle("error", isError);
  if (text) setTimeout(() => { if (msgEl.textContent === text) showMsg(""); }, 2500);
}

function fmt(n) {
  return Number(n).toLocaleString("zh-CN", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function render(stocks) {
  emptyEl.hidden = stocks.length > 0;
  rowsEl.innerHTML = "";
  for (const s of stocks) {
    const change = s.price - s.prevClose;
    const pct = s.prevClose > 0 ? (change / s.prevClose) * 100 : 0;
    const cls = change > 0 ? "up" : change < 0 ? "down" : "flat";
    const sign = change > 0 ? "+" : "";

    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td class="sym">${s.symbol}</td>
      <td>${s.name || "—"}</td>
      <td class="num"><input class="price-input ${cls}" type="number" step="0.01" min="0" value="${s.price.toFixed(2)}"></td>
      <td class="num flat">${fmt(s.prevClose)}</td>
      <td class="num ${cls}">${sign}${fmt(change)}</td>
      <td class="num ${cls}">${sign}${pct.toFixed(2)}%</td>
      <td class="num"><button class="del" title="删除">✕</button></td>
    `;

    tr.querySelector(".price-input").addEventListener("change", (e) => {
      updatePrice(s.id, parseFloat(e.target.value));
    });
    tr.querySelector(".del").addEventListener("click", () => removeStock(s.id, s.symbol));
    rowsEl.appendChild(tr);
  }
}

async function load() {
  const res = await fetch("/api/stocks");
  render(await res.json());
}

async function updatePrice(id, price) {
  if (isNaN(price) || price < 0) { showMsg("价格无效", true); load(); return; }
  const res = await fetch(`/api/stocks/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ price }),
  });
  if (!res.ok) showMsg("更新失败", true);
  load();
}

async function removeStock(id, symbol) {
  if (!confirm(`从自选中删除 ${symbol}？`)) return;
  await fetch(`/api/stocks/${id}`, { method: "DELETE" });
  load();
}

document.getElementById("addForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  const f = e.target;
  const body = {
    symbol: f.symbol.value,
    name: f.name.value,
    price: parseFloat(f.price.value),
  };
  const res = await fetch("/api/stocks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (res.ok) {
    f.reset();
    f.symbol.focus();
    load();
  } else {
    const err = await res.json().catch(() => ({}));
    showMsg(err.error || "添加失败", true);
  }
});

async function tick() {
  const res = await fetch("/api/tick", { method: "POST" });
  if (res.ok) render(await res.json());
}
document.getElementById("tickBtn").addEventListener("click", tick);

let autoTimer = null;
document.getElementById("autoTick").addEventListener("change", (e) => {
  if (e.target.checked) autoTimer = setInterval(tick, 3000);
  else clearInterval(autoTimer);
});

load();
