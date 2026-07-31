// Popup: показывает найденное на текущей вкладке и просит service worker
// отправить выбранное в программу. Своей сетевой логики здесь нет —
// за единственным исключением: превью со страницы грузится обычным <img>,
// то есть браузером, а не нами (картинка почти всегда уже в его кеше).

const $ = (id) => document.getElementById(id);
const PROTOCOL = 1;

let tabId = null;
let pageUrl = '';
let state = null;
let lastSignature = '';
// Выбор качества по строкам: key → высота в пикселях (0 — «лучшее»).
const choice = {};
// Состояние превью по строкам: key → {src} готовое, {done:true} без картинки.
const previews = {};
// Картинки со страницы: og:image / twitter:image и poster у <video>.
let pageImages = null;
let pagePromise = null;
// Очередь кадров через программу — строго по одному: ffmpeg дорогой, а первые
// строки списка человеку нужнее последних.
let frameQueue = Promise.resolve();

const ICON = {
  drm: '<svg class="icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="4" y="10" width="16" height="10" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/></svg>',
  warn: '<svg class="icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 3 2 20h20L12 3z"/><path d="M12 9v5m0 3v.5"/></svg>',
  info: '<svg class="icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M12 11v5m0-8.5v.5"/></svg>',
  empty: '<svg class="icon" width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><rect x="2" y="5" width="20" height="14" rx="2"/><path d="m10 9 5 3-5 3z"/></svg>',
  // Заглушки по типу источника: место под картинку занято в любом случае.
  film: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><rect x="2" y="5" width="20" height="14" rx="2"/><path d="M7 5v14M17 5v14"/></svg>',
  wave: '<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M4 10v4M8 7v10M12 4v16M16 7v10M20 10v4"/></svg>'
};

function esc(s) {
  return String(s == null ? '' : s).replace(/[&<>"']/g,
    (c) => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'}[c]));
}

function humanSize(n) {
  if (!n) return '';
  const u = ['Б', 'КБ', 'МБ', 'ГБ'];
  let i = 0, v = n;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return (v >= 10 || i === 0 ? Math.round(v) : v.toFixed(1)) + ' ' + u[i];
}

function humanTime(sec) {
  if (!sec) return '';
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60;
  const pad = (x) => String(x).padStart(2, '0');
  return h ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
}

// humanRate — битрейт варианта, чтобы различать «720p подешевле» и «720p получше»,
// когда высоты совпали.
function humanRate(bps) {
  if (!bps) return '';
  return bps >= 1000000 ? (bps / 1000000).toFixed(1) + ' Мбит' : Math.round(bps / 1000) + ' кбит';
}

const KIND_LABEL = {hls: 'HLS', dash: 'DASH', file: 'ФАЙЛ'};
const AUDIO_EXT = /^(mp3|m4a|aac|ogg|oga|opus|wav)$/i;

function toast(text, cls) {
  const el = $('toast');
  el.textContent = text;
  el.className = 'toast ' + (cls || '');
  el.hidden = false;
  clearTimeout(toast.t);
  toast.t = setTimeout(() => { el.hidden = true; }, 4000);
}

function banner(kind, title, text) {
  const icon = kind === 'err' ? ICON.warn : (kind === 'warn' ? ICON.warn : ICON.info);
  return `<div class="banner ${kind}">${icon}<div><b>${esc(title)}</b>${esc(text)}</div></div>`;
}

// --- выбор качества ---

// pickDefault выбирает вариант при первой отрисовке строки: точное совпадение
// с прошлым выбором, иначе ближайший снизу, иначе «лучшее».
function pickDefault(levels, pref) {
  if (!pref || !levels.length) return 0;
  if (levels.some((l) => l.h === pref)) return pref;
  const lower = levels.filter((l) => l.h <= pref);
  return lower.length ? lower[0].h : 0;
}

function levelsOf(it) {
  return Array.isArray(it.levels) ? it.levels : [];
}

function qualitySelect(it) {
  const levels = levelsOf(it);
  // Прямой файл — вариантов нет, пустой селектор не показываем вовсе.
  if (it.drm || levels.length < 2) return '';
  const current = choice[it.key];
  const opts = [`<option value="0"${current ? '' : ' selected'}>Лучшее</option>`];
  for (const l of levels) {
    const rate = humanRate(l.bw);
    const label = l.h + 'p' + (rate ? ' · ' + rate : '');
    opts.push(`<option value="${l.h}"${current === l.h ? ' selected' : ''}>${esc(label)}</option>`);
  }
  return `<select class="quality" data-key="${esc(it.key)}" aria-label="Качество"` +
    ` title="Качество для этой находки">${opts.join('')}</select>`;
}

// --- отрисовка ---

// signature — по чему решаем, что список изменился. Перерисовка каждые две
// секунды выбивала бы открытый список качества и сбрасывала загруженные превью.
function signature(st) {
  if (!st) return '';
  return JSON.stringify([
    st.app ? st.app.port : 0, st.tokenSet, st.drmSeen, (st.items || []).length ? 0 : st.segments,
    (st.items || []).map((i) => [i.key, i.size, i.duration, i.quality, i.drm, i.live, levelsOf(i).length])
  ]);
}

function render() {
  if (!state) return;
  const sig = signature(state);
  if (sig === lastSignature) return;
  lastSignature = sig;

  const banners = [];
  const body = $('body');

  // Связь с программой.
  const st = $('appState');
  if (state.app) {
    st.className = 'chip ok';
    st.textContent = 'программа на :' + state.app.port;
  } else {
    st.className = 'chip err';
    st.textContent = 'не запущена';
    banners.push(banner('warn', 'Программа не запущена',
      'Откройте Video Downloader — расширение само найдёт её на портах 8080–8090.'));
  }

  if (!state.tokenSet) {
    banners.push(banner('err', 'Расширение без ключа',
      'Папка скопирована мимо программы. Нажмите «Расширение» в программе и загрузите папку, которую она подготовит.'));
  } else if (state.app && state.app.protocol && state.app.protocol !== PROTOCOL) {
    banners.push(banner('warn', 'Версии разошлись',
      `Расширение говорит на протоколе ${PROTOCOL}, программа — на ${state.app.protocol}. Обновите обе стороны.`));
  }

  if (state.drmSeen) {
    banners.push(banner('info', 'На странице замечена DRM-защита',
      'Такие потоки скачивать нельзя — они помечены и кнопки для них нет.'));
  }

  $('banners').innerHTML = banners.join('');

  const items = state.items || [];
  if (!items.length) {
    const hint = state.segments
      ? `Видно только сегменты потока (${state.segments} шт.), а манифест был запрошен до открытия страницы. Обновите страницу — и он попадёт в список.`
      : 'Запустите видео на странице — расширение поймает его запросы. Список живёт отдельно для каждой вкладки.';
    body.innerHTML = `<div class="empty">${ICON.empty}<b>Пока ничего не найдено</b><p>${esc(hint)}</p></div>`;
    $('all').disabled = true;
    return;
  }

  const rows = items.map((it) => {
    const levels = levelsOf(it);
    if (!(it.key in choice)) choice[it.key] = pickDefault(levels, prefHeight);

    const tags = [`<span class="tag kind">${KIND_LABEL[it.kind] || esc(it.ext.toUpperCase())}</span>`];
    if (it.ext && it.kind === 'file') tags.push(`<span class="tag">${esc(it.ext)}</span>`);
    if (it.live) tags.push('<span class="tag live" title="Прямой эфир — длительность неизвестна">эфир</span>');
    else if (it.duration) tags.push(`<span class="tag">${humanTime(it.duration)}</span>`);
    // Качество отдельной меткой нужно только там, где выбрать нельзя.
    if (it.quality && levels.length < 2) tags.push(`<span class="tag">${esc(it.quality)}</span>`);
    // «N вар.» имеет смысл, только если вариантов было больше, чем в списке:
    // одинаковые высоты с разным битрейтом свёрнуты в одну строку.
    if (levels.length && it.variants > levels.length) {
      tags.push(`<span class="tag" title="Показаны ${levels.length} из ${it.variants}: по одному варианту на разрешение">${it.variants} вар.</span>`);
    }
    if (it.size) tags.push(`<span class="tag">${humanSize(it.size)}</span>`);
    if (it.headers && it.headers.cookie) tags.push('<span class="tag" title="Передадим куки и Referer">с куками</span>');
    if (it.drm) tags.push('<span class="tag drm">DRM</span>');

    const action = it.drm
      ? '<span class="chip err" title="Обход защиты не поддерживается">защищено</span>'
      : `<button class="btn btn-primary dl" data-key="${esc(it.key)}">Скачать</button>`;

    return `<div class="item${it.drm ? ' blocked' : ''}">
        <div class="thumb loading" data-key="${esc(it.key)}">${placeholder(it)}</div>
        <div class="body">
          <div class="name" title="${esc(it.url)}">${esc(it.name)}</div>
          <div class="sub">${qualitySelect(it)}${tags.join('')}</div>
        </div>${action}
      </div>`;
  });

  body.innerHTML = `<div class="list">${rows.join('')}</div>`;
  $('all').disabled = !items.some((i) => !i.drm) || !state.app;

  for (const btn of body.querySelectorAll('.dl')) {
    btn.addEventListener('click', () => download(btn));
  }
  for (const sel of body.querySelectorAll('.quality')) {
    sel.addEventListener('change', () => {
      choice[sel.dataset.key] = parseInt(sel.value, 10) || 0;
      rememberPref(choice[sel.dataset.key]);
    });
  }

  applyPreviews();
  loadPreviews(items);
}

function placeholder(it) {
  return AUDIO_EXT.test(it.ext || '') ? ICON.wave : ICON.film;
}

// --- превью ---

function applyPreviews() {
  for (const box of document.querySelectorAll('.thumb')) {
    const p = previews[box.dataset.key];
    if (!p) continue;
    box.classList.remove('loading');
    if (p.src) box.innerHTML = `<img alt="" src="${esc(p.src)}">`;
  }
}

function setPreview(key, src) {
  previews[key] = src ? {src} : {done: true};
  applyPreviews();
}

// pageImageFor подбирает картинку со страницы: сначала poster у того же <video>,
// потом общий poster, потом og:image / twitter:image страницы.
function pageImageFor(it) {
  if (!pageImages) return '';
  const same = pageImages.posters.find((p) => p.src && (p.src === it.url || sameMedia(p.src, it.url)));
  if (same) return same.poster;
  if (pageImages.posters.length) return pageImages.posters[0].poster;
  return pageImages.og || '';
}

function sameMedia(a, b) {
  try {
    const x = new URL(a), y = new URL(b);
    return x.origin === y.origin && x.pathname === y.pathname;
  } catch (e) {
    return false;
  }
}

// tryImage проверяет картинку загрузкой: чужой домен может ответить 403 без
// Referer, и тогда мы просто уходим на следующий источник.
function tryImage(url) {
  return new Promise((resolve) => {
    const img = new Image();
    img.onload = () => resolve(img.naturalWidth > 0);
    img.onerror = () => resolve(false);
    img.src = url;
  });
}

async function loadPreviews(items) {
  await pagePromise;
  for (const it of items) {
    if (previews[it.key]) continue;
    const fromPage = pageImageFor(it);
    if (fromPage && await tryImage(fromPage)) {
      setPreview(it.key, fromPage);
      continue;
    }
    if (previews[it.key]) continue;
    // Кадр через программу — по одному в очереди, чтобы список не ждал.
    frameQueue = frameQueue.then(async () => {
      if (previews[it.key]) return;
      const res = await chrome.runtime.sendMessage({action: 'preview', tabId, key: it.key})
        .catch(() => null);
      setPreview(it.key, res && res.ok ? res.thumb : '');
    });
  }
}

// grabPageMedia исполняется на странице — разово, в момент открытия попапа.
// Постоянного content script нет намеренно: расширение и так видит весь трафик,
// добавлять к этому вечную инъекцию в каждую вкладку незачем.
function grabPageMedia() {
  const meta = (sel) => {
    const el = document.querySelector(sel);
    return (el && el.getAttribute('content')) || '';
  };
  const posters = [];
  for (const v of document.querySelectorAll('video')) {
    if (v.poster) posters.push({poster: v.poster, src: v.currentSrc || v.src || ''});
    if (posters.length >= 8) break;
  }
  return {
    og: meta('meta[property="og:image"]') || meta('meta[name="og:image"]') ||
        meta('meta[name="twitter:image"]') || meta('meta[property="twitter:image"]'),
    posters
  };
}

async function grabPageImages() {
  try {
    const [res] = await chrome.scripting.executeScript({target: {tabId}, func: grabPageMedia});
    const data = (res && res.result) || {og: '', posters: []};
    pageImages = {
      og: safeImageUrl(data.og, pageUrl),
      posters: (data.posters || [])
        .map((p) => ({poster: safeImageUrl(p.poster, pageUrl), src: p.src || ''}))
        .filter((p) => p.poster)
    };
  } catch (e) {
    // Вкладку закрыли, страница служебная или скрипт не пустили — не беда.
    pageImages = {og: '', posters: []};
  }
}

// safeImageUrl приводит адрес к абсолютному и пропускает только то, что
// вообще может быть картинкой: javascript: и прочее сюда попадать не должно.
function safeImageUrl(raw, base) {
  if (!raw) return '';
  if (/^data:image\//i.test(raw)) return raw.length <= 200000 ? raw : '';
  try {
    const u = new URL(raw, base || undefined);
    return (u.protocol === 'http:' || u.protocol === 'https:') ? u.href : '';
  } catch (e) {
    return '';
  }
}

// --- память о выборе ---

let prefHeight = 0;

async function loadPref() {
  try {
    const data = await chrome.storage.local.get('prefHeight');
    prefHeight = (data && parseInt(data.prefHeight, 10)) || 0;
  } catch (e) { /* не критично */ }
}

function rememberPref(h) {
  prefHeight = h | 0;
  chrome.storage.local.set({prefHeight}).catch(() => {});
}

// --- действия ---

async function download(btn) {
  btn.disabled = true;
  const old = btn.textContent;
  btn.textContent = '…';
  const key = btn.dataset.key;
  const res = await chrome.runtime.sendMessage(
    {action: 'download', tabId, key, height: choice[key] | 0});
  if (res && res.ok) {
    btn.textContent = 'В очереди';
    toast('Задача поставлена в очередь программы', 'ok');
  } else {
    btn.disabled = false;
    btn.textContent = old;
    toast((res && res.error) || 'Не удалось передать программе', 'err');
    refresh(true);
  }
}

async function refresh(force) {
  state = await chrome.runtime.sendMessage({action: 'state', tabId, force: force === true});
  render();
}

async function init() {
  const [tab] = await chrome.tabs.query({active: true, currentWindow: true});
  if (!tab || !/^https?:/i.test(tab.url || '')) {
    $('appState').className = 'chip';
    $('appState').textContent = '—';
    $('body').innerHTML = `<div class="empty">${ICON.empty}<b>Здесь расширение не работает</b>` +
      `<p>Служебные страницы браузера (chrome://, about:) и приватные окна не отслеживаются — так задумано.</p></div>`;
    $('all').disabled = true;
    $('clear').disabled = true;
    return;
  }
  tabId = tab.id;
  pageUrl = tab.url || '';
  // Метаданные страницы читаем сразу и один раз: вкладку могут закрыть,
  // пока попап открыт, и тогда второго шанса не будет.
  pagePromise = grabPageImages();
  await loadPref();

  $('all').addEventListener('click', async () => {
    $('all').disabled = true;
    $('all').textContent = 'Отправляю…';
    const res = await chrome.runtime.sendMessage({action: 'downloadAll', tabId, heights: choice});
    $('all').textContent = 'Скачать всё';
    if (res && res.ok) toast(`Поставлено в очередь: ${res.queued}`, 'ok');
    else toast((res && res.error) || 'Не удалось передать программе', 'err');
    refresh(true);
  });

  $('clear').addEventListener('click', async () => {
    await chrome.runtime.sendMessage({action: 'clear', tabId});
    for (const k of Object.keys(previews)) delete previews[k];
    refresh(true);
  });

  await refresh(true);
  // Пока popup открыт, страница может подгрузить ещё потоки.
  setInterval(() => refresh(false), 2000);
}

init();
