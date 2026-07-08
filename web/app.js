// beacon frontend. Renders /api/status generically: adding a provider in
// config makes a card appear with zero edits here. No dependencies, no build.

const $ = (id) => document.getElementById(id);
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

let data = null;      // last /api/status payload
let failed = false;   // last fetch failed
let query = '';
let engineIdx = 0;
let activeIndex = 0;
let fetchTimer = null;

// ── time formatting ─────────────────────────────────────────────────────────

function fmtAge(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 45) return 'just now';
  const m = Math.round(s / 60);
  if (m < 60) return m + 'm ago';
  const h = Math.floor(m / 60);
  if (h < 24) return h + 'h ago';
  return Math.floor(h / 24) + 'd ago';
}

function fmtShort(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return s + 's';
  const m = Math.round(s / 60);
  if (m < 60) return m + 'm';
  const h = Math.floor(m / 60);
  if (h < 24) return h + 'h';
  return Math.floor(h / 24) + 'd';
}

// ── status mapping ──────────────────────────────────────────────────────────

const LABEL = { ok: 'OK', warn: 'WARNING', fail: 'FAILED', down: 'OFFLINE', loading: 'LOADING' };
const hasTime = (t) => t && !t.startsWith('0001');

// Backend status -> design visual: error->fail; unknown->down (or the loading
// shimmer before the first poll); ok/warn map straight through.
function visual(e) {
  if (e.status === 'error') return 'fail';
  if (e.status === 'unknown') return hasTime(e.updatedAt) ? 'down' : 'loading';
  return e.status;
}

// Sort: error, then warn, then stale float to the top; healthy sinks.
function rank(e) {
  const base = { fail: 0, warn: 1, down: 3, loading: 3, ok: 4 }[visual(e)];
  return e.stale ? Math.min(base, 2) : base;
}

const TYPE_ICONS = {
  restic: 'shield', syncthing: 'sync', disk: 'hdd', ntfy: 'bell', adguard: 'filter',
  'http-json': 'globe', 'http-health': 'globe', command: 'terminal',
};
const iconOf = (e) => '#i-' + (e.icon || TYPE_ICONS[e.type] || 'activity');

const svgUse = (href, size, sw = 1.7) =>
  `<svg viewBox="0 0 24 24" width="${size}" height="${size}" fill="none" stroke="currentColor" stroke-width="${sw}" stroke-linecap="round" stroke-linejoin="round"><use href="${esc(href)}"></use></svg>`;

// ── metric primitives ───────────────────────────────────────────────────────

function metricHTML(label, v, now) {
  if (v !== null && typeof v === 'object' && v.t) {
    switch (v.t) {
      case 'stat':
        return `<div class="m-stat"><div class="m-eyebrow nk-mono">${esc(label)}</div>
          <div class="m-stat-row"><span class="m-stat-val nk-mono">${esc(v.value)}</span>${v.unit ? `<span class="m-stat-unit nk-mono">${esc(v.unit)}</span>` : ''}</div></div>`;
      case 'bar': {
        const max = Number(v.max) || 100;
        const pct = Math.min(100, Math.round((Number(v.value) / max) * 1000) / 10);
        let cls = '';
        if (v.critAt != null && Number(v.value) >= Number(v.critAt)) cls = 'crit';
        else if (v.warnAt != null && Number(v.value) >= Number(v.warnAt)) cls = 'warn';
        return `<div class="m-bar"><div class="m-bar-head"><span class="m-bar-label">${esc(label)}</span><span class="m-bar-val nk-mono">${esc(v.valueLabel ?? pct + '%')}</span></div>
          <div class="m-bar-track"><div class="m-bar-fill ${cls}" style="width:${pct}%"></div></div></div>`;
      }
      case 'age': {
        const age = now - Number(v.at);
        let cls = '';
        if (v.critAfterMs != null && age >= Number(v.critAfterMs)) cls = 'crit';
        else if (v.warnAfterMs != null && age >= Number(v.warnAfterMs)) cls = 'warn';
        return `<div class="m-age"><span class="m-eyebrow nk-mono">${esc(label)}</span><span class="m-age-val nk-mono ${cls}">${fmtAge(age)}</span></div>`;
      }
      case 'spark': {
        const pts = (v.points || []).map(Number);
        const mn = Math.min(...pts), mx = Math.max(...pts), rng = (mx - mn) || 1;
        const pstr = pts.map((p, i) => {
          const x = pts.length > 1 ? (i / (pts.length - 1)) * 100 : 0;
          const y = 26 - ((p - mn) / rng) * 22;
          return x.toFixed(1) + ',' + y.toFixed(1);
        }).join(' ');
        return `<div class="m-spark"><div class="m-spark-head"><span class="m-eyebrow nk-mono">${esc(label)}</span><span class="m-spark-val nk-mono">${esc(v.value ?? '')}${esc(v.unit ?? '')}</span></div>
          <svg viewBox="0 0 100 28" preserveAspectRatio="none"><polyline points="${pstr}" fill="none" stroke="var(--nk-data-3)" stroke-width="1.6" vector-effect="non-scaling-stroke" stroke-linecap="round" stroke-linejoin="round"></polyline></svg></div>`;
      }
      case 'pill':
        return `<div class="m-pill"><span class="m-kv-label">${esc(label)}</span><span class="m-pill-badge nk-mono ${esc(v.kind ?? 'idle')}">${esc(v.value)}</span></div>`;
      default:
        v = v.value ?? JSON.stringify(v);
    }
  }
  return `<div class="m-kv"><span class="m-kv-label">${esc(label)}</span><span class="m-kv-val nk-mono">${esc(v)}</span></div>`;
}

function feedHTML(items, now) {
  const sev = (p) => (p >= 5 ? 'crit' : p === 4 ? 'warn' : 'info');
  const rows = items.map((it) => `<div class="feed-row">
    <span class="feed-dot ${sev(it.priority ?? 3)}"></span>
    <span class="feed-text" title="${esc(it.message ?? '')}">${esc(it.title || it.message || '')}</span>
    <span class="feed-time nk-mono">${fmtAge(now - Date.parse(it.time))}</span></div>`).join('');
  return `<div class="feed"><div class="m-eyebrow nk-mono">latest</div>${rows}</div>`;
}

// ── card ────────────────────────────────────────────────────────────────────

function cardHTML(e, now) {
  const v = visual(e);
  const stale = e.stale && v !== 'loading';
  const chip = stale && v === 'ok'
    ? '<span class="card-status st-stale nk-mono">STALE</span>'
    : `<span class="card-status st-${v} nk-mono">${LABEL[v]}</span>`;

  let body = '';
  if (v === 'loading') {
    body = `<div class="shim-rows"><div class="shim" style="height:30px;width:52%"></div>
      <div class="shim" style="height:13px;width:100%"></div><div class="shim" style="height:13px;width:78%"></div></div>`;
  } else if (v === 'down') {
    body = `<div class="down-block">${svgUse('#i-down', 27, 1.6)}
      <div class="down-title">Known down</div>
      <div class="down-text nk-mono">${esc(e.error || 'host unreachable')}</div></div>`;
  } else {
    const alert = v === 'fail' && e.error
      ? `<div class="card-alert">${svgUse('#i-alert', 15, 1.8)}<span>${esc(e.error.split('\n')[0])}</span></div>` : '';
    const summary = e.summary && e.summary !== e.error
      ? `<div class="card-summary">${esc(e.summary)}</div>` : '';
    const metrics = Object.entries(e.metrics || {}).sort(([a], [b]) => a.localeCompare(b))
      .map(([label, val]) => metricHTML(label, val, now)).join('');
    const feed = e.items && e.items.length ? feedHTML(e.items, now) : '';
    body = alert + summary + (metrics || feed ? `<div class="metrics">${metrics}${feed}</div>` : '');
  }

  const links = e.url && e.url !== '#'
    ? `<div class="card-links"><a href="${esc(e.url)}" target="_blank" rel="noreferrer"><span>Open</span>${svgUse('#i-external', 12, 1.8)}</a></div>` : '';
  let updated;
  if (v === 'loading') updated = 'refreshing…';
  else if (v === 'down') updated = 'last seen ' + fmtAge(now - Date.parse(e.updatedAt));
  else {
    updated = 'updated ' + fmtAge(now - Date.parse(e.updatedAt));
    if (e.intervalSeconds) updated += ' · every ' + fmtShort(e.intervalSeconds * 1000);
    if (stale) updated += ' · <span class="stale-word">stale</span>';
  }

  return `<article class="card v-${v}${stale ? ' is-stale' : ''}">
    <div class="card-bar"></div>
    <div class="card-body">
      <div class="card-head">
        <div class="card-id">
          <span class="card-ico">${svgUse(iconOf(e), 17)}</span>
          <div class="card-titles">
            <div class="card-title">${esc(e.title)}</div>
            <div class="card-sub nk-mono">${esc(e.subtitle || e.type || '')}</div>
          </div>
        </div>
        ${chip}
      </div>
      ${body}
      <div class="card-foot">${links}<div class="card-updated nk-mono">${updated}</div></div>
    </div>
  </article>`;
}

// ── page sections ───────────────────────────────────────────────────────────

function render() {
  const now = Date.now();
  const es = (data && data.providers) || [];

  // verdict
  const nFail = es.filter((e) => visual(e) === 'fail').length;
  const nWarn = es.filter((e) => visual(e) === 'warn').length;
  const nStale = es.filter((e) => e.stale && visual(e) !== 'fail' && visual(e) !== 'warn').length;
  let cls, label, detail;
  if (!data) {
    cls = 'v-none'; label = 'no data'; detail = failed ? 'server unreachable' : '';
  } else if (nFail) {
    cls = 'v-fail'; label = 'Attention needed';
    detail = [`${nFail} failed`, nWarn && `${nWarn} warning${nWarn === 1 ? '' : 's'}`, nStale && `${nStale} stale`].filter(Boolean).join(' · ');
  } else if (nWarn + nStale) {
    cls = 'v-warn'; label = 'Check soon';
    detail = [nWarn && `${nWarn} warning${nWarn === 1 ? '' : 's'}`, nStale && `${nStale} stale`].filter(Boolean).join(' · ');
  } else {
    cls = 'v-ok'; label = 'All clear'; detail = `${es.length} services healthy`;
  }
  $('verdict-bar').className = cls === 'v-ok' ? '' : cls;
  $('verdict').className = 'verdict ' + cls;
  $('verdict-label').textContent = label;
  $('verdict-detail').textContent = detail;
  $('host').textContent = (data && data.hostLabel) || '';
  $('banner').hidden = !failed;

  // health strip pills
  $('pills').innerHTML = es.map((e) => {
    const v = visual(e);
    const dot = e.stale && v === 'ok' ? 'd-stale' : 'd-' + v;
    const val = v === 'loading' ? '…'
      : e.stale ? fmtShort(now - Date.parse(e.updatedAt)) + ' stale'
      : fmtShort(now - Date.parse(e.updatedAt));
    const tag = e.url && e.url !== '#' ? `a href="${esc(e.url)}" target="_blank" rel="noreferrer"` : 'span';
    return `<${tag} class="pill"><span class="dot ${dot}"></span><span class="pill-title nk-mono">${esc(e.title)}</span><span class="pill-val nk-mono">${esc(val)}</span></${tag.split(' ')[0]}>`;
  }).join('');

  // quick launch
  const links = (data && data.links) || [];
  $('quick').hidden = !links.length;
  $('quick-links').innerHTML = links.map((l) =>
    `<a href="${esc(l.url)}" target="_blank" rel="noreferrer" title="${esc(l.label)}">${svgUse('#i-' + (l.icon || 'external'), 16)}<span>${esc(l.label)}</span></a>`).join('');

  // card grid (stable sort keeps config order within a rank)
  $('grid').innerHTML = [...es].sort((a, b) => rank(a) - rank(b)).map((e) => cardHTML(e, now)).join('');

  renderEngines();
  renderResults();
}

function renderEngines() {
  const engines = (data && data.engines) || [];
  $('engines').innerHTML = engines.map((en, i) =>
    `<button class="eng${i === engineIdx ? ' on' : ''}" data-i="${i}">${esc(en.name)}</button>`).join('');
}

// ── command bar ─────────────────────────────────────────────────────────────

function searchRows() {
  const q = query.trim().toLowerCase();
  if (!q) return [];
  const es = ((data && data.providers) || [])
    .filter((e) => [e.id, e.title, e.subtitle, e.type].some((x) => (x || '').toLowerCase().includes(q)))
    .slice(0, 5)
    .map((e) => ({ title: e.title, sub: e.subtitle || e.type, icon: iconOf(e), url: e.url }));
  const ls = ((data && data.links) || [])
    .filter((l) => l.label.toLowerCase().includes(q))
    .slice(0, 3)
    .map((l) => ({ title: l.label, sub: 'link', icon: '#i-' + (l.icon || 'external'), url: l.url }));
  const rows = [...es, ...ls];
  if (((data && data.engines) || []).length) rows.push({ search: true });
  return rows;
}

function renderResults() {
  const rows = searchRows();
  const box = $('results');
  box.hidden = !rows.length;
  if (!rows.length) return;
  const en = (data.engines || [])[engineIdx];
  box.innerHTML = rows.map((r, i) => r.search
    ? `<div class="result-row${i === activeIndex ? ' active' : ''}" data-i="${i}">
        <span class="result-ico search">${svgUse('#i-search', 15)}</span>
        <div class="result-label">Search ${esc(en ? en.name : '')} for “${esc(query.trim())}”</div>
        <span class="result-hint nk-mono">Search ↵</span></div>`
    : `<div class="result-row${i === activeIndex ? ' active' : ''}" data-i="${i}">
        <span class="result-ico">${svgUse(r.icon, 15)}</span>
        <div class="result-main"><div class="result-title">${esc(r.title)}</div><div class="result-sub nk-mono">${esc(r.sub)}</div></div>
        <span class="result-hint nk-mono">Open ↵</span></div>`).join('');
}

function activate(i) {
  const rows = searchRows();
  const r = rows[i];
  if (!r) return;
  if (r.search) {
    const en = (data.engines || [])[engineIdx];
    if (en) window.open(en.url + encodeURIComponent(query.trim()), '_blank', 'noopener');
  } else if (r.url && r.url !== '#') {
    window.open(r.url, '_blank', 'noopener');
  }
}

const input = $('q');
input.addEventListener('input', () => { query = input.value; activeIndex = 0; renderResults(); });
input.addEventListener('keydown', (e) => {
  const len = searchRows().length;
  if (e.key === 'ArrowDown') { e.preventDefault(); activeIndex = Math.min(len - 1, activeIndex + 1); renderResults(); }
  else if (e.key === 'ArrowUp') { e.preventDefault(); activeIndex = Math.max(0, activeIndex - 1); renderResults(); }
  else if (e.key === 'Enter') { e.preventDefault(); activate(activeIndex); }
  else if (e.key === 'Escape') { query = input.value = ''; activeIndex = 0; renderResults(); }
});
$('results').addEventListener('click', (e) => {
  const row = e.target.closest('.result-row');
  if (row) activate(Number(row.dataset.i));
});
$('results').addEventListener('mouseover', (e) => {
  const row = e.target.closest('.result-row');
  if (row && Number(row.dataset.i) !== activeIndex) { activeIndex = Number(row.dataset.i); renderResults(); }
});
$('engines').addEventListener('click', (e) => {
  const btn = e.target.closest('.eng');
  if (btn) { engineIdx = Number(btn.dataset.i); renderEngines(); renderResults(); input.focus(); }
});
document.addEventListener('keydown', (e) => {
  const typing = /INPUT|TEXTAREA/.test(e.target.tagName || '');
  if ((e.key === '/' && !typing) || ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k')) {
    e.preventDefault();
    input.focus();
  }
});

// ── theme ───────────────────────────────────────────────────────────────────

const root = $('root');
let theme = (() => {
  try {
    const t = localStorage.getItem('beacon-theme');
    if (t === 'dark' || t === 'light') return t;
  } catch { /* private mode */ }
  return matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
})();
function applyTheme() {
  root.dataset.theme = theme;
  $('theme-icon').setAttribute('href', theme === 'dark' ? '#i-sun' : '#i-moon');
}
$('theme-btn').addEventListener('click', () => {
  theme = theme === 'dark' ? 'light' : 'dark';
  try { localStorage.setItem('beacon-theme', theme); } catch { /* ignore */ }
  applyTheme();
});
applyTheme();

// ── fetch loop ──────────────────────────────────────────────────────────────

async function refresh() {
  try {
    const r = await fetch('/api/status', { cache: 'no-store' });
    if (!r.ok) throw new Error('HTTP ' + r.status);
    data = await r.json();
    failed = false;
    try { localStorage.setItem('beacon-data', JSON.stringify(data)); } catch { /* ignore */ }
  } catch {
    failed = true;
  }
  render();
  clearTimeout(fetchTimer);
  fetchTimer = setTimeout(refresh, ((data && data.refreshSeconds) || 30) * 1000);
}

// render instantly from the last known payload, then fetch fresh
try { data = JSON.parse(localStorage.getItem('beacon-data')); } catch { /* ignore */ }
if (data) render();
refresh();
setInterval(() => { if (data && !document.hidden) render(); }, 30000); // keep ages ticking
input.focus();
