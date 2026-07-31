// Service worker расширения: наблюдает за сетевыми запросами вкладок,
// собирает найденное медиа и по команде из popup отдаёт его программе.
//
// Принципы:
//   * только наблюдение — блокирующий webRequest не используется, трафик не меняем;
//   * никаких внешних адресов: единственный получатель данных — 127.0.0.1;
//   * DRM не трогаем — защищённые потоки помечаем и не даём скачивать.
//
// Про время жизни: MV3 глушит service worker примерно через 30 секунд простоя,
// поэтому всё найденное дублируется в chrome.storage.session (память, не диск)
// и поднимается обратно при следующем пробуждении.

importScripts('config.js');

const CFG = self.VDOWN_CONFIG || {};
const PROTOCOL = 1;
const EXT_VERSION = chrome.runtime.getManifest().version;

// Пределы: чтобы список не рос бесконечно на «долгоживущих» вкладках.
const MAX_ITEMS_PER_TAB = 40;
const MAX_TABS = 20;
const MAX_PENDING = 300;
const MANIFEST_FETCH_LIMIT = 512 * 1024; // читаем не больше 512 КБ манифеста

// --- что считаем медиа ---

const MEDIA_EXT = /\.(m3u8|mpd|mp4|m4v|webm|mov|mkv|flv|mp3|m4a|aac|ogg|oga|opus|wav)(?:$|[?#])/i;
// Сегменты потока: показывать их по одному бессмысленно — нужен манифест.
const SEGMENT_EXT = /\.(ts|m4s|cmfv|cmfa|cmft|vtt)(?:$|[?#])/i;
const SEGMENT_NAME = /(?:^|\/)(?:init|seg|segment|chunk|frag|media)[-_]?\d*\.(?:mp4|m4v|webm|m4a|aac)(?:$|[?#])/i;
const SEGMENT_NUMBERED = /[-_]\d{3,}\.(?:mp4|m4v|webm|m4a|aac)(?:$|[?#])/i;

const MEDIA_CTYPE = /^(?:video\/|audio\/|application\/(?:vnd\.apple\.mpegurl|x-mpegurl|dash\+xml|vnd\.ms-sstr\+xml|octet-stream))/i;
// Ответ text/html с расширением .mp4 в адресе — почти всегда заглушка или ошибка.
const NOT_MEDIA_CTYPE = /^(?:text\/html|application\/json|text\/plain)/i;

// Признаки лицензионного сервера DRM. Точного способа нет, поэтому это только
// повод показать предупреждение на вкладке, а не блокировать всё подряд.
const DRM_HINT = /(widevine|playready|fairplay|licen[sc]e|\/wv\/|drmtoday|getlicense|keydelivery)/i;

const HEADERS_OF_INTEREST = ['referer', 'user-agent', 'cookie', 'origin'];

// --- состояние ---

// tabs[tabId] = {pageUrl, title, drmSeen, segments, updated, items:{[key]: item}}
let tabs = {};
// pending[requestId] = {tabId, url, headers, matched, ts}
const pending = new Map();
let restored = false;
let saveTimer = null;

// Рабочий порт программы: кешируется, чтобы не перебирать диапазон на каждый чих.
let appPort = 0;
let appPortAt = 0;
let appInfo = null;

// --- восстановление и сохранение состояния ---

async function ensureRestored() {
  if (restored) return;
  restored = true;
  try {
    const data = await chrome.storage.session.get('tabs');
    if (data && data.tabs) tabs = data.tabs;
  } catch (e) {
    tabs = {};
  }
  try {
    const local = await chrome.storage.local.get('port');
    if (local && local.port) appPort = local.port;
  } catch (e) { /* не критично */ }
}

function scheduleSave() {
  if (saveTimer) return;
  saveTimer = setTimeout(() => {
    saveTimer = null;
    chrome.storage.session.set({tabs}).catch(() => {});
  }, 500);
}

// --- разбор адресов ---

function keyOf(url) {
  // Подпись живёт в query, поэтому ключ дедупликации — origin+path без неё.
  try {
    const u = new URL(url);
    return u.origin + u.pathname;
  } catch (e) {
    return url;
  }
}

function isSegment(url) {
  const path = pathOf(url);
  return SEGMENT_EXT.test(path) || SEGMENT_NAME.test(path) || SEGMENT_NUMBERED.test(path);
}

function pathOf(url) {
  try {
    return new URL(url).pathname;
  } catch (e) {
    return url;
  }
}

function kindOf(url, ctype) {
  const p = pathOf(url).toLowerCase();
  const c = (ctype || '').toLowerCase();
  if (p.endsWith('.m3u8') || c.includes('mpegurl')) return 'hls';
  if (p.endsWith('.mpd') || c.includes('dash+xml')) return 'dash';
  return 'file';
}

function extOf(url) {
  const m = pathOf(url).match(/\.([a-z0-9]{2,5})$/i);
  return m ? m[1].toLowerCase() : '';
}

function nameOf(url) {
  const p = pathOf(url);
  const base = p.slice(p.lastIndexOf('/') + 1);
  return base || p;
}

// --- наблюдение за трафиком ---

function trackable(details) {
  // tabId < 0 — служебные запросы (в том числе наши собственные проверки манифестов).
  if (details.tabId === undefined || details.tabId < 0) return false;
  return /^https?:/i.test(details.url);
}

chrome.webRequest.onBeforeRequest.addListener(
  (d) => {
    if (!trackable(d)) return;
    if (DRM_HINT.test(d.url)) markDrmSeen(d.tabId);
    if (pending.size > MAX_PENDING) {
      // Старые «висяки» (запросы без ответа) не должны копиться.
      const first = pending.keys().next();
      if (!first.done) pending.delete(first.value);
    }
    pending.set(d.requestId, {
      tabId: d.tabId,
      url: d.url,
      headers: {},
      matchedByUrl: MEDIA_EXT.test(d.url) && !isSegment(d.url),
      segment: isSegment(d.url),
      ts: Date.now()
    });
  },
  {urls: ['<all_urls>']}
);

function headerListener(d) {
  const p = pending.get(d.requestId);
  if (!p) return;
  for (const h of d.requestHeaders || []) {
    const name = h.name.toLowerCase();
    if (HEADERS_OF_INTEREST.includes(name) && h.value) p.headers[name] = h.value;
  }
  if (p.matchedByUrl) commit(p, {});
  else if (p.segment) countSegment(p.tabId);
}

// extraHeaders обязателен, иначе Chrome не покажет Cookie и часть Referer.
// В отдельных сборках Chromium его может не быть — тогда работаем без него.
try {
  chrome.webRequest.onSendHeaders.addListener(
    headerListener, {urls: ['<all_urls>']}, ['requestHeaders', 'extraHeaders']);
} catch (e) {
  chrome.webRequest.onSendHeaders.addListener(
    headerListener, {urls: ['<all_urls>']}, ['requestHeaders']);
}

function responseListener(d) {
  const p = pending.get(d.requestId);
  if (!p) return;
  let ctype = '', clen = 0;
  for (const h of d.responseHeaders || []) {
    const name = h.name.toLowerCase();
    if (name === 'content-type') ctype = h.value || '';
    else if (name === 'content-length') clen = parseInt(h.value, 10) || 0;
  }
  if (p.segment) return;
  if (NOT_MEDIA_CTYPE.test(ctype)) return;
  // octet-stream отдают под что угодно — принимаем его только вместе с медийным адресом.
  const octet = /octet-stream/i.test(ctype);
  if (MEDIA_CTYPE.test(ctype) && (!octet || p.matchedByUrl)) {
    commit(p, {ctype, size: clen});
  }
}

try {
  chrome.webRequest.onHeadersReceived.addListener(
    responseListener, {urls: ['<all_urls>']}, ['responseHeaders', 'extraHeaders']);
} catch (e) {
  chrome.webRequest.onHeadersReceived.addListener(
    responseListener, {urls: ['<all_urls>']}, ['responseHeaders']);
}

const forget = (d) => pending.delete(d.requestId);
chrome.webRequest.onCompleted.addListener(forget, {urls: ['<all_urls>']});
chrome.webRequest.onErrorOccurred.addListener(forget, {urls: ['<all_urls>']});

// --- накопление находок ---

async function tabEntry(tabId) {
  await ensureRestored();
  let t = tabs[tabId];
  if (!t) {
    t = {pageUrl: '', title: '', drmSeen: false, segments: 0, updated: Date.now(), items: {}};
    tabs[tabId] = t;
    pruneTabs();
  }
  return t;
}

function pruneTabs() {
  const ids = Object.keys(tabs);
  if (ids.length <= MAX_TABS) return;
  ids.sort((a, b) => (tabs[a].updated || 0) - (tabs[b].updated || 0));
  for (const id of ids.slice(0, ids.length - MAX_TABS)) delete tabs[id];
}

async function markDrmSeen(tabId) {
  const t = await tabEntry(tabId);
  if (!t.drmSeen) {
    t.drmSeen = true;
    scheduleSave();
  }
}

async function countSegment(tabId) {
  const t = await tabEntry(tabId);
  t.segments = (t.segments || 0) + 1;
  scheduleSave();
}

// announce отмечается в программе, чтобы она показала «расширение подключено».
// Отдельного «сердцебиения» нет намеренно: MV3 усыпляет service worker, и будить
// его таймером ради статуса — тратить батарею. Достаточно моментов, когда
// расширение и так проснулось: старт, первая находка, открытие popup, загрузка.
let announcedAt = 0;
function announce() {
  if (Date.now() - announcedAt < 30000) return;
  announcedAt = Date.now();
  findApp(true).catch(() => {});
}

async function commit(p, extra) {
  const t = await tabEntry(p.tabId);
  const key = keyOf(p.url);
  const now = Date.now();
  const prev = t.items[key];

  const item = prev || {
    key,
    url: p.url,
    name: nameOf(p.url),
    kind: kindOf(p.url, extra.ctype),
    ext: extOf(p.url),
    size: 0,
    quality: '',
    duration: 0,
    variants: 0,
    drm: false,
    master: false,
    probed: false,
    ts: now
  };
  // Подпись в query протухает — держим самый свежий адрес.
  item.url = p.url;
  item.seen = now;
  if (extra.ctype) {
    item.ctype = extra.ctype;
    item.kind = kindOf(p.url, extra.ctype);
  }
  if (extra.size && extra.size > item.size) item.size = extra.size;
  if (Object.keys(p.headers).length) {
    item.headers = {
      referer: p.headers['referer'] || (item.headers && item.headers.referer) || '',
      userAgent: p.headers['user-agent'] || (item.headers && item.headers.userAgent) || '',
      cookie: p.headers['cookie'] || (item.headers && item.headers.cookie) || '',
      origin: p.headers['origin'] || (item.headers && item.headers.origin) || ''
    };
  }

  t.items[key] = item;
  t.updated = now;
  limitItems(t);
  scheduleSave();
  updateBadge(p.tabId, t);
  fillPageInfo(p.tabId, t);
  if (!prev) announce();

  if (!item.probed && (item.kind === 'hls' || item.kind === 'dash')) {
    item.probed = true;
    probeManifest(p.tabId, key, item.url, item.kind);
  }
}

function limitItems(t) {
  const keys = Object.keys(t.items);
  if (keys.length <= MAX_ITEMS_PER_TAB) return;
  keys.sort((a, b) => (t.items[a].seen || 0) - (t.items[b].seen || 0));
  for (const k of keys.slice(0, keys.length - MAX_ITEMS_PER_TAB)) delete t.items[k];
}

async function fillPageInfo(tabId, t) {
  if (t.pageUrl && t.title) return;
  try {
    const tab = await chrome.tabs.get(tabId);
    if (tab) {
      t.pageUrl = tab.url || t.pageUrl;
      t.title = tab.title || t.title;
    }
  } catch (e) { /* вкладка уже закрыта */ }
}

function visibleItems(t) {
  if (!t) return [];
  const items = Object.values(t.items || {});
  // Мастер-манифест перекрывает свои варианты: показываем один вход в поток.
  const masters = items.filter((i) => i.master).map((i) => i.key.slice(0, i.key.lastIndexOf('/') + 1));
  const filtered = items.filter((i) => {
    if (i.master) return true;
    if (i.kind !== 'hls' && i.kind !== 'dash') return true;
    const dir = i.key.slice(0, i.key.lastIndexOf('/') + 1);
    return !masters.includes(dir);
  });
  filtered.sort((a, b) => (b.seen || 0) - (a.seen || 0));
  return filtered;
}

function updateBadge(tabId, t) {
  const n = visibleItems(t).length;
  chrome.action.setBadgeText({tabId, text: n ? String(n) : ''}).catch(() => {});
  chrome.action.setBadgeBackgroundColor({tabId, color: '#2563EB'}).catch(() => {});
}

// --- разбор манифестов: качество, длительность, признак DRM ---

async function probeManifest(tabId, key, url, kind) {
  let text = '';
  try {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), 8000);
    const res = await fetch(url, {credentials: 'include', signal: ctrl.signal});
    clearTimeout(timer);
    if (!res.ok) return;
    const buf = await res.arrayBuffer();
    text = new TextDecoder().decode(buf.slice(0, MANIFEST_FETCH_LIMIT));
  } catch (e) {
    return; // не смогли — не беда, покажем без качества
  }

  await ensureRestored();
  const t = tabs[tabId];
  if (!t || !t.items[key]) return;
  const item = t.items[key];

  if (kind === 'hls') parseHls(item, text);
  else parseDash(item, text);

  scheduleSave();
  updateBadge(tabId, t);
}

function parseHls(item, text) {
  // SAMPLE-AES + com.apple.streamingkeydelivery / widevine → DRM.
  // Обычный AES-128 (METHOD=AES-128) — это не DRM, yt-dlp его берёт штатно.
  if (/#EXT-X-(?:SESSION-)?KEY:[^\n]*(?:SAMPLE-AES|com\.apple\.streamingkeydelivery|widevine|urn:uuid:edef8ba9)/i.test(text)) {
    item.drm = true;
  }
  const variants = [...text.matchAll(/#EXT-X-STREAM-INF:[^\n]*/gi)];
  if (variants.length) {
    item.master = true;
    item.variants = variants.length;
    let best = 0;
    for (const v of variants) {
      const m = v[0].match(/RESOLUTION=(\d+)x(\d+)/i);
      if (m) best = Math.max(best, parseInt(m[2], 10));
    }
    if (best) item.quality = best + 'p';
  } else {
    let dur = 0;
    for (const m of text.matchAll(/#EXTINF:([0-9.]+)/g)) dur += parseFloat(m[1]) || 0;
    if (dur > 0) item.duration = Math.round(dur);
  }
}

function parseDash(item, text) {
  if (/<ContentProtection/i.test(text)) item.drm = true;
  let best = 0;
  for (const m of text.matchAll(/height="(\d+)"/gi)) best = Math.max(best, parseInt(m[1], 10));
  if (best) item.quality = best + 'p';
  const d = text.match(/mediaPresentationDuration="PT(?:(\d+)H)?(?:(\d+)M)?(?:([0-9.]+)S)?"/i);
  if (d) {
    const sec = (parseInt(d[1] || 0, 10) * 3600) + (parseInt(d[2] || 0, 10) * 60) + Math.round(parseFloat(d[3] || 0));
    if (sec > 0) item.duration = sec;
  }
}

// --- уборка за вкладками ---

chrome.tabs.onRemoved.addListener(async (tabId) => {
  await ensureRestored();
  if (tabs[tabId]) {
    delete tabs[tabId];
    scheduleSave();
  }
});

chrome.tabs.onUpdated.addListener(async (tabId, info, tab) => {
  if (!info.url) return;
  await ensureRestored();
  const t = tabs[tabId];
  if (!t) return;
  // Переход на другую страницу — прошлые находки к ней отношения не имеют.
  // Смена только хеша (#) страницу не меняет, список сохраняем.
  const sameDoc = t.pageUrl && t.pageUrl.split('#')[0] === info.url.split('#')[0];
  if (!sameDoc) {
    t.items = {};
    t.drmSeen = false;
    t.segments = 0;
  }
  t.pageUrl = info.url;
  t.title = (tab && tab.title) || '';
  t.updated = Date.now();
  scheduleSave();
  updateBadge(tabId, t);
});

// --- связь с программой ---

async function ping(port, timeoutMs) {
  try {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), timeoutMs || 900);
    const res = await fetch(
      `http://127.0.0.1:${port}/api/ping?from=ext&v=${encodeURIComponent(EXT_VERSION)}&p=${PROTOCOL}`,
      {signal: ctrl.signal, cache: 'no-store'});
    clearTimeout(timer);
    if (!res.ok) return null;
    const j = await res.json();
    return j && j.app === 'go_yt_dwn' ? j : null;
  } catch (e) {
    return null;
  }
}

function portCandidates() {
  const list = [];
  if (appPort) list.push(appPort);
  if (CFG.preferredPort) list.push(CFG.preferredPort);
  for (const p of CFG.ports || []) list.push(p);
  if (!list.length) list.push(8080);
  return [...new Set(list)];
}

// findApp ищет живой порт программы. force=true — не доверять кешу.
async function findApp(force) {
  await ensureRestored();
  if (!force && appPort && Date.now() - appPortAt < 10000 && appInfo) {
    return {port: appPort, info: appInfo};
  }
  for (const p of portCandidates()) {
    const info = await ping(p);
    if (info) {
      appPort = p;
      appPortAt = Date.now();
      appInfo = info;
      chrome.storage.local.set({port: p}).catch(() => {});
      return {port: p, info};
    }
  }
  appInfo = null;
  return {port: 0, info: null};
}

async function sendCapture(tabId, item) {
  const {port, info} = await findApp(false);
  if (!port) {
    return {ok: false, code: 0, error: 'Программа Video Downloader не запущена. Запустите её и повторите.'};
  }
  if (item.drm) {
    return {ok: false, code: 0, error: 'Поток защищён DRM — скачивание не поддерживается.'};
  }
  const t = tabs[tabId] || {};
  const body = {
    token: CFG.token || '',
    url: item.url,
    pageUrl: t.pageUrl || '',
    title: t.title || item.name,
    kind: item.kind,
    headers: item.headers || {},
    size: item.size || 0,
    quality: item.quality || '',
    duration: item.duration || 0,
    protocol: PROTOCOL,
    extVersion: EXT_VERSION
  };
  try {
    const res = await fetch(`http://127.0.0.1:${port}/api/capture`, {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(body)
    });
    const j = await res.json().catch(() => ({}));
    if (res.status === 403) {
      return {ok: false, code: 403,
        error: j.error || 'Программа не приняла ключ. Переустановите расширение из программы (кнопка «Расширение»).'};
    }
    if (!res.ok) return {ok: false, code: res.status, error: j.error || `Ошибка программы (${res.status})`};
    return {ok: true, id: j.id, info};
  } catch (e) {
    appInfo = null;
    return {ok: false, code: 0, error: 'Не удалось связаться с программой: ' + e.message};
  }
}

// --- сообщения из popup ---

chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  handleMessage(msg).then(sendResponse).catch((e) => sendResponse({error: String(e)}));
  return true; // ответ придёт асинхронно
});

async function handleMessage(msg) {
  await ensureRestored();
  switch (msg && msg.action) {
    case 'state': {
      const t = tabs[msg.tabId];
      const {port, info} = await findApp(msg.force === true);
      return {
        items: visibleItems(t),
        drmSeen: t ? !!t.drmSeen : false,
        segments: t ? (t.segments || 0) : 0,
        pageUrl: t ? t.pageUrl : '',
        app: port ? {port, version: info.version, protocol: info.protocol} : null,
        protocol: PROTOCOL,
        extVersion: EXT_VERSION,
        tokenSet: !!(CFG.token && CFG.token.indexOf('__') !== 0)
      };
    }
    case 'download': {
      const t = tabs[msg.tabId];
      const item = t && t.items[msg.key];
      if (!item) return {ok: false, error: 'Запись устарела, обновите список.'};
      return sendCapture(msg.tabId, item);
    }
    case 'downloadAll': {
      const t = tabs[msg.tabId];
      const list = visibleItems(t).filter((i) => !i.drm);
      let queued = 0;
      let last = null;
      for (const item of list) {
        const r = await sendCapture(msg.tabId, item);
        if (r.ok) queued++;
        else last = r;
      }
      if (queued === 0 && last) return last;
      return {ok: true, queued};
    }
    case 'clear': {
      const t = tabs[msg.tabId];
      if (t) {
        t.items = {};
        t.segments = 0;
        scheduleSave();
        updateBadge(msg.tabId, t);
      }
      return {ok: true};
    }
    default:
      return {error: 'неизвестная команда'};
  }
}

// Пробуждение после перезапуска браузера/обновления расширения.
chrome.runtime.onStartup.addListener(() => { ensureRestored().then(announce); });
chrome.runtime.onInstalled.addListener(() => { ensureRestored().then(announce); });
// Первый запуск service worker: программа сразу увидит «расширение подключено».
ensureRestored().then(announce);
