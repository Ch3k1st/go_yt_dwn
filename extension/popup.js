// Popup: показывает найденное на текущей вкладке и просит service worker
// отправить выбранное в программу. Своей сетевой логики здесь нет.

const $ = (id) => document.getElementById(id);
const PROTOCOL = 1;

let tabId = null;
let state = null;

const ICON = {
  drm: '<svg class="icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="4" y="10" width="16" height="10" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/></svg>',
  warn: '<svg class="icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 3 2 20h20L12 3z"/><path d="M12 9v5m0 3v.5"/></svg>',
  info: '<svg class="icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M12 11v5m0-8.5v.5"/></svg>',
  empty: '<svg class="icon" width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><rect x="2" y="5" width="20" height="14" rx="2"/><path d="m10 9 5 3-5 3z"/></svg>'
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

const KIND_LABEL = {hls: 'HLS', dash: 'DASH', file: 'ФАЙЛ'};

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

function render() {
  const banners = [];
  const body = $('body');

  if (!state) return;

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
    const tags = [`<span class="tag kind">${KIND_LABEL[it.kind] || esc(it.ext.toUpperCase())}</span>`];
    if (it.ext && it.kind === 'file') tags.push(`<span class="tag">${esc(it.ext)}</span>`);
    if (it.quality) tags.push(`<span class="tag">${esc(it.quality)}</span>`);
    if (it.variants > 1) tags.push(`<span class="tag">${it.variants} вар.</span>`);
    if (it.size) tags.push(`<span class="tag">${humanSize(it.size)}</span>`);
    if (it.duration) tags.push(`<span class="tag">${humanTime(it.duration)}</span>`);
    if (it.headers && it.headers.cookie) tags.push('<span class="tag" title="Передадим куки и Referer">с куками</span>');
    if (it.drm) tags.push('<span class="tag drm">DRM</span>');

    const action = it.drm
      ? '<span class="chip err" title="Обход защиты не поддерживается">защищено</span>'
      : `<button class="btn btn-primary dl" data-key="${esc(it.key)}">Скачать</button>`;

    return `<div class="item${it.drm ? ' blocked' : ''}">
        <div class="body">
          <div class="name" title="${esc(it.url)}">${esc(it.name)}</div>
          <div class="sub">${tags.join('')}</div>
        </div>${action}
      </div>`;
  });

  body.innerHTML = `<div class="list">${rows.join('')}</div>`;
  $('all').disabled = !items.some((i) => !i.drm) || !state.app;

  for (const btn of body.querySelectorAll('.dl')) {
    btn.addEventListener('click', () => download(btn));
  }
}

async function download(btn) {
  btn.disabled = true;
  const old = btn.textContent;
  btn.textContent = '…';
  const res = await chrome.runtime.sendMessage({action: 'download', tabId, key: btn.dataset.key});
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

  $('all').addEventListener('click', async () => {
    $('all').disabled = true;
    $('all').textContent = 'Отправляю…';
    const res = await chrome.runtime.sendMessage({action: 'downloadAll', tabId});
    $('all').textContent = 'Скачать всё';
    if (res && res.ok) toast(`Поставлено в очередь: ${res.queued}`, 'ok');
    else toast((res && res.error) || 'Не удалось передать программе', 'err');
    refresh(true);
  });

  $('clear').addEventListener('click', async () => {
    await chrome.runtime.sendMessage({action: 'clear', tabId});
    refresh(false);
  });

  await refresh(true);
  // Пока popup открыт, страница может подгрузить ещё потоки.
  setInterval(() => refresh(false), 2000);
}

init();
