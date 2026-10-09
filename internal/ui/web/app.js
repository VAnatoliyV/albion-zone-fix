// Albion Journal для Windows — страница окна. Данные — с локального
// сервера программы (/api/...), раз в две секунды.
'use strict';

const $ = id => document.getElementById(id);
const TOKEN = document.querySelector('meta[name="aj-token"]').content;
const ICONS = 'https://vanatoliyv.github.io/albion-craft-profit/ico/'; // копия на GitHub: render в РФ заблокирован
// innerHTML только если что-то поменялось: иначе каждые две секунды
// пересоздаются картинки предметов и мигают.
function setHTML(el, html) { if (el._html !== html) { el._html = html; el.innerHTML = html; } }
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

let T = {};          // словарь текущего языка
let lang = '';       // ru / en / es / pl / de / tr / fr / pt / it
let S = null;        // последнее /api/state
let tab = 'own';
try { tab = localStorage.getItem('tab') || 'own'; } catch (e) {}
let copied = false;

// --- язык -------------------------------------------------------------------

function t(key, ...args) {
  let s = T[key];
  if (s === undefined) return key;
  for (const a of args) s = s.replace('%s', a);
  return s;
}

async function loadLang(l) {
  const r = await fetch('/api/i18n?lang=' + encodeURIComponent(l || ''));
  const j = await r.json();
  T = j.t; lang = j.lang;
  document.documentElement.lang = lang;
  document.querySelectorAll('[data-t]').forEach(el => { el.textContent = t(el.dataset.t); });
  document.querySelectorAll('[data-title]').forEach(el => { el.title = t(el.dataset.title); });
}

// Числа — с узким неразрывным пробелом между разрядами, как у мака.
const fmt = n => {
  const v = Math.trunc(Number(n) || 0);
  const s = String(Math.abs(v)).replace(/\B(?=(\d{3})+(?!\d))/g, ' ');
  return (v < 0 ? '-' : '') + s;
};

function plural(n, forms) {
  if (lang === 'ru' && forms.length >= 3) {
    const a = n % 100, b = n % 10;
    if (a >= 11 && a <= 14) return forms[2];
    if (b === 1) return forms[0];
    if (b >= 2 && b <= 4) return forms[1];
    return forms[2];
  }
  if (lang === 'pl' && forms.length >= 3) {
    // Польский: 1 minutę, 2–4 minuty (кроме 12–14), остальное — minut.
    const a = n % 100, b = n % 10;
    if (n === 1) return forms[0];
    if (b >= 2 && b <= 4 && !(a >= 12 && a <= 14)) return forms[1];
    return forms[2];
  }
  return n === 1 ? forms[0] : forms[1];
}

function ago(ts) {
  if (!ts) return '—';
  const d = Math.floor(Date.now() / 1000 - ts);
  if (d < 10) return t('ago.now');
  if (d < 60) return t('ago.sec', d);
  const unit = (n, key) => t('ago.wrap', n + ' ' + plural(n, t(key).split('|')));
  const m = Math.floor(d / 60);
  if (m < 60) return unit(m, 'ago.min');
  const h = Math.floor(m / 60);
  if (h < 24) return unit(h, 'ago.hour');
  return unit(Math.floor(h / 24), 'ago.day');
}

const locale = () => ({ ru: 'ru-RU', en: 'en-GB', es: 'es-ES', pl: 'pl-PL', de: 'de-DE', tr: 'tr-TR', fr: 'fr-FR', pt: 'pt-BR', it: 'it-IT' }[lang] || 'en-GB');
const clock = iso => new Date(iso).toLocaleTimeString(locale());

// --- запросы ----------------------------------------------------------------

async function post(url, data, json) {
  const opt = { method: 'POST', headers: { 'X-AJ-Token': TOKEN } };
  if (json) { opt.body = JSON.stringify(data); opt.headers['Content-Type'] = 'application/json'; }
  else opt.body = new URLSearchParams(data || {});
  const r = await fetch(url, opt);
  const j = await r.json().catch(() => ({}));
  if (!r.ok && !json) alert(j.error || t('err.generic'));
  return { ok: r.ok, j };
}

const getJSON = async url => (await fetch(url)).json();

// --- вкладки ----------------------------------------------------------------

function showTab(name) {
  tab = name;
  try { localStorage.setItem('tab', name); } catch (e) {}
  document.querySelectorAll('#tabs button').forEach(b => b.classList.toggle('on', b.dataset.tab === name));
  document.querySelectorAll('section.tab').forEach(s => { s.hidden = s.id !== 'tab-' + name; });
  refreshTab();
}

$('tabs').addEventListener('click', e => { const b = e.target.closest('button'); if (b) showTab(b.dataset.tab); });

function refreshTab() {
  if (tab === 'own') refreshOwn();
  else if (tab === 'shared') refreshShared(false);
  else if (tab === 'session') refreshSession();
  else if (tab === 'zonefix') renderZoneFix();
  else if (tab === 'zone') renderZone();
}

// --- строки состояния ---------------------------------------------------------

function setRow(id, up, detail) {
  const r = $(id);
  r.classList.toggle('up', !!up);
  r.querySelector('.sdetail').textContent = detail;
}

function setBtn(b, label, primary) {
  b.textContent = label;
  b.classList.toggle('ghost', !primary);
}

function renderStatus() {
  const s = S;
  setRow('rowReceiver', s.receiver, t(s.receiver ? 'st.on' : 'st.off'));
  setBtn($('btnReceiver'), t(s.receiver ? 'btn.stop' : 'btn.start'), !s.receiver);
  $('rcvErr').hidden = !s.receiverError || s.receiver;
  $('rcvErr').textContent = s.receiverError ? t('st.rcvError', s.receiverError) : '';

  // Авалон (зона, проходы, карта) следит всегда, пока работает перехват, —
  // отдельно от сбора цен и счётчика: они включаются сами по себе.
  // Зелёным — только когда пакеты игры уже идут.
  const watching = !s.sniffError && s.packets > 0;
  setRow('rowAvalon', watching, s.sniffError ? t('st.avalonNoCapture') : !watching ? t('st.waitingGame')
    : t(s.settings.mapSend ? 'st.watching' : 'st.watchingNoMap'));

  // Сбор цен выключен — «выключен» и кнопка «Включить»; включён, но
  // разборщик не поднялся — «остановлен».
  const collecting = s.collecting && s.collector.running;
  setRow('rowCollect', collecting, t(collecting ? 'st.on' : s.collecting ? 'st.off' : 'st.disabled'));
  setBtn($('btnCollect'), t(s.collecting ? 'btn.stop' : 'btn.enable'), !s.collecting);

  setRow('rowSite', s.siteReady, !s.receiver ? t('st.off') : t(s.siteReady ? 'st.ready' : 'st.loading'));
  $('btnSite').disabled = !s.receiver;
  $('btnSite').title = s.receiver ? '' : t('st.needRecv');

  const fame = s.collector.running && s.collector.session;
  setRow('rowFame', fame, t(fame ? 'st.on' : s.settings.sessionStats ? 'st.off' : 'st.disabled'));
  setBtn($('btnFame'), t(s.settings.sessionStats ? 'btn.stop' : 'btn.enable'), !s.settings.sessionStats);
  setBtn($('btnFame2'), t(s.settings.sessionStats ? 'btn.stop' : 'btn.startFame'), !s.settings.sessionStats);

  $('sniffErr').hidden = !s.sniffError;
  $('sniffErr').textContent = s.sniffError ? t('st.sniffError', s.sniffError) : '';
  const p = s.collector.panics || 0;
  $('panics').hidden = !p;
  $('panics').textContent = p ? t('st.panics', fmt(p)) : '';
}

// «Включить» запоминается (как у счётчика): сбор начнётся и при следующем
// запуске. «Остановить» — до выхода.
$('btnCollect').onclick = async () => {
  await post('/api/collect', S && S.collecting ? { on: '0' } : { on: '1', save: '1' });
  refresh();
};
$('btnReceiver').onclick = async () => { await post('/api/receiver', { on: S && S.receiver ? '0' : '1' }); refresh(); };
$('btnSite').onclick = () => post('/api/open', { what: 'site' });
$('btnAvalonMap').onclick = () => post('/api/open', { what: 'map' });
const toggleFame = async () => { if (S) { await saveSettings({ sessionStats: !S.settings.sessionStats }); refresh(); } };
$('btnFame').onclick = toggleFame;
$('btnFame2').onclick = toggleFame;

async function copyText(text) {
  try { await navigator.clipboard.writeText(text); }
  catch (e) {
    const ta = document.createElement('textarea'); ta.value = text; document.body.appendChild(ta); ta.select();
    document.execCommand('copy'); ta.remove();
  }
}

// --- «Мои цены» ---------------------------------------------------------------

function icon(id, big) {
  if (!id) return '';
  return `<span class="ico${big ? ' big' : ''}"><img loading="lazy" alt="" src="${ICONS + encodeURIComponent(id)}.webp" onerror="this.remove()"></span>`;
}

let ownLoaded = false;
async function refreshOwn() {
  let o;
  try { o = await getJSON('/api/own'); } catch (e) { return; }
  ownLoaded = true;
  $('ownPositions').textContent = fmt(o.positions);
  $('ownOrders').textContent = fmt(o.orders);
  $('ownCities').textContent = fmt(o.cities);
  $('ownLast').textContent = o.error ? o.error : (o.exists ? t('own.lastPrice', ago(o.lastTs)) : t('own.noDb'));
  const list = o.recent || [];
  $('ownList').hidden = !list.length;
  $('ownEmpty').hidden = !!list.length;
  if (!list.length) {
    const up = S && S.collecting && S.collector.running;
    $('ownEmpty').textContent = !ownLoaded ? t('own.wait')
      : (S && !S.receiver && !o.exists ? t('own.noReceiver') : t(up ? 'own.hintUp' : 'own.hintDown'));
    return;
  }
  setHTML($('ownList'), list.map(it => {
    const where = it.quality ? t('item.quality', esc(it.city), esc(it.quality)) : esc(it.city);
    const sell = it.sell != null ? `<span class="green">${t('item.buy', fmt(it.sell))}</span>` : '';
    const buy = it.buy != null ? `<span class="gold">${t('item.sell', fmt(it.buy))}</span>` : '';
    return `<div class="item">${icon(it.name)}<div class="ibody"><div class="itop"><span class="iname">${esc(it.name)}</span><span class="iwhere">${where}</span></div><div class="iprices">${sell}${buy}</div></div></div>`;
  }).join(''));
}

// --- «Общие» ------------------------------------------------------------------

let sharedAt = 0;
async function refreshShared(force) {
  if (!force && sharedAt && Date.now() - sharedAt < 60000) return renderShared();
  if (!sharedAt) { $('sharedMsg').textContent = t('sh.loading'); $('sharedMsg').hidden = false; }
  try { lastShared = await getJSON('/api/shared' + (force ? '?force=1' : '')); } catch (e) { return; }
  sharedAt = Date.now();
  renderShared();
}
let lastShared = null;
function renderShared() {
  const s = lastShared;
  if (!s) return;
  const ok = s.loadedAt && !s.failed;
  $('sharedBody').hidden = !ok;
  $('sharedMsg').hidden = ok;
  $('sharedMsg').classList.toggle('bad', !!s.failed);
  if (!ok) { $('sharedMsg').textContent = t(s.failed ? 'sh.failed' : 'sh.loading'); return; }
  $('shItems').textContent = fmt(s.items);
  $('shBm').textContent = fmt(s.blackMarket);
  $('shCities').textContent = fmt(s.cities);
  $('shSnapshot').textContent = t('sh.snapshot', ago(s.snapshotTs));
  $('shWhat2').textContent = t('sh.what2', fmt(s.resources));
  $('shFrom').textContent = t(s.fromLocal ? 'sh.local' : 'sh.remote');
}

// --- «Сессия» -----------------------------------------------------------------

let lastSession = null;
async function refreshSession() {
  try { lastSession = await getJSON('/api/session'); } catch (e) { return; }
  renderSession();
}

function renderSession() {
  const r = lastSession;
  if (!r) return;
  const f = r.exists ? r.data : null;
  const list = (f && f.fighters) || [];
  const total = list.reduce((a, x) => a + (x.damage || 0), 0);
  $('seBody').hidden = !f;
  if (f) {
    $('seFame').textContent = fmt(f.fame);
    $('seRespec').textContent = f.respec > 0 ? t('se.respec') + ' ' + fmt(f.respec) : '';
    $('seLooted').textContent = fmt(f.silverLooted);
    $('seWallet').textContent = fmt(f.silverEarned);
    $('seCity').textContent = f.silverCity > 0 ? t('se.inCity') + ' ' + fmt(f.silverCity) : '';
    $('seDamage').textContent = fmt(total);
    const hours = Math.max(60, (f.updatedAt || 0) - (f.startedAt || 0)) / 3600;
    const parts = [t('se.lastEvent', ago(f.updatedAt)), t('se.famePerHour', fmt((f.fame || 0) / hours))];
    if (f.might > 0) parts.push(t('se.might', fmt(f.might)));
    if (f.silverBalance > 0) parts.push(t('se.balance', fmt(f.silverBalance)));
    $('seSummary').textContent = parts.join('  ·  ');
    $('seUnknown').hidden = !(f.unknownHits > 0);
    if (f.unknownHits > 0) $('seUnknown').textContent = '⚠ ' + t('se.unknown', fmt(f.unknownHits), fmt(f.unknownSources));
  }
  $('seList').hidden = !list.length;
  const msg = !f ? t('se.never') : (!list.length ? t(r.running ? 'se.noDamageUp' : 'se.noDamageDown') : '');
  $('seMsg').hidden = !msg;
  $('seMsg').textContent = msg;
  if (list.length) {
    setHTML($('seList'), list.map(x => {
      const share = total > 0 ? x.damage / total : 0;
      const dps = (x.damage || 0) / Math.max(1, (x.lastAt || 0) - (x.firstAt || 0));
      const heal = x.heal > 0 ? `<div class="heal">${t('se.heal', fmt(x.heal))}</div>` : '';
      return `<div class="item fighter">${icon(x.weapon, true)}<div class="ibody"><div class="itop"><span class="iname">${esc(x.name)}</span><span class="share">${Math.floor(share * 100)}%</span><span class="dmg">${fmt(x.damage)}</span><span class="dps">${t('se.perSec', fmt(dps))}</span></div><div class="bar"><i style="width:${(share * 100).toFixed(1)}%"></i></div>${heal}</div></div>`;
    }).join(''));
  }
  $('btnReset').hidden = !f;
  $('btnCopy').hidden = !f;
  $('btnCopy').disabled = !list.length;
  $('btnCopy').textContent = t(copied ? 'btn.copied' : 'btn.copy');
}

$('btnReset').onclick = async () => { await post('/api/session/reset'); refreshSession(); };
$('btnCopy').onclick = async () => {
  const f = lastSession && lastSession.data;
  const list = (f && f.fighters) || [];
  const total = list.reduce((a, x) => a + x.damage, 0);
  if (!total) return;
  const lines = list.map(x => `${x.name} ${Math.floor(x.damage / total * 100)}% — ${fmt(x.damage)}`);
  if (f.fame > 0) lines.push(t('se.copyTotals', fmt(f.fame), fmt(f.silverEarned)));
  await copyText(lines.join('\n'));
  copied = true; renderSession();
  setTimeout(() => { copied = false; renderSession(); }, 1600);
};

// --- «Переходы» (Zone Fix) ----------------------------------------------------

let stratsFilled = false;
const bypName = s => esc(!s || s === 'off' ? t('zf.no') : s);

function zoneRows(list) {
  if (!list || !list.length) return `<tr><td colspan="5" class="muted">${t('zf.noData')}</td></tr>`;
  return list.slice(0, 15).map(z => `<tr><td>${esc(z.name)}</td><td class="num">${z.count}</td><td class="num ${z.fails ? 'red' : ''}">${z.fails}</td><td class="num">${z.avgLoad}${z.avgAlive ? ` + ${z.avgAlive}` : ''}</td><td class="num">${z.maxLoad}</td></tr>`).join('');
}

function renderZoneFix() {
  const s = S;
  if (!s) return;
  $('zfGame').textContent = s.zone ? s.zone : (s.packets ? t('zf.gameNoZone') : t('zf.waitGame'));
  $('zfGameSub').textContent = s.packets ? t('zf.packets', fmt(s.packets), s.lastPacket) : t('zf.noPackets');

  if (!stratsFilled && s.strategies) {
    setHTML($('zfStrat'), s.strategies.map(n => `<option>${esc(n)}</option>`).join(''));
    stratsFilled = true;
  }
  const allowed = s.settings.builtinBypass;
  const on = s.bypass && s.bypass !== 'off';
  setHTML($('zfByp'), on ? `<span class="green">${t('zf.bypassOn')}</span>: ${esc(s.bypass)}` : esc(t('zf.bypassOff')));
  $('zfBypDisabled').hidden = allowed;
  for (const id of ['zfStrat', 'zfApply', 'zfAuto']) $(id).disabled = !allowed;
  $('zfOff').disabled = !on;
  $('zfAutoNote').textContent = s.auto || '';
  $('zfBypErr').hidden = !s.bypassError;
  $('zfBypErr').textContent = s.bypassError || '';

  setHTML($('zfRecent'), (s.recent && s.recent.length) ? s.recent.map(tr => {
    const res = tr.ok ? `<span class="green">${t('zf.ok')}</span>` : `<span class="red">${esc(tr.fail || t('zf.fail'))}</span>`;
    const load = tr.ok ? tr.loadSec : `<span class="red">${tr.loadSec}</span>`;
    const alive = tr.aliveSec < 0 ? '—' : tr.aliveSec;
    const srv = esc((tr.server || '').replace(':5056', ''));
    const replied = tr.replied
      ? `<span class="green">${t('zf.yes')}</span>${tr.replySec >= 0 ? ' ' + t('zf.after', tr.replySec) : ''} <span class="muted">${srv}</span>`
      : `<span class="red">${t('zf.no')}</span> <span class="muted">${srv}</span>`;
    return `<tr><td>${clock(tr.t)}</td><td>${esc(tr.fromName || tr.from || '—')}</td><td>${esc(tr.toName || tr.to || '—')}</td><td class="num">${load}</td><td class="num">${alive}</td><td>${replied}</td><td>${res}</td><td class="muted">${bypName(tr.strategy)}</td></tr>`;
  }).join('') : `<tr><td colspan="8" class="muted">${t('zf.noTransitions')}</td></tr>`);

  setHTML($('zfWorstOff'), zoneRows(s.worstOff));
  setHTML($('zfWorstOn'), zoneRows(s.worstOn));

  $('zfProbe').disabled = !!s.probing;
  $('zfProbeNote').textContent = s.probing ? t('zf.probing') : '';
  setHTML($('zfProbes'), (s.probeRuns && s.probeRuns.length) ? s.probeRuns.map(r => {
    const silent = (r.results || []).filter(x => !x.ok).map(x => x.name.replace('win-', '')).join(' ');
    return `<tr><td>${clock(r.t)}</td><td>${bypName(r.strategy)}</td><td class="num">${t('zf.of', r.ok, r.total)}</td><td class="muted wrap">${esc(silent)}</td></tr>`;
  }).join('') : `<tr><td colspan="4" class="muted">${t('zf.noProbes')}</td></tr>`);

  $('zfTrace').disabled = !!s.tracing;
  $('zfTraceNote').textContent = s.tracing ? t('zf.tracing', s.tracing.replace(':5056', '')) : '';
  setHTML($('zfTraces'), (s.traces || []).map(r => {
    const head = `<div class="trace-head"><b>${esc(r.server.replace(':5056', ''))}</b> <span class="muted">— ${esc(r.why)}, ${clock(r.t)}, ${t('zf.traceBypass', bypName(r.strategy))}</span> ${r.reached ? `<span class="green">${t('zf.reached')}</span>` : `<span class="red">${t('zf.notReached')}</span>`}</div>`;
    if (r.error) return head + `<div class="alert">${esc(r.error)}</div>`;
    const rows = (r.hops || []).map(h => `<tr><td class="num">${h.ttl}</td><td>${h.ip ? esc(h.ip) : `<span class="muted">${t('zf.hopSilent')}</span>`}</td><td class="muted">${esc(h.name || '')}</td><td class="num">${h.ms >= 0 ? t('zf.ms', h.ms) : ''}</td><td>${h.server ? `<span class="green">${t('zf.gameServer')}</span>` : ''}</td></tr>`).join('');
    return head + `<div class="scrollx"><table><thead><tr><th class="num">#</th><th>${t('zf.colHop')}</th><th>${t('zf.colName')}</th><th class="num">${t('zf.colDelay')}</th><th></th></tr></thead><tbody>${rows}</tbody></table></div>`;
  }).join(''));

  const rec = s.recording || '';
  $('zfRec').textContent = t(rec ? 'zf.recStop' : 'zf.rec');
  $('zfRec').classList.toggle('ghost', !rec);
  $('zfRecNote').textContent = rec ? t('zf.recNote', rec.split(/[\\/]/).pop(), Math.max(0, Math.ceil(s.recLeft / 60))) : '';
}

const zfPost = async (url, data) => { await post(url, data); refresh(); };
$('zfOff').onclick = () => zfPost('/api/bypass', { mode: 'off' });
$('zfApply').onclick = () => zfPost('/api/bypass', { mode: $('zfStrat').value });
$('zfAuto').onclick = () => zfPost('/api/bypass', { mode: 'auto' });
$('zfProbe').onclick = () => zfPost('/api/probe');
$('zfTrace').onclick = () => zfPost('/api/trace');
$('zfRec').onclick = () => zfPost('/api/record', S && S.recording ? { stop: 1 } : {});
$('zfOpenSettings').onclick = () => openSettings();

// --- «Зона» и карта Авалона -------------------------------------------------

const REGION = { europe: 'rg.europe', americas: 'rg.americas', asia: 'rg.asia' };
const KIND = {
  roads: 'kind.roads', black: 'kind.black', red: 'kind.red', yellow: 'kind.yellow', safe: 'kind.safe',
  city: 'kind.city', island: 'kind.island', instance: 'kind.instance', other: 'kind.other',
};
const minutesTo = iso => Math.max(1, Math.ceil((new Date(iso) - Date.now()) / 60000));

// Итог отправки прохода — строка и цвет.
function mapResult(m) {
  const retry = m.retryAt && new Date(m.retryAt) > Date.now() ? ' · ' + t('map.retry', minutesTo(m.retryAt)) : '';
  switch (m.result) {
    case 'ok': return [t('map.ok'), 'green'];
    case 'sending': return [t('map.sending'), 'muted'];
    case 'limit': return [t('map.limit') + retry, 'gold'];
    case 'iplive': return [t('map.ipLive'), 'gold'];
    case 'region': return [t('map.notEurope'), 'gold'];
    case 'offline': case 'paused': return [t('map.offline') + retry, 'red'];
    default: return [t('map.refused', m.why || m.result), 'red'];
  }
}

function renderZone() {
  const s = S;
  if (!s) return;
  renderCard();
  renderLegend();
  const h = s.here;
  if (!h) {
    $('znName').textContent = '—';
    $('znSub').textContent = s.packets ? t('zn.waitJoin') : t('zn.wait');
    $('znRegion').hidden = true;
  } else if (!h.code) {
    $('znName').textContent = t('zn.lost');
    $('znSub').textContent = t('zn.lostSub') + '  ·  ' + t('zn.since', ago(Date.parse(h.since) / 1000));
    $('znRegion').hidden = true;
  } else {
    $('znName').textContent = h.name || h.code;
    const parts = [h.code];
    if (h.kind && KIND[h.kind]) parts.push(t(KIND[h.kind]));
    if (h.tier) parts.push(t('zn.tier', h.tier));
    if (REGION[h.region]) parts.push(t('zn.server', t(REGION[h.region])));
    parts.push(t('zn.since', ago(Date.parse(h.since) / 1000)));
    $('znSub').textContent = parts.join('  ·  ');
    const far = h.region && h.region !== 'europe';
    $('znRegion').hidden = !far;
    $('znRegion').textContent = far ? t('map.notEurope') : '';
  }

  const m = s.mapLast;
  const on = s.settings.mapSend;
  $('znSettings').hidden = on;
  if (m) {
    $('znPass').textContent = t('zn.lastPass', m.fromName || m.from, m.toName || m.to, clock(m.at));
    const [text, cls] = mapResult(m);
    $('znResult').textContent = text;
    $('znResult').className = 'note ' + cls;
  } else {
    $('znPass').textContent = '';
    $('znResult').textContent = on ? t('zn.noPass') : '';
    $('znResult').className = 'note';
  }
  if (!on) {
    $('znResult').textContent = t('zn.sendOff');
    $('znResult').className = 'note gold';
  }
}

$('znOpenMap').onclick = () => post('/api/open', { what: 'map' });

// --- карточка зоны (КарточкаЗоны и Оформление у мака) -------------------------

const QCOLOR = { safe: '#4E77C4', yellow: '#D8B23A', red: '#C24A3E', black: '#6B6F7A', city: '#8A6A3A', island: '#4E8E5A', roads: '#8A5ABF' };
const CHEST_SQ = { small: '🟩', small_veteran: '🟦', medium_veteran: '🟦', small_elite: '🟪', medium_elite: '🟨', large_elite: '🟨' };
const ROMAN = ['', 'I', 'II', 'III', 'IV', 'V', 'VI'];
// Ключа нет — пусто (обычные дороги словом не подписываем, как у мака).
const tOpt = key => (T[key] === undefined ? '' : T[key]);
const sortedEntries = o => Object.entries(o || {}).sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0));

function zoneKind(z) {
  const name = z.road ? tOpt('type.' + z.type) : t('q.' + z.quality);
  if (!name) return '';
  return z.road && z.grade > 0 && z.grade < ROMAN.length ? name + ' ' + ROMAN[z.grade] : name;
}

function zoneSub(z) {
  const parts = [];
  const k = zoneKind(z);
  if (k) parts.push(k);
  if (z.tier > 0) parts.push('T' + z.tier);
  if (!z.road && z.grade > 0) parts.push(t('q.grade', z.grade));
  return parts.join(' · ');
}

function clockLeft(sec) {
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = Math.floor(sec % 60);
  if (h > 0) return t('t.hm', h, m);
  return m + ':' + String(s).padStart(2, '0');
}

// Подпись кнопки карточки: мышь — словами, клавиша — «Ctrl+Q», «F7».
const KEY_MODS = { ctrl: 'Ctrl', alt: 'Alt', shift: 'Shift', win: 'Win' };
const KEY_NAMES = { pageup: 'Page Up', pagedown: 'Page Down', scrolllock: 'Scroll Lock', capslock: 'Caps Lock', numlock: 'Num Lock',
  insert: 'Insert', delete: 'Delete', break: 'Break', home: 'Home', end: 'End', pause: 'Pause', esc: 'Esc', space: 'Space', enter: 'Enter',
  tab: 'Tab', backspace: 'Backspace', left: '←', right: '→', up: '↑', down: '↓',
  vkba: ';', vkbb: '=', vkbc: ',', vkbd: '-', vkbe: '.', vkbf: '/', vkc0: '`', vkdb: '[', vkdc: '\\', vkdd: ']', vkde: "'" };
function keyLabel(k) {
  if (k === 'xbutton1') return t('key.mouse4');
  if (k === 'xbutton2') return t('key.mouse5');
  if (k === 'mbutton') return t('key.mouse3');
  if (k === 'off') return t('key.off');
  let s = String(k || '');
  let last = '';
  if (s.endsWith('num+')) { last = 'num+'; s = s.slice(0, -4); } else { const i = s.lastIndexOf('+'); last = s.slice(i + 1); s = i < 0 ? '' : s.slice(0, i + 1); }
  const mods = s.split('+').filter(Boolean).map(m => KEY_MODS[m] || m);
  let name = KEY_NAMES[last];
  if (!name && /^num./.test(last)) name = 'Num ' + last.slice(3);
  if (!name) name = last.toUpperCase();
  return mods.concat(name).join('+');
}

const cdRow = (label, html) => `<div class="cdrow"><div class="cdlabel">${esc(label)}</div><div class="cdval">${html}</div></div>`;

function cardHTML(c) {
  const z = c.zone;
  let h = `<div class="cdname">${esc(z.name)}${c.doubt ? ' ?' : ''}</div>`;
  h += `<div class="cdsub"><i class="qsq" style="background:${QCOLOR[z.quality] || 'var(--muted)'}"></i>${esc(zoneSub(z))}</div>`;
  if (c.doubt && c.alt) h += `<div class="cddoubt">${esc(t('zn.doubt', c.alt))}</div>`;
  const rows = [];
  const pts = z.points || [];
  rows.push(cdRow(t('zn.points'), pts.length ? esc(pts.length + ' · ' + pts.map(p => t('res.' + p)).join(', ')) : esc(t('zn.none'))));
  if ((z.biome || []).length) {
    // В открытом мире биом главнее: один основной и два попутных, без чисел.
    const icons = z.biome.map((r, i) => `<div class="cdicon" title="${esc(i === 0 ? t('zn.mainRes') : t('res.' + r))}">${icon('T' + Math.max(z.tier, 4) + '_' + r)}<span class="${i === 0 ? 'gold' : ''}">${esc(t('res.' + r))}</span></div>`).join('');
    rows.push(cdRow(t('zn.biome'), `<div class="cdicons">${icons}</div>`));
  } else if (Object.keys(z.res || {}).length) {
    const icons = sortedEntries(z.res).map(([r, pairs]) => {
      const tiers = pairs.map(p => p[0]);
      const lo = Math.min(...tiers), hi = Math.max(...tiers);
      const total = pairs.reduce((a, p) => a + p[1], 0);
      return `<div class="cdicon" title="${esc(t('res.' + r))}">${icon('T' + hi + '_' + r)}<span>${tiers.length > 1 ? 'T' + lo + '–T' + hi : 'T' + hi}</span><span class="n">×${total}</span></div>`;
    }).join('');
    rows.push(cdRow(t(z.road ? 'zn.nodes' : 'zn.biome'), `<div class="cdicons">${icons}</div>`));
  }
  if (Object.keys(z.chests || {}).length || z.road) {
    let v = Object.keys(z.chests || {}).length
      ? `<div class="cdicons">${sortedEntries(z.chests).map(([k, n]) => `<div class="cdicon" title="${esc(t('chest.' + k))}"><span class="chest">${CHEST_SQ[k] || '📦'}</span><span class="n">×${n}</span></div>`).join('')}</div>`
      : esc(t('zn.none'));
    const camps = sortedEntries(z.camps).map(([k, n]) => n + ' × ' + t('camp.' + k)).join(', ');
    if (camps) v += `<div class="camps">${esc(camps)}</div>`;
    rows.push(cdRow(t('zn.camps'), v));
  }
  if (Object.keys(z.dungeons || {}).length || z.mists) {
    const d = Object.keys(z.dungeons || {}).length ? sortedEntries(z.dungeons).map(([k, n]) => n + ' × ' + t('dng.' + k)).join(', ') : t('zn.none');
    rows.push(cdRow(t('zn.dungeons'), esc(d) + (z.mists ? '<div class="camps">' + esc(t('zn.mists')) + '</div>' : '')));
  }
  h += `<div class="cdpanel">${rows.join('')}</div>`;

  const foot = [];
  if (c.unstable) foot.push(`<span class="gold">${esc(t('zn.unstable'))}</span>`);
  if (c.size) foot.push(`<span>${esc(t('zn.portal', c.size))}</span>`);
  if (c.closesAt) {
    const left = (Date.parse(c.closesAt) - Date.now()) / 1000;
    foot.push(left > 0 ? `<span class="${left < 300 ? 'gold' : ''}">${esc(c.unstable ? t('zn.closesGroup', clockLeft(left)) : t('zn.closes', clockLeft(left)))}</span>` : `<span>${esc(t('zn.closed'))}</span>`);
  }
  foot.push(`<span>${esc(t('zn.shot', clock(c.at)))}</span>`);
  h += `<div class="cdfoot">${foot.join('')}</div>`;
  if (c.portal) {
    let line = '', cls = 'muted';
    if (c.map) [line, cls] = mapResult(c.map);
    else if (c.mapWhy === 'off') { line = t('zn.sendOff'); cls = 'gold'; }
    else if (c.mapWhy) { line = t('map.why.' + c.mapWhy); cls = 'gold'; }
    if (line) h += `<div class="note ${cls}" style="margin:6px 0 0">${esc(line)}</div>`;
  }
  if (c.risk) {
    h += `<div class="warn" style="margin-top:6px">${esc(t('zn.risk', c.black, c.total))}</div>`;
    h += `<div class="hint flat">${esc(t('zn.riskNote'))}</div>`;
  }
  return h;
}

function cardError(c) {
  switch (c.error) {
    case 'noTooltip': return t('zn.noTooltip');
    case 'unknown': return t('zn.unknown', c.arg || '');
    case 'capture': return t('zn.failed', c.arg || '');
    case 'ocr': return t('zn.ocrFailed', c.arg || '');
    case 'noLang': return t('ocr.hint.none');
    case 'noDict': return t('zn.noRef');
    case 'blank': return t('zn.blank');
  }
  return c.error;
}

function renderCard() {
  const c = S.card || {};
  const key = S.settings.zoneKey || 'xbutton1';
  $('cdHint').textContent = c.busy ? t('zn.busy') : key === 'off' ? t('zn.keyOff') : t('zn.hint', keyLabel(key));
  const hint = c.ocrHint;
  $('cdOcr').hidden = !hint;
  if (hint) $('cdOcr').textContent = t('ocr.hint.' + hint);
  let body = '';
  if (c.error) body = `<div class="note gold flat">${esc(cardError(c))}</div><div class="hint flat">${esc(t('zn.shot', clock(c.at)))}</div>`;
  else if (c.zone) body = cardHTML(c);
  if (c.mapHint) body += `<div class="hint flat">${esc(t('zn.mapHint'))}</div>`;
  setHTML($('cdBody'), body);
}

function renderLegend() {
  setHTML($('lgChests'), ['small', 'medium_veteran', 'small_elite', 'medium_elite']
    .map(k => `<span>${CHEST_SQ[k]} ${esc(t('chest.' + k))}</span>`).join(''));
  setHTML($('lgZones'), ['safe', 'yellow', 'red', 'black', 'city', 'island', 'roads']
    .map(q => `<span><i class="qsq" style="background:${QCOLOR[q]}"></i>${esc(t('q.' + q))}</span>`).join(''));
}

// Подсказка значков свёрнута по умолчанию (zoneLegend у мака).
try { $('cdLegend').open = localStorage.getItem('zoneLegend') === '1'; } catch (e) {}
$('cdLegend').addEventListener('toggle', () => { try { localStorage.setItem('zoneLegend', $('cdLegend').open ? '1' : '0'); } catch (e) {} });
$('znSettings').onclick = () => openSettings();

// --- оформление и кролик ------------------------------------------------------

// Тема меняется сразу, без перезапуска: переменные цветов и шрифтов висят
// на <html data-skin>. Кролик не крутится, пока окно спрятано в трей или
// свёрнуто: об этом говорит программа (windowHidden в /api/state и сразу —
// window.ajVisible), потому что WebView2 спрятанную страницу скрытой не
// считает. document.hidden — для браузера в режиме без WebView2.
function applyLook() {
  if (!S) return;
  const st = S.settings;
  const skin = st.skin === 'pixel' ? 'pixel' : 'plain';
  if (document.documentElement.dataset.skin !== skin) document.documentElement.dataset.skin = skin;
  const still = !st.logoAnim || S.windowHidden || document.hidden;
  const src = still ? 'rabbit.png' : 'rabbit.gif';
  const r = $('rabbit');
  if (!r.src.endsWith('/' + src)) r.src = src;
  r.classList.toggle('still', !st.logoAnim);
}
document.addEventListener('visibilitychange', applyLook);
window.ajVisible = v => { if (S) { S.windowHidden = !v; applyLook(); } };

// --- настройки ----------------------------------------------------------------

function renderSettings() {
  if (!S) return;
  const st = S.settings;
  applyLook();
  document.querySelectorAll('#skinTabs button').forEach(b => b.classList.toggle('on', b.dataset.skin === (st.skin === 'pixel' ? 'pixel' : 'plain')));
  document.querySelectorAll('[data-set]').forEach(el => { el.checked = !!st[el.dataset.set]; });
  document.querySelectorAll('#langTabs button').forEach(b => b.classList.toggle('on', b.dataset.lang === lang));
  $('shareHint').textContent = t(st.shareADP ? 'set.shareOn' : 'set.shareOff');
  renderKeyRec();
  const show = ['notify', 'panel', 'off'].includes(st.zoneShow) ? st.zoneShow : 'notify';
  document.querySelectorAll('#showTabs button').forEach(b => b.classList.toggle('on', b.dataset.show === show));
  $('showHint').textContent = t('show.' + show + 'Hint');
  $('panelBox').hidden = show !== 'panel';
  document.querySelectorAll('#cornerTabs button').forEach(b => b.classList.toggle('on', b.dataset.corner === (st.zoneOverlayCorner || 'topRight')));
  const sec = String(st.zoneOverlaySec || 5), sel = $('overlaySec');
  if (![...sel.options].some(o => o.value === sec)) sel.add(new Option(sec, sec));
  sel.value = sec;
  // Части карточки — и для уведомления, и для панели.
  $('ntBox').hidden = show === 'off';
  document.querySelectorAll('#ntOrder button').forEach(b => b.classList.toggle('on', b.dataset.order === (st.notifyOrder || 'chestsFirst')));
  $('dataDir').textContent = S.dataDir || '';
}

async function saveSettings(patch) {
  const { ok, j } = await post('/api/settings', patch, true);
  $('setErr').hidden = ok;
  if (!ok) $('setErr').textContent = t('set.saveFailed', j.error || t('err.generic'));
  if (j.settings && S) S.settings = j.settings;
  renderSettings();
  return ok;
}

function openSettings() { renderSettings(); $('setErr').hidden = true; $('settings').hidden = false; }
$('gear').onclick = openSettings;
$('setClose').onclick = () => { $('settings').hidden = true; };
$('settings').addEventListener('click', e => { if (e.target.id === 'settings') $('settings').hidden = true; });
document.addEventListener('keydown', e => {
  if (e.key !== 'Escape') return;
  // Во время записи Esc — отмена записи (хук программы тоже её видит), а не закрытие настроек.
  if (KR.active) post('/api/hotkey/cancel', {}).then(pollKeyRec);
  else $('settings').hidden = true;
});
document.querySelectorAll('[data-set]').forEach(el => {
  el.addEventListener('change', async () => { await saveSettings({ [el.dataset.set]: el.checked }); refresh(); });
});
$('langTabs').addEventListener('click', async e => {
  const b = e.target.closest('button');
  if (!b) return;
  if (await saveSettings({ language: b.dataset.lang })) {
    await loadLang(b.dataset.lang);
    renderAll();
  }
});
$('skinTabs').addEventListener('click', async e => {
  const b = e.target.closest('button');
  if (b) await saveSettings({ skin: b.dataset.skin });
});
$('openData').onclick = () => post('/api/open', { what: 'data' });
$('showTabs').addEventListener('click', async e => {
  const b = e.target.closest('button');
  if (b) await saveSettings({ zoneShow: b.dataset.show });
});
$('cornerTabs').addEventListener('click', async e => {
  const b = e.target.closest('button');
  if (b) await saveSettings({ zoneOverlayCorner: b.dataset.corner });
});
$('overlaySec').addEventListener('change', e => saveSettings({ zoneOverlaySec: Number(e.target.value) || 5 }));

// Кнопка карточки — как на маке: «Назначить» и следующее нажатие. Ловит
// программа своими хуками (WebView2 съел бы боковые кнопки как «назад»),
// страница только спрашивает, чем кончилось.
const KR = { active: false, hint: '', res: '', err: '', seq: 0, timer: 0 };
function renderKeyRec() {
  if (!S) return;
  const key = S.settings.zoneKey || 'xbutton1';
  $('zoneKey').textContent = KR.active ? t('key.waiting') : keyLabel(key);
  $('zoneKey').classList.toggle('waiting', KR.active);
  $('zoneKeyRec').textContent = KR.active ? t('key.cancel') : t('key.set');
  $('zoneKeyOff').hidden = KR.active || key === 'off';
  const done = KR.res === 'timeout' ? t('key.timeout') : KR.res === 'error' ? t('key.failed', KR.err || t('err.generic'))
    : KR.res === 'unsupported' ? t('key.unsupported') : '';
  const msg = KR.active ? (KR.hint === 'needMod' ? t('key.needMod') : t('key.recHint')) : done;
  $('zoneKeyMsg').hidden = !msg;
  $('zoneKeyMsg').textContent = msg;
  $('zoneKeyMsg').classList.toggle('gold', KR.active ? KR.hint === 'needMod' : !!done);
}
async function pollKeyRec() {
  clearTimeout(KR.timer);
  let st;
  try { st = await getJSON('/api/hotkey/record'); } catch (e) { KR.timer = setTimeout(pollKeyRec, 500); return; }
  if (st.seq !== KR.seq) st = { active: false, result: '' };
  KR.active = !!st.active;
  KR.hint = st.hint || '';
  if (KR.active) KR.timer = setTimeout(pollKeyRec, 250);
  else {
    KR.res = st.result || ''; KR.err = st.error || '';
    if (st.result === 'ok' && S) S.settings.zoneKey = st.key;
    refresh();
  }
  renderKeyRec();
}
$('zoneKeyRec').onclick = async () => {
  if (KR.active) { await post('/api/hotkey/cancel', {}); return pollKeyRec(); }
  KR.res = KR.err = '';
  const { ok, j } = await post('/api/hotkey/record', {}, true);
  if (!ok) { KR.res = 'error'; KR.err = j.error || ''; return renderKeyRec(); }
  KR.seq = j.seq; KR.active = true; KR.hint = '';
  renderKeyRec();
  pollKeyRec();
};
// Страницу перезагрузили посреди записи — подхватываем её.
getJSON('/api/hotkey/record').then(st => { if (st.active) { KR.seq = st.seq; KR.active = true; pollKeyRec(); } }).catch(() => {});
$('zoneKeyOff').onclick = async () => { KR.res = ''; await saveSettings({ zoneKey: 'off' }); refresh(); };
$('ntOrder').addEventListener('click', async e => {
  const b = e.target.closest('button');
  if (b) await saveSettings({ notifyOrder: b.dataset.order });
});

// --- обновление и поддержка ---------------------------------------------------

let restarting = false;   // нажали «Перезапустить сейчас», ждём закрытия
let supCopied = false;

function renderUpdate() {
  const u = S.update;
  const r = u && u.ready;
  $('updBanner').hidden = !r;
  if (r) {
    $('updTitle').textContent = t('upd.ready', r.version);
    $('updNotesBox').hidden = !r.notes;
    if ($('updNotes').textContent !== (r.notes || '')) $('updNotes').textContent = r.notes || '';
    $('updRestart').hidden = !!u.noWrite;
    $('updRestart').disabled = restarting || u.installing;
    let hint, bad = false;
    if (u.noWrite) { hint = t('upd.noWrite'); bad = true; }
    else if (u.installing || restarting) hint = t('upd.restarting');
    else if (u.installFailed) { hint = t('upd.installFailed'); bad = true; }
    else hint = S.settings.autoUpdate ? t('upd.onQuit') : t('upd.manualOnly');
    $('updHint').textContent = hint;
    $('updHint').classList.toggle('red', bad);
  }

  $('appVersion').textContent = t('upd.version', S.version || '—');
  const st = u ? u.state : 'dev';
  $('updCheck').disabled = !u || st === 'checking' || st === 'downloading' || st === 'dev';
  let res = '';
  if (st === 'checking') res = t('upd.checking');
  else if (st === 'latest') res = t('upd.latest', u.version);
  else if (st === 'downloading') res = t('upd.downloading', u.version);
  else if (st === 'ready') res = t('upd.downloaded', u.version);
  else if (st === 'error') res = t('upd.failed');
  else if (st === 'dev') res = t('upd.dev');
  $('updResult').textContent = res;
  $('updResult').classList.toggle('red', st === 'error');
  $('supCopy').textContent = supCopied ? t('sup.copied') : t('sup.copy');
}

$('updCheck').onclick = async () => { await post('/api/update/check'); refresh(); };
$('updRestart').onclick = async () => {
  restarting = true; renderUpdate();
  // Ошибку покажет сама плашка (installFailed) — без окна alert.
  const { ok } = await post('/api/update/restart', {}, true);
  if (!ok) restarting = false;
  refresh();
};
$('supDiscord').onclick = () => post('/api/open', { what: 'discord' });
$('supCopy').onclick = async () => {
  const { ok, j } = await post('/api/support', {}, true);
  if (!ok || !j.text) return;
  await copyText(j.text);
  supCopied = true; renderUpdate();
  setTimeout(() => { supCopied = false; if (S) renderUpdate(); }, 1600);
};

// --- общий цикл ---------------------------------------------------------------

function renderAll() {
  if (!S) return;
  renderStatus();
  renderSettings();
  renderUpdate();
  if (tab === 'zonefix') renderZoneFix();
  if (tab === 'zone') renderZone();
  if (tab === 'session') renderSession();
  if (tab === 'shared') renderShared();
}

let tick = 0;
async function refresh() {
  try { S = await getJSON('/api/state'); }
  catch (e) { document.title = 'Albion Journal — ' + t('err.offline'); return; }
  document.title = 'Albion Journal';
  if (S.lang && S.lang !== lang) await loadLang(S.lang);
  renderAll();
  tick++;
  if (tab === 'own' || tab === 'session') refreshTab();
  else if (tab === 'shared' && tick % 30 === 0) refreshShared(false);
}

(async () => {
  await loadLang('');
  showTab(['own', 'shared', 'session', 'zone', 'zonefix'].includes(tab) ? tab : 'own');
  await refresh();
  setInterval(refresh, 2000);
})();
