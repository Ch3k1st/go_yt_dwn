package main

// indexHTML — одностраничный интерфейс веб-оболочки.
// CSS и JS встроены, внешних зависимостей нет.
const indexHTML = `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Video Downloader</title>
<style>
  :root {
    --bg: #0d0f17;
    --panel: #161a26;
    --panel2: #1f2433;
    --text: #e6e9f0;
    --dim: #8a91a6;
    --cyan: #36c5d8;
    --green: #3fd17a;
    --yellow: #ecc94b;
    --purple: #a779e0;
    --red: #ef5d6f;
    --border: #2a3042;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; min-height: 100vh; background: var(--bg); color: var(--text);
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    display: flex; flex-direction: column; align-items: center; padding: 32px 16px;
  }
  .logo { color: var(--cyan); font-weight: 700; letter-spacing: 2px; font-size: 26px; }
  .logo .v { color: var(--red); }
  .sub { color: var(--dim); font-size: 12px; margin-top: 4px; letter-spacing: 3px; }
  .card {
    width: 100%; max-width: 680px; background: var(--panel);
    border: 1px solid var(--border); border-radius: 14px; padding: 22px; margin-top: 26px;
  }
  label { display: block; color: var(--dim); font-size: 12px; margin-bottom: 8px; text-transform: uppercase; letter-spacing: 1px; }
  .row { display: flex; gap: 10px; }
  input, select {
    width: 100%; background: var(--panel2); border: 1px solid var(--border); color: var(--text);
    padding: 12px 14px; border-radius: 9px; font-family: inherit; font-size: 14px; outline: none;
  }
  input:focus, select:focus { border-color: var(--cyan); }
  button {
    background: var(--cyan); color: #04222a; border: none; padding: 12px 20px; border-radius: 9px;
    font-family: inherit; font-weight: 700; font-size: 14px; cursor: pointer; white-space: nowrap;
    transition: filter .15s, opacity .15s;
  }
  button:hover { filter: brightness(1.08); }
  button:disabled { opacity: .45; cursor: not-allowed; }
  button.dl { background: var(--green); color: #052414; width: 100%; margin-top: 16px; padding: 14px; }
  .field { margin-bottom: 16px; }
  .info { display: none; margin-top: 22px; gap: 16px; }
  .info.show { display: flex; }
  .info img { width: 180px; border-radius: 9px; border: 1px solid var(--border); object-fit: cover; }
  .info .meta { flex: 1; min-width: 0; }
  .info .title { font-size: 16px; font-weight: 700; margin: 0 0 10px; line-height: 1.4; }
  .info .tags { display: flex; flex-wrap: wrap; gap: 8px; }
  .qrow { margin-top: 14px; }
  .qrow label { margin-bottom: 6px; }
  .qrow select { width: 100%; }
  .tag { background: var(--panel2); border: 1px solid var(--border); color: var(--dim);
    padding: 4px 10px; border-radius: 20px; font-size: 12px; }
  .tag b { color: var(--text); font-weight: 600; }
  .progress-wrap { display: none; margin-top: 20px; }
  .progress-wrap.show { display: block; }
  .bar { height: 12px; background: var(--panel2); border-radius: 8px; overflow: hidden; border: 1px solid var(--border); }
  .bar > span { display: block; height: 100%; width: 0%; background: linear-gradient(90deg, var(--cyan), var(--purple)); transition: width .25s; }
  .pstats { display: flex; justify-content: space-between; color: var(--dim); font-size: 12px; margin-top: 8px; }
  .status { margin-top: 14px; font-size: 13px; min-height: 18px; }
  .status.err { color: var(--red); }
  .status.ok { color: var(--green); }
  .status.work { color: var(--yellow); }
  .hint { color: var(--dim); font-size: 12px; margin-top: 10px; }
  .spinner { display: inline-block; width: 12px; height: 12px; border: 2px solid var(--border);
    border-top-color: var(--cyan); border-radius: 50%; animation: spin .7s linear infinite; vertical-align: middle; }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (max-width: 520px) { .info { flex-direction: column; } .info img { width: 100%; } }
</style>
</head>
<body>
  <div class="logo"><span class="v">▶</span> VIDEO DOWNLOADER</div>
  <div class="sub">by Ch3kL1st</div>

  <div class="card">
    <div class="field">
      <label>Источник cookies (обход ограничений YouTube)</label>
      <select id="cookie"><option value="-1">Без cookies</option></select>
    </div>

    <div class="field">
      <label>Ссылка на видео</label>
      <div class="row">
        <input id="url" type="text" placeholder="https://www.youtube.com/watch?v=..." autocomplete="off">
        <button id="check">Анализ</button>
      </div>
    </div>

    <div class="info" id="info">
      <img id="thumb" alt="">
      <div class="meta">
        <p class="title" id="title"></p>
        <div class="tags" id="tags"></div>
        <div class="qrow" id="qrow">
          <label for="quality">Качество</label>
          <select id="quality"></select>
        </div>
        <button class="dl" id="download">⬇ Скачать в downloads/</button>
      </div>
    </div>

    <div class="progress-wrap" id="pwrap">
      <div class="bar"><span id="barfill"></span></div>
      <div class="pstats">
        <span id="ppercent">0%</span>
        <span id="pspeed"></span>
        <span id="peta"></span>
      </div>
    </div>

    <div class="status" id="status"></div>
    <div class="hint">Файлы сохраняются в папку <b>downloads/</b> рядом с программой.</div>
  </div>

<script>
  var elUrl = document.getElementById('url');
  var elCookie = document.getElementById('cookie');
  var elCheck = document.getElementById('check');
  var elInfo = document.getElementById('info');
  var elThumb = document.getElementById('thumb');
  var elTitle = document.getElementById('title');
  var elTags = document.getElementById('tags');
  var elQuality = document.getElementById('quality');
  var elDownload = document.getElementById('download');
  var elStatus = document.getElementById('status');
  var elPwrap = document.getElementById('pwrap');
  var elBar = document.getElementById('barfill');
  var elPercent = document.getElementById('ppercent');
  var elSpeed = document.getElementById('pspeed');
  var elEta = document.getElementById('peta');
  var es = null;

  function setStatus(text, cls) {
    elStatus.className = 'status' + (cls ? ' ' + cls : '');
    elStatus.innerHTML = text;
  }

  function loadBrowsers() {
    fetch('/api/browsers').then(function(r){ return r.json(); }).then(function(list){
      list.forEach(function(b){
        var o = document.createElement('option');
        o.value = b.idx;
        o.textContent = b.display;
        elCookie.appendChild(o);
      });
      if (list.length > 0) { elCookie.value = list[0].idx; }
    }).catch(function(){});
  }

  function tag(label, value) {
    if (!value) return '';
    return '<span class="tag">' + label + ': <b>' + value + '</b></span>';
  }

  function fmtViews(n) {
    if (!n) return '';
    return n.toLocaleString('ru-RU');
  }

  function checkInfo() {
    var url = elUrl.value.trim();
    if (!url) { setStatus('Вставьте ссылку на видео.', 'err'); return; }
    elInfo.classList.remove('show');
    elPwrap.classList.remove('show');
    setStatus('<span class="spinner"></span> Получаю информацию о видео...', 'work');
    elCheck.disabled = true;

    fetch('/api/info', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url: url, cookie: parseInt(elCookie.value, 10) })
    }).then(function(r){ return r.json().then(function(d){ return { ok: r.ok, d: d }; }); })
      .then(function(res){
        elCheck.disabled = false;
        if (!res.ok) { setStatus(res.d.error || 'Ошибка', 'err'); return; }
        var d = res.d;
        elTitle.textContent = d.title || 'Без названия';
        if (d.thumbnail) { elThumb.src = d.thumbnail; elThumb.style.display = ''; }
        else { elThumb.style.display = 'none'; }
        elTags.innerHTML =
          tag('Канал', d.channel || d.uploader) +
          tag('Длительность', d.duration_string) +
          tag('Просмотры', fmtViews(d.view_count)) +
          tag('Источник', d.extractor_key);
        elQuality.innerHTML = '';
        (d.qualities || []).forEach(function(q){
          var o = document.createElement('option');
          o.value = q.value;
          o.textContent = q.label;
          elQuality.appendChild(o);
        });
        elInfo.classList.add('show');
        setStatus('');
      }).catch(function(){
        elCheck.disabled = false;
        setStatus('Сетевая ошибка при запросе информации.', 'err');
      });
  }

  function startDownload() {
    var url = elUrl.value.trim();
    if (!url) return;
    if (es) { es.close(); }
    elDownload.disabled = true;
    elPwrap.classList.add('show');
    elBar.style.width = '0%';
    elPercent.textContent = '0%';
    elSpeed.textContent = '';
    elEta.textContent = '';
    setStatus('<span class="spinner"></span> Скачивание началось...', 'work');

    var quality = elQuality.value || 'best';
    var q = '/api/download?url=' + encodeURIComponent(url) + '&cookie=' + encodeURIComponent(elCookie.value) + '&quality=' + encodeURIComponent(quality);
    es = new EventSource(q);

    es.addEventListener('progress', function(e){
      var p = JSON.parse(e.data);
      var pct = (p.percent || '').replace('%','').trim();
      if (pct) { elBar.style.width = pct + '%'; elPercent.textContent = p.percent; }
      elSpeed.textContent = p.speed ? 'скорость: ' + p.speed : '';
      elEta.textContent = p.eta ? 'ETA: ' + p.eta : '';
    });

    es.addEventListener('done', function(e){
      var d = JSON.parse(e.data);
      elBar.style.width = '100%';
      elPercent.textContent = '100%';
      var name = d.file ? d.file.split('/').pop() : '';
      setStatus('✓ Готово! Файл сохранён' + (name ? ': <b>' + name + '</b>' : ' в downloads/'), 'ok');
      elDownload.disabled = false;
      es.close();
    });

    es.addEventListener('error', function(e){
      if (e.data) { setStatus('✗ ' + e.data, 'err'); }
      else { setStatus('✗ Соединение прервано.', 'err'); }
      elDownload.disabled = false;
      if (es) { es.close(); }
    });
  }

  elCheck.addEventListener('click', checkInfo);
  elUrl.addEventListener('keydown', function(e){ if (e.key === 'Enter') checkInfo(); });
  elDownload.addEventListener('click', startDownload);
  loadBrowsers();
</script>
</body>
</html>`
