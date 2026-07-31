package main

// indexHTML — одностраничный интерфейс веб-оболочки и нативных приложений.
// CSS и JS встроены, внешних зависимостей нет: программа обязана работать
// офлайн, поэтому ни CDN, ни веб-шрифтов здесь быть не может.
//
// Дизайн собран по базе ui-ux-pro-max (своей дизайн-системы у проекта нет):
//   стиль    — Flat Design + Minimalism (products.csv → «File Manager & Transfer»),
//              вторичные: Accessible & Ethical, Dark Mode (OLED);
//              следствие — ни теней, ни градиентов, цвет отвечает за иерархию;
//   палитра  — colors.csv: светлая тема «File Manager & Transfer» (folder blue
//              #2563EB + file amber #D97706), тёмная «Developer Tool / IDE»
//              (#0F172A + run green #22C55E). Обе на нейтрали slate — семейство
//              общее, поэтому темы переключаются без смены характера;
//   шрифты   — typography.csv, пара «Dashboard Data» (Fira Sans + Fira Code):
//              sans для подписей, mono для данных. Веб-шрифты запрещены, поэтому
//              взяты системные аналоги той же роли (SF Pro / Segoe UI и
//              SF Mono / Cascadia Mono) — распределение ролей сохранено;
//   моушн    — motion.csv: hover 150-200 мс ease-out, появление списка
//              250-350 мс со сдвигом 8px; всё гасится prefers-reduced-motion.
const indexHTML = `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Video Downloader</title>
<style>
  /* ---- Токены: светлая тема (colors.csv → File Manager & Transfer) ---- */
  :root, :root[data-theme="light"] {
    color-scheme: light;
    --bg: #F8FAFC;
    --surface: #FFFFFF;
    --surface-2: #F1F5FD;
    --fg: #0F172A;
    --fg-muted: #64748B;
    --line: #E4ECFC;
    --line-strong: #64748B;   /* границы полей: 4.76:1 к surface, WCAG 1.4.11 */
    --primary: #2563EB;
    --on-primary: #FFFFFF;
    --success: #059669;
    --on-success: #0F172A;
    --warn: #D97706;
    --on-warn: #0F172A;
    --danger: #DC2626;
    --on-danger: #FFFFFF;
    --shade: rgba(15, 23, 42, .06);
  }
  /* ---- Токены: тёмная тема (colors.csv → Developer Tool / IDE) ---- */
  @media (prefers-color-scheme: dark) {
    :root:not([data-theme="light"]) {
      color-scheme: dark;
      --bg: #0F172A;
      --surface: #1B2336;
      --surface-2: #272F42;
      --fg: #F8FAFC;
      --fg-muted: #94A3B8;
      --line: #475569;
      --line-strong: #94A3B8;
      --primary: #3B82F6;
      --on-primary: #0F172A;
      --success: #22C55E;
      --on-success: #0F172A;
      --warn: #F97316;
      --on-warn: #0F172A;
      --danger: #EF4444;
      --on-danger: #0F172A;
      --shade: rgba(248, 250, 252, .07);
    }
  }
  :root[data-theme="dark"] {
    color-scheme: dark;
    --bg: #0F172A;
    --surface: #1B2336;
    --surface-2: #272F42;
    --fg: #F8FAFC;
    --fg-muted: #94A3B8;
    --line: #475569;
    --line-strong: #94A3B8;
    --primary: #3B82F6;
    --on-primary: #0F172A;
    --success: #22C55E;
    --on-success: #0F172A;
    --warn: #F97316;
    --on-warn: #0F172A;
    --danger: #EF4444;
    --on-danger: #0F172A;
    --shade: rgba(248, 250, 252, .07);
  }

  :root {
    /* Шаг сетки 4pt (styles.csv → Flat Design) */
    --s1: 4px;  --s2: 8px;  --s3: 12px; --s4: 16px;
    --s5: 24px; --s6: 32px; --s7: 48px;
    --r-ctl: 6px;   /* Flat: скругления минимальные */
    --r-card: 10px;
    --font-ui: -apple-system, BlinkMacSystemFont, "Segoe UI Variable Text",
               "Segoe UI", Roboto, "Noto Sans", system-ui, sans-serif;
    --font-mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo,
                 "Cascadia Mono", Consolas, "Liberation Mono", monospace;
    --dur: .16s;
  }

  * { box-sizing: border-box; }
  html, body { height: 100%; }
  body {
    margin: 0; background: var(--bg); color: var(--fg);
    font: 14px/1.5 var(--font-ui);
    -webkit-font-smoothing: antialiased;
  }
  ::selection { background: var(--primary); color: var(--on-primary); }

  /* Фокус виден всегда и толще 2px — WCAG 2.2, 2.4.11 Focus Appearance */
  :focus-visible {
    outline: 2px solid var(--primary);
    outline-offset: 2px;
    border-radius: var(--r-ctl);
  }

  .app { display: flex; min-height: 100vh; }

  /* ---------- Боковая панель ---------- */
  .rail {
    width: 232px; flex: none; background: var(--surface);
    border-right: 1px solid var(--line);
    display: flex; flex-direction: column; gap: var(--s1);
    padding: var(--s4) var(--s3);
    position: sticky; top: 0; height: 100vh;
  }
  .brand {
    display: flex; align-items: center; gap: var(--s2);
    padding: var(--s2) var(--s2) var(--s4);
  }
  .brand .mark {
    width: 30px; height: 30px; flex: none; border-radius: var(--r-ctl);
    background: var(--primary); color: var(--on-primary);
    display: grid; place-items: center;
  }
  .brand b { font-size: 15px; font-weight: 700; letter-spacing: -.01em; }
  .brand span { display: block; font-size: 11px; color: var(--fg-muted); }

  .navbtn {
    display: flex; align-items: center; gap: var(--s3);
    width: 100%; min-height: 40px; padding: 0 var(--s3);
    border: none; border-radius: var(--r-ctl); background: transparent;
    color: var(--fg); font: 500 14px/1 var(--font-ui); text-align: left;
    cursor: pointer; transition: background var(--dur) ease-out;
  }
  .navbtn:hover { background: var(--shade); }
  .navbtn[aria-current="page"] { background: var(--primary); color: var(--on-primary); }
  .navbtn .count {
    margin-left: auto; min-width: 22px; height: 20px; padding: 0 6px;
    border-radius: 10px; background: var(--surface-2); color: var(--fg);
    font: 600 11px/20px var(--font-mono); text-align: center;
  }
  .navbtn[aria-current="page"] .count { background: var(--on-primary); color: var(--primary); }
  .rail .spacer { flex: 1; }

  /* ---------- Основная область ---------- */
  main { flex: 1; min-width: 0; padding: var(--s5) var(--s6) var(--s7); }
  /* Колонка контента ограничена: на широком окне строки иначе растягиваются
     до нечитаемой длины (ux-guidelines: Line Length). */
  main > * { max-width: 940px; }
  .head { margin-bottom: var(--s5); }
  .head h1 { margin: 0; font-size: 22px; font-weight: 700; letter-spacing: -.02em; }
  .head p { margin: var(--s1) 0 0; color: var(--fg-muted); font-size: 13px; max-width: 70ch; }
  /* Второй абзац — отдельная мысль, а не продолжение первого: 4px их склеивают. */
  .head p + p { margin-top: var(--s2); }
  .pane { display: none; }
  .pane.active { display: block; animation: rise .28s ease-out; }
  /* Появление двигает только transform и НЕ трогает opacity. Причина не
     эстетическая: WebKit останавливает анимации в окне, которое сейчас не
     видно (свёрнуто, перекрыто, приложение в фоне). Анимация с opacity: 0 в
     первом кадре в этот момент замирает — и содержимое остаётся невидимым уже
     после того, как окно снова показали. Сдвиг на 8px такого не устроит. */
  @keyframes rise { from { transform: translateY(8px); } }

  .card {
    background: var(--surface); border: 1px solid var(--line);
    border-radius: var(--r-card); padding: var(--s5); margin-bottom: var(--s4);
  }
  .card > h2 {
    margin: 0 0 var(--s4); font-size: 12px; font-weight: 700;
    letter-spacing: .08em; text-transform: uppercase; color: var(--fg-muted);
  }

  /* ---------- Поля ---------- */
  .field { margin-bottom: var(--s4); }
  .field:last-child { margin-bottom: 0; }
  label, .lbl {
    display: block; margin-bottom: 6px;
    font-size: 12px; font-weight: 600; color: var(--fg-muted);
  }
  input[type=text], select, textarea {
    width: 100%; min-height: 40px; padding: 9px var(--s3);
    background: var(--surface); color: var(--fg);
    border: 1px solid var(--line-strong); border-radius: var(--r-ctl);
    font: 14px/1.4 var(--font-ui); outline: none;
    transition: border-color var(--dur) ease-out;
  }
  select { appearance: none; padding-right: var(--s6);
    background-image: linear-gradient(transparent, transparent); }
  .sel { position: relative; }
  .sel::after {
    content: ""; position: absolute; right: 14px; top: 50%; width: 8px; height: 8px;
    border-right: 2px solid var(--fg-muted); border-bottom: 2px solid var(--fg-muted);
    transform: translateY(-70%) rotate(45deg); pointer-events: none;
  }
  input[type=text]:focus, select:focus, textarea:focus { border-color: var(--primary); }
  input[type=text]::placeholder { color: var(--fg-muted); }
  .row { display: flex; gap: var(--s2); align-items: flex-end; }
  .row > .grow { flex: 1; min-width: 0; }
  .grid3 { display: grid; grid-template-columns: repeat(3, 1fr); gap: var(--s3); }

  /* ---------- Кнопки ---------- */
  .btn {
    display: inline-flex; align-items: center; justify-content: center; gap: var(--s2);
    min-height: 40px; padding: 0 var(--s4);
    border: 1px solid transparent; border-radius: var(--r-ctl);
    background: var(--surface-2); color: var(--fg);
    font: 600 14px/1 var(--font-ui); cursor: pointer; white-space: nowrap;
    transition: opacity var(--dur) ease-out, background var(--dur) ease-out;
  }
  .btn:hover { opacity: .88; }
  .btn:active { transform: translateY(1px); }
  .btn:disabled { opacity: .45; cursor: not-allowed; transform: none; }
  .btn-primary { background: var(--primary); color: var(--on-primary); }
  .btn-danger  { background: var(--danger);  color: var(--on-danger); }
  .btn-ghost   { background: transparent; border-color: var(--line-strong); color: var(--fg); }
  .btn-wide { width: 100%; }
  /* Иконочные кнопки: минимум 32×32 — WCAG 2.2, 2.5.8 Target Size */
  .btn-icon {
    width: 32px; height: 32px; min-height: 32px; padding: 0; flex: none;
    background: transparent; border-color: var(--line-strong); color: var(--fg);
  }
  .btn-icon:hover { background: var(--shade); opacity: 1; }

  /* ---------- Плашки статусов ---------- */
  .chip {
    display: inline-flex; align-items: center; gap: 6px;
    height: 22px; padding: 0 var(--s2); border-radius: 11px;
    font: 600 11px/1 var(--font-ui); letter-spacing: .02em; white-space: nowrap;
    background: var(--surface-2); color: var(--fg);
  }
  .chip.info    { background: var(--primary); color: var(--on-primary); }
  .chip.ok      { background: var(--success); color: var(--on-success); }
  .chip.work    { background: var(--warn);    color: var(--on-warn); }
  .chip.err     { background: var(--danger);  color: var(--on-danger); }

  /* ---------- Баннеры состояний ---------- */
  .banner {
    display: flex; align-items: flex-start; gap: var(--s3);
    padding: var(--s3) var(--s4); border-radius: var(--r-card);
    margin-bottom: var(--s4); background: var(--surface-2); color: var(--fg);
  }
  .banner.err  { background: var(--danger);  color: var(--on-danger); }
  .banner.warn { background: var(--warn);    color: var(--on-warn); }
  .banner.info { background: var(--primary); color: var(--on-primary); }
  .banner .txt { flex: 1; min-width: 0; font-size: 13px; }
  .banner .txt b { display: block; font-size: 14px; margin-bottom: 2px; }
  /* Кнопка на цветной плашке — инверсия плашки: фон её текстом, текст её фоном. */
  .banner .btn { border: none; }
  .banner.err .btn  { background: var(--on-danger);  color: var(--danger); }
  .banner.warn .btn { background: var(--on-warn);    color: var(--warn); }
  .banner.info .btn { background: var(--on-primary); color: var(--primary); }
  .banner .btn-icon { background: transparent; color: inherit; border-color: currentColor; }
  .banner .btn-icon:hover { background: transparent; opacity: .7; }
  .banner .icon { flex: none; margin-top: 1px; }

  /* ---------- Пустые состояния ---------- */
  .empty { text-align: center; padding: var(--s7) var(--s4); color: var(--fg-muted); }
  .empty .icon { color: var(--line-strong); margin-bottom: var(--s3); }
  .empty b { display: block; color: var(--fg); font-size: 15px; margin-bottom: var(--s1); }
  .empty p { margin: 0 auto; max-width: 46ch; font-size: 13px; }

  /* ---------- Скелетоны загрузки ---------- */
  .skel { border-radius: var(--r-ctl); background: var(--surface-2); animation: pulse 1.2s ease-in-out infinite; }
  @keyframes pulse { 50% { opacity: .5; } }
  .skel-row { display: flex; gap: var(--s4); }
  .skel-thumb { width: 192px; height: 108px; flex: none; }
  .skel-line { height: 12px; margin-bottom: var(--s2); }

  /* ---------- Карточка видео ---------- */
  .preview { display: flex; gap: var(--s4); }
  .preview img {
    width: 192px; height: 108px; flex: none; object-fit: cover;
    border-radius: var(--r-ctl); background: var(--surface-2);
  }
  .preview .meta { flex: 1; min-width: 0; }
  .preview .title {
    margin: 0 0 var(--s2); font-size: 16px; font-weight: 700; line-height: 1.35;
    display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
  }
  .tags { display: flex; flex-wrap: wrap; gap: 6px; }
  .tag {
    font: 500 12px/1 var(--font-ui); color: var(--fg-muted);
    background: var(--surface-2); border-radius: var(--r-ctl); padding: 5px var(--s2);
  }
  .tag b { color: var(--fg); font-weight: 600; font-family: var(--font-mono); }

  /* ---------- Список задач и истории ---------- */
  .list { display: flex; flex-direction: column; gap: var(--s2); }
  .item {
    display: flex; align-items: center; gap: var(--s3);
    padding: var(--s3); background: var(--surface);
    border: 1px solid var(--line); border-radius: var(--r-card);
    animation: rise .3s ease-out both;
  }
  .item .body { flex: 1; min-width: 0; }
  .item .name {
    font-weight: 600; font-size: 14px;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .item .sub {
    margin-top: 2px; font: 12px/1.4 var(--font-mono); color: var(--fg-muted);
    font-variant-numeric: tabular-nums;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .item .acts { display: flex; gap: 6px; flex: none; }
  .item .thumb { width: 64px; height: 36px; flex: none; object-fit: cover;
    border-radius: 4px; background: var(--surface-2); }

  /* ---------- Прогресс ---------- */
  .bar { height: 6px; border-radius: 3px; background: var(--surface-2); overflow: hidden; margin-top: var(--s2); }
  .bar > i { display: block; height: 100%; width: 0; background: var(--primary);
    transition: width .25s ease-out; }
  .bar.ok > i   { background: var(--success); }
  .bar.work > i { background: var(--warn); }
  .bar.err > i  { background: var(--danger); }
  /* Неопределённый прогресс: yt-dlp молчит первые секунды, процентов ещё нет.
     Пульсация вместо бегущего блока — на любом кадре видно, что полоса живая,
     и не появляется ощущение сломанной вёрстки. */
  .bar.indet > i { width: 100%; opacity: .35; animation: pulse 1.2s ease-in-out infinite; }

  /* ---------- Пронумерованная инструкция ---------- */
  /* Установка расширения — единственное место, где пользователь работает
     руками в чужой программе. Номера показывают, что это последовательность,
     а не набор советов (ux-guidelines: Progress Indicators). */
  .steps { margin: 0; padding: 0; list-style: none; counter-reset: step; }
  .steps li {
    position: relative; counter-increment: step;
    min-height: 24px; padding: 0 0 var(--s3) var(--s6);
  }
  .steps li:last-child { padding-bottom: 0; }
  .steps li::before {
    content: counter(step);
    position: absolute; left: 0; top: 0; width: 24px; height: 24px;
    border-radius: 12px; background: var(--primary); color: var(--on-primary);
    font: 700 12px/24px var(--font-mono); text-align: center;
  }

  /* ---------- Путь или адрес, который придётся скопировать ---------- */
  .pathline {
    display: flex; align-items: center; gap: var(--s2);
    margin-top: var(--s2); padding: var(--s2) var(--s3);
    background: var(--surface-2); border-radius: var(--r-ctl);
    font: 12px/1.5 var(--font-mono);
  }
  .pathline > span { flex: 1; min-width: 0; word-break: break-all; }

  /* ---------- Заголовок вложенной секции списка ---------- */
  .subhead {
    display: flex; align-items: center; gap: var(--s2);
    margin: var(--s5) 0 var(--s3);
    font-size: 12px; font-weight: 700; letter-spacing: .08em;
    text-transform: uppercase; color: var(--fg-muted);
  }

  /* ---------- Зона перетаскивания ---------- */
  .drop {
    border: 2px dashed var(--line-strong); border-radius: var(--r-card);
    padding: var(--s5); text-align: center; color: var(--fg-muted);
    transition: border-color var(--dur) ease-out, background var(--dur) ease-out;
  }
  .drop.hot { border-color: var(--primary); background: var(--surface-2); }
  .drop b { display: block; color: var(--fg); font-size: 14px; margin-bottom: var(--s1); }
  .drop .picked {
    display: inline-flex; align-items: center; gap: var(--s2); max-width: 100%;
    margin-top: var(--s3); padding: 6px var(--s3); border-radius: var(--r-ctl);
    background: var(--surface-2); color: var(--fg);
    font: 12px var(--font-mono); word-break: break-all; text-align: left;
  }

  /* ---------- Превью текста ---------- */
  .preview-text {
    margin: 0; padding: var(--s3); max-height: 260px; overflow: auto;
    background: var(--surface-2); border-radius: var(--r-ctl);
    font: 13px/1.6 var(--font-mono); white-space: pre-wrap; word-break: break-word;
  }
  .hint { color: var(--fg-muted); font-size: 12px; margin-top: var(--s3); }
  .hint b { color: var(--fg); }
  .mono { font-family: var(--font-mono); font-variant-numeric: tabular-nums; }
  .sr { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }

  .toolbar { display: flex; gap: var(--s2); margin-bottom: var(--s4); flex-wrap: wrap; }
  .toolbar .spacer { flex: 1; }

  /* ---------- Адаптив: окно уже 900px — панель уходит наверх ---------- */
  @media (max-width: 900px) {
    .app { flex-direction: column; }
    .rail { width: auto; height: auto; position: static; flex-direction: row;
      align-items: center; overflow-x: auto; border-right: none; border-bottom: 1px solid var(--line); }
    .brand { padding: 0 var(--s3) 0 0; }
    .brand .txt { display: none; }
    .navbtn { width: auto; }
    .navbtn .count { margin-left: var(--s1); }
    main { padding: var(--s4); }
    .grid3 { grid-template-columns: 1fr; }
    .preview { flex-direction: column; }
    .preview img { width: 100%; height: auto; aspect-ratio: 16 / 9; }
  }

  @media (prefers-reduced-motion: reduce) {
    *, *::before, *::after {
      animation-duration: .001ms !important; animation-iteration-count: 1 !important;
      transition-duration: .001ms !important;
    }
  }
</style>
</head>
<body>

<svg class="sr" aria-hidden="true"><defs>
  <symbol id="i-dl" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round" stroke-linejoin="round">
    <path d="M12 3v12m0 0 4-4m-4 4-4-4M4 19h16"/></symbol>
  <symbol id="i-queue" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round"><path d="M4 6h16M4 12h16M4 18h10"/></symbol>
  <symbol id="i-hist" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round" stroke-linejoin="round">
    <circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></symbol>
  <symbol id="i-text" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round" stroke-linejoin="round">
    <rect x="3" y="5" width="18" height="14" rx="2"/><path d="M7 10h6M7 14h10"/></symbol>
  <symbol id="i-play" viewBox="0 0 24 24" fill="currentColor"><path d="M8 5.5v13l11-6.5z"/></symbol>
  <symbol id="i-x" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round"><path d="M6 6l12 12M18 6L6 18"/></symbol>
  <symbol id="i-retry" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round" stroke-linejoin="round">
    <path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 5v6h-6"/></symbol>
  <symbol id="i-folder" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round" stroke-linejoin="round">
    <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></symbol>
  <symbol id="i-trash" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round" stroke-linejoin="round">
    <path d="M4 7h16M9 7V5h6v2M6 7l1 12h10l1-12"/></symbol>
  <symbol id="i-alert" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round" stroke-linejoin="round">
    <path d="M12 4 2.5 20h19z"/><path d="M12 10v4M12 17h.01"/></symbol>
  <symbol id="i-sun" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round"><circle cx="12" cy="12" r="4"/>
    <path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M19.1 4.9l-1.4 1.4M6.3 17.7l-1.4 1.4"/></symbol>
  <symbol id="i-moon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round" stroke-linejoin="round"><path d="M20 14A8.5 8.5 0 0 1 10 4a8.5 8.5 0 1 0 10 10z"/></symbol>
  <symbol id="i-auto" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round"><circle cx="12" cy="12" r="8"/><path d="M12 4v16" fill="none"/>
    <path d="M12 4a8 8 0 0 1 0 16z" fill="currentColor" stroke="none"/></symbol>
  <symbol id="i-file" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round" stroke-linejoin="round">
    <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/></symbol>
  <symbol id="i-check" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round" stroke-linejoin="round"><path d="M4 12.5 9.5 18 20 6"/></symbol>
  <symbol id="i-ext" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round" stroke-linejoin="round">
    <path d="M4 5a1 1 0 0 1 1-1h4.5a2.5 2.5 0 0 1 5 0H19a1 1 0 0 1 1 1v4.5a2.5 2.5 0 0 1 0 5V19a1 1 0 0 1-1 1h-4.5a2.5 2.5 0 0 0-5 0H5a1 1 0 0 1-1-1v-4.5a2.5 2.5 0 0 0 0-5z"/></symbol>
  <symbol id="i-copy" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
    stroke-linecap="round" stroke-linejoin="round">
    <rect x="9" y="9" width="12" height="12" rx="2"/>
    <path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1"/></symbol>
</defs></svg>

<div class="app">
  <aside class="rail">
    <div class="brand">
      <span class="mark"><svg width="18" height="18" aria-hidden="true"><use href="#i-play"/></svg></span>
      <span class="txt"><b>Video Downloader</b><span>yt-dlp + FFmpeg</span></span>
    </div>

    <nav id="nav" aria-label="Разделы">
      <button class="navbtn" data-go="download" aria-current="page" type="button">
        <svg width="18" height="18" aria-hidden="true"><use href="#i-dl"/></svg>Скачать</button>
      <button class="navbtn" data-go="queue" type="button">
        <svg width="18" height="18" aria-hidden="true"><use href="#i-queue"/></svg>Очередь
        <span class="count" id="qcount" hidden>0</span></button>
      <button class="navbtn" data-go="history" type="button">
        <svg width="18" height="18" aria-hidden="true"><use href="#i-hist"/></svg>История
        <span class="count" id="hcount" hidden>0</span></button>
      <button class="navbtn" data-go="text" type="button">
        <svg width="18" height="18" aria-hidden="true"><use href="#i-text"/></svg>Транскрибация</button>
      <button class="navbtn" data-go="ext" type="button">
        <svg width="18" height="18" aria-hidden="true"><use href="#i-ext"/></svg>Расширение</button>
    </nav>

    <div class="spacer"></div>
    <button class="navbtn" id="theme" type="button" title="Оформление">
      <svg width="18" height="18" aria-hidden="true"><use id="themeicon" href="#i-auto"/></svg>
      <span id="themetext">Как в системе</span></button>
  </aside>

  <main>
    <div id="banners"></div>

    <!-- ================= Скачать ================= -->
    <section class="pane active" id="pane-download">
      <div class="head">
        <h1>Скачать видео</h1>
        <p>Вставьте ссылку, посмотрите, что нашлось, и отправьте в очередь.
           Поддерживается всё, что умеет yt-dlp: YouTube, Vimeo, VK и сотни других сайтов.</p>
      </div>

      <div class="card">
        <div class="field">
          <label for="url">Ссылка на видео</label>
          <div class="row">
            <div class="grow"><input id="url" type="text" autocomplete="off" spellcheck="false"
              placeholder="https://www.youtube.com/watch?v=..."></div>
            <button class="btn btn-primary" id="check" type="button">Анализ</button>
          </div>
        </div>
        <div class="field">
          <label for="cookie">Источник cookies</label>
          <div class="sel"><select id="cookie"><option value="-1">Без cookies</option></select></div>
          <div class="hint">Нужен, если сайт просит подтвердить, что вы не робот.</div>
        </div>
      </div>

      <div id="dlbody"></div>
    </section>

    <!-- ================= Очередь ================= -->
    <section class="pane" id="pane-queue">
      <div class="head">
        <h1>Очередь загрузок</h1>
        <p>Одновременно качаются две задачи, остальные ждут. Загрузку можно отменить и повторить.</p>
      </div>
      <div class="toolbar">
        <button class="btn btn-ghost" id="qclear" type="button">
          <svg width="16" height="16" aria-hidden="true"><use href="#i-trash"/></svg>Убрать завершённые</button>
      </div>
      <div id="qbody"></div>
    </section>

    <!-- ================= История ================= -->
    <section class="pane" id="pane-history">
      <div class="head">
        <h1>История</h1>
        <p>Последние 200 скачанных файлов. Список хранится только на этом компьютере.</p>
      </div>
      <div class="toolbar">
        <button class="btn btn-ghost" id="hopen" type="button">
          <svg width="16" height="16" aria-hidden="true"><use href="#i-folder"/></svg>Открыть папку загрузок</button>
        <div class="spacer"></div>
        <button class="btn btn-ghost" id="hclear" type="button">
          <svg width="16" height="16" aria-hidden="true"><use href="#i-trash"/></svg>Очистить историю</button>
      </div>
      <div id="hbody"></div>
    </section>

    <!-- ================= Транскрибация ================= -->
    <section class="pane" id="pane-text">
      <div class="head">
        <h1>Транскрибация</h1>
        <p>Локальное распознавание речи через whisper. Интернет не нужен, файлы никуда не уходят.</p>
      </div>
      <div id="tbody"></div>
    </section>

    <!-- ================= Расширение ================= -->
    <section class="pane" id="pane-ext">
      <div class="head">
        <h1>Расширение для браузера</h1>
        <p>yt-dlp разбирает сайт снаружи и спотыкается там, где нужен вход или где ссылка
           подписана под конкретную страницу. Расширение смотрит изнутри браузера: берёт
           настоящий адрес плеера вместе с куками и заголовками — поэтому качается и то,
           на чём обычная ссылка отвечает «403».</p>
        <p>Установить его молча программа не может: браузеры это запрещают намеренно.
           Остаются три действия вручную — открыть страницу расширений, включить
           «Режим разработчика» и указать папку. Страницу и папку программа откроет сама.</p>
      </div>
      <div id="extbody"></div>
    </section>
  </main>
</div>

<script>
"use strict";

/* ============================ Мелкие утилиты ============================ */

var $ = function (id) { return document.getElementById(id); };

/* Все тексты от сервера (названия видео, ошибки yt-dlp, пути) вставляются
   только через textContent: название ролика — это чужие данные. */
function el(tag, cls, text) {
  var n = document.createElement(tag);
  if (cls) { n.className = cls; }
  if (text !== undefined && text !== null) { n.textContent = text; }
  return n;
}
function icon(id, size) {
  var svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('width', size || 16);
  svg.setAttribute('height', size || 16);
  svg.setAttribute('aria-hidden', 'true');
  var use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
  use.setAttribute('href', '#' + id);
  svg.appendChild(use);
  return svg;
}
function clear(node) { while (node.firstChild) { node.removeChild(node.firstChild); } }
function baseName(p) {
  if (!p) { return ''; }
  var parts = String(p).split(/[\\/]/);
  return parts[parts.length - 1] || p;
}
function fmtNum(n) { return (typeof n === 'number' && n > 0) ? n.toLocaleString('ru-RU') : ''; }
function fmtSize(b) {
  if (!b) { return ''; }
  var u = ['Б', 'КБ', 'МБ', 'ГБ'], i = 0;
  while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
  return (b >= 10 || i === 0 ? Math.round(b) : b.toFixed(1)) + ' ' + u[i];
}
function fmtTime(sec) {
  sec = Math.max(0, Math.round(sec || 0));
  var m = Math.floor(sec / 60), s = sec % 60;
  return m + ':' + (s < 10 ? '0' : '') + s;
}
/* localStorage может быть недоступен (приватный режим, переполнение квоты). */
function lsGet(key, fallback) {
  try {
    var raw = localStorage.getItem(key);
    return raw ? JSON.parse(raw) : fallback;
  } catch (e) { return fallback; }
}
function lsSet(key, value) {
  try { localStorage.setItem(key, JSON.stringify(value)); } catch (e) { /* не критично */ }
}

/* ====================== Мост в нативную оболочку ======================= */
/* В .app и .exe интерфейс живёт в WebView, у которого есть доступ к системе:
   показать файл в проводнике и выбрать файл диалогом. Оболочки говорят
   по-разному: macOS — messageHandlers WKWebView, Windows — функция, которую
   привязал WebView2. В обычном браузере моста нет, там запасной вариант. */
var native = {
  mac: !!(window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.app),
  win: typeof window.__native === 'function',
  send: function (msg) {
    if (this.mac) { window.webkit.messageHandlers.app.postMessage(msg); return true; }
    if (this.win) { window.__native(JSON.stringify(msg)); return true; }
    return false;
  },
  reveal: function (path) {
    if (this.send({ action: 'reveal', path: path || '' })) { return; }
    /* В браузере моста нет, но папку умеет открыть сам сервер — он на этой же
       машине. Если и это не вышло, остаётся отдать путь в буфер обмена. */
    if (!path) { return; }
    fetch('/api/reveal', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: path })
    }).then(function (r) {
      if (!r.ok) { copyPath(path); }
    }).catch(function () { copyPath(path); });
  },
  pickFile: function () { return this.send({ action: 'pickFile' }); }
};
native.ok = native.mac || native.win;
function copyPath(path) { copyText(path, 'Путь скопирован'); }

/* Буфер обмена есть не везде (старый WebView, страница открыта не с 127.0.0.1).
   Молчать в таком случае нельзя: адрес страницы расширений пользователю всё
   равно надо вставить руками, поэтому показываем текст баннером. */
function copyText(text, okTitle) {
  if (!text) { return; }
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(function () {
      banner('info', okTitle, text, null);
    }).catch(function () {
      banner('info', 'Скопируйте вручную', text, null);
    });
    return;
  }
  banner('info', 'Скопируйте вручную', text, null);
}

/* ============================== Состояние ============================== */

var MAX_PARALLEL = 2;
var state = {
  section: 'download',
  browsers: [],
  info: null,           // разобранное видео
  infoState: 'idle',    // idle | loading | error | ready
  infoError: '',
  /* Очередь живёт только в памяти: восстановить оборванный SSE-поток нельзя,
     а «воскресшая» после перезапуска задача выглядела бы висящей. */
  queue: [],
  history: lsGet('vd.history', []),
  running: 0,
  seq: 1,
  deps: null,           // null = ручка недоступна, иначе {ok, missing:[...]}
  whisper: { state: 'unknown', data: null, error: '' },
  tjob: null,           // текущая задача транскрибации
  tfile: '',            // выбранный файл
  tfileTitle: '',
  tmodelJob: null,      // скачивание модели
  /* Расширение браузера: состояние ручки /api/extension/status и результат
     последней подготовки к установке (шаги показываем, пока не ушли с раздела). */
  ext: { state: 'idle', data: null, error: '', installing: '', install: null, installError: '' },
  /* Очередь из расширения живёт на сервере, а не в этой вкладке: список
     переживает перезагрузку окна, поэтому его только читаем. */
  caps: { ok: false, jobs: [], connected: false }
};
var streams = {};       // id задачи -> EventSource
var tpoll = null;       // таймер опроса прогресса транскрибации
/* Задачи, чьи строки уже показывались: появление анимируется один раз.
   Иначе прогресс перерисовывает список десять раз в секунду и строки
   бесконечно проигрывают въезд, то есть мигают и пропадают. */
var seenRows = {};
/* Прогресс приходит чаще, чем экран обновляется, — склеиваем перерисовки. */
var queueFrame = 0;
function scheduleQueueRender() {
  if (queueFrame) { return; }
  queueFrame = requestAnimationFrame(function () {
    queueFrame = 0;
    renderQueue();
  });
}

/* ============================== Баннеры ================================ */

var bannerList = [];
function banner(kind, title, text, action) {
  bannerList = bannerList.filter(function (b) { return b.title !== title; });
  bannerList.push({ kind: kind, title: title, text: text, action: action });
  if (bannerList.length > 3) { bannerList.shift(); }
  renderBanners();
}
function dropBanner(title) {
  bannerList = bannerList.filter(function (b) { return b.title !== title; });
  renderBanners();
}
function renderBanners() {
  var host = $('banners');
  clear(host);
  bannerList.forEach(function (b) {
    var node = el('div', 'banner ' + b.kind);
    node.setAttribute('role', b.kind === 'err' ? 'alert' : 'status');
    var ic = icon(b.kind === 'ok' ? 'i-check' : 'i-alert', 20);
    ic.classList.add('icon');
    node.appendChild(ic);
    var txt = el('div', 'txt');
    txt.appendChild(el('b', null, b.title));
    if (b.text) { txt.appendChild(document.createTextNode(b.text)); }
    node.appendChild(txt);
    if (b.action) {
      var btn = el('button', 'btn');
      btn.type = 'button';
      btn.appendChild(el('span', null, b.action.label));
      btn.addEventListener('click', b.action.run);
      node.appendChild(btn);
    }
    var close = el('button', 'btn btn-icon');
    close.type = 'button';
    close.title = 'Скрыть';
    close.setAttribute('aria-label', 'Скрыть сообщение');
    close.appendChild(icon('i-x', 16));
    close.addEventListener('click', function () { dropBanner(b.title); });
    node.appendChild(close);
    host.appendChild(node);
  });
}

/* ============================== Навигация ============================== */

function go(section) {
  state.section = section;
  var panes = document.querySelectorAll('.pane');
  for (var i = 0; i < panes.length; i++) {
    panes[i].classList.toggle('active', panes[i].id === 'pane-' + section);
  }
  var btns = document.querySelectorAll('.navbtn[data-go]');
  for (var j = 0; j < btns.length; j++) {
    if (btns[j].dataset.go === section) { btns[j].setAttribute('aria-current', 'page'); }
    else { btns[j].removeAttribute('aria-current'); }
  }
  if (section === 'text') { loadWhisper(); }
  /* Состояние связи с расширением опрашиваем только пока раздел на экране:
     фоновый опрос ничего не показывает, а сервер дёргает. */
  if (section === 'ext') { extSig = ''; startExtPoll(); } else { stopExtPoll(); }
  /* На открытой очереди задачи расширения читаем чаще — см. capDelay. */
  if (section === 'queue') { loadCaps(); }
  render();
}

/* ============================== Оформление ============================= */

var THEMES = [
  { id: 'auto',  icon: 'i-auto', text: 'Как в системе' },
  { id: 'light', icon: 'i-sun',  text: 'Светлая тема' },
  { id: 'dark',  icon: 'i-moon', text: 'Тёмная тема' }
];
function applyTheme(id) {
  var t = THEMES.filter(function (x) { return x.id === id; })[0] || THEMES[0];
  if (t.id === 'auto') { delete document.documentElement.dataset.theme; }
  else { document.documentElement.dataset.theme = t.id; }
  $('themeicon').setAttribute('href', '#' + t.icon);
  $('themetext').textContent = t.text;
  lsSet('vd.theme', t.id);
  /* Рамка окна нативная — просим оболочку перекраситься вместе со страницей,
     иначе светлая страница окажется в тёмном заголовке. */
  native.send({ action: 'theme', value: t.id });
}
function cycleTheme() {
  var cur = lsGet('vd.theme', 'auto');
  var i = 0;
  THEMES.forEach(function (t, k) { if (t.id === cur) { i = k; } });
  applyTheme(THEMES[(i + 1) % THEMES.length].id);
}

/* ========================= Раздел «Скачать» ============================ */

function loadBrowsers() {
  fetch('/api/browsers').then(function (r) { return r.json(); }).then(function (list) {
    state.browsers = list || [];
    var sel = $('cookie');
    state.browsers.forEach(function (b) {
      var o = el('option', null, b.display);
      o.value = b.idx;
      sel.appendChild(o);
    });
    if (state.browsers.length) { sel.value = state.browsers[0].idx; }
  }).catch(function () {
    banner('err', 'Сервер не отвечает',
      'Похоже, движок программы остановился. Перезапустите приложение.', null);
  });
}

var infoAbort = null;
function analyze() {
  var url = $('url').value.trim();
  if (!url) {
    state.infoState = 'error';
    state.infoError = 'Вставьте ссылку на видео.';
    render();
    return;
  }
  if (infoAbort) { infoAbort.abort(); }
  infoAbort = new AbortController();
  state.infoState = 'loading';
  state.info = null;
  render();

  fetch('/api/info', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url: url, cookie: parseInt($('cookie').value, 10) }),
    signal: infoAbort.signal
  }).then(function (r) {
    return r.json().then(function (d) { return { ok: r.ok, d: d }; });
  }).then(function (res) {
    if (!res.ok) {
      state.infoState = 'error';
      state.infoError = res.d.error || 'yt-dlp не смог разобрать ссылку.';
    } else {
      state.info = res.d;
      state.info.url = url;
      state.infoState = 'ready';
    }
    render();
  }).catch(function (e) {
    if (e.name === 'AbortError') { return; }
    state.infoState = 'error';
    state.infoError = 'Нет связи с движком программы. Проверьте, что приложение не закрыто.';
    render();
  });
}

function renderDownload() {
  var host = $('dlbody');
  clear(host);

  if (state.infoState === 'loading') {
    var card = el('div', 'card');
    var row = el('div', 'skel-row');
    row.appendChild(el('div', 'skel skel-thumb'));
    var lines = el('div');
    lines.style.flex = '1';
    [70, 40, 55].forEach(function (w) {
      var l = el('div', 'skel skel-line');
      l.style.width = w + '%';
      lines.appendChild(l);
    });
    row.appendChild(lines);
    card.appendChild(row);
    host.appendChild(card);
    return;
  }

  if (state.infoState === 'error') {
    var errCard = el('div', 'card');
    var e = el('div', 'empty');
    var ei = icon('i-alert', 40); ei.classList.add('icon');
    e.appendChild(ei);
    e.appendChild(el('b', null, 'Не получилось разобрать ссылку'));
    var p = el('p', null, state.infoError);
    e.appendChild(p);
    var retry = el('button', 'btn btn-primary');
    retry.type = 'button';
    retry.style.marginTop = '16px';
    retry.appendChild(icon('i-retry', 16));
    retry.appendChild(el('span', null, 'Повторить'));
    retry.addEventListener('click', analyze);
    e.appendChild(retry);
    errCard.appendChild(e);
    host.appendChild(errCard);
    return;
  }

  if (state.infoState === 'idle' || !state.info) {
    var idle = el('div', 'card');
    var em = el('div', 'empty');
    var ii = icon('i-dl', 40); ii.classList.add('icon');
    em.appendChild(ii);
    em.appendChild(el('b', null, 'Пока пусто'));
    em.appendChild(el('p', null,
      'Вставьте ссылку в поле выше и нажмите «Анализ» — покажем название, длительность и доступные качества.'));
    idle.appendChild(em);
    host.appendChild(idle);
    return;
  }

  var d = state.info;
  var c = el('div', 'card');
  var prev = el('div', 'preview');
  if (d.thumbnail) {
    var img = document.createElement('img');
    img.width = 192; img.height = 108;          /* явные размеры: без скачков вёрстки */
    img.alt = '';
    img.src = d.thumbnail;
    img.addEventListener('error', function () { img.remove(); });
    prev.appendChild(img);
  }
  var meta = el('div', 'meta');
  var title = el('p', 'title', d.title || 'Без названия');
  title.title = d.title || '';
  meta.appendChild(title);

  var tags = el('div', 'tags');
  [['Канал', d.channel || d.uploader], ['Длительность', d.duration_string],
   ['Просмотры', fmtNum(d.view_count)], ['Источник', d.extractor_key]].forEach(function (t) {
    if (!t[1]) { return; }
    var tag = el('span', 'tag', t[0] + ': ');
    tag.appendChild(el('b', null, t[1]));
    tags.appendChild(tag);
  });
  meta.appendChild(tags);
  prev.appendChild(meta);
  c.appendChild(prev);

  var f = el('div', 'field');
  f.style.marginTop = '24px';
  var lab = el('label', null, 'Качество и формат');
  lab.setAttribute('for', 'quality');
  f.appendChild(lab);
  var wrap = el('div', 'sel');
  var sel = document.createElement('select');
  sel.id = 'quality';
  (d.qualities || []).forEach(function (q) {
    var o = el('option', null, q.label);
    o.value = q.value;
    sel.appendChild(o);
  });
  wrap.appendChild(sel);
  f.appendChild(wrap);
  c.appendChild(f);

  var add = el('button', 'btn btn-primary btn-wide');
  add.type = 'button';
  add.style.marginTop = '16px';
  add.appendChild(icon('i-dl', 18));
  add.appendChild(el('span', null, 'Добавить в очередь'));
  add.addEventListener('click', function () { enqueue(sel.value, sel.options[sel.selectedIndex].text); });
  c.appendChild(add);
  host.appendChild(c);
}

/* ========================= Очередь загрузок ============================ */

function enqueue(quality, qualityLabel) {
  var d = state.info;
  if (!d) { return; }
  var task = {
    id: 'q' + (state.seq++),
    url: d.url,
    title: d.title || d.url,
    thumb: d.thumbnail || '',
    quality: quality,
    qualityLabel: qualityLabel,
    cookie: parseInt($('cookie').value, 10),
    state: 'queued',
    percent: 0,
    speed: '',
    eta: '',
    error: '',
    file: ''
  };
  state.queue.unshift(task);
  banner('ok', 'Добавлено в очередь', task.title, {
    label: 'Показать', run: function () { dropBanner('Добавлено в очередь'); go('queue'); }
  });
  pump();
  render();
}

function pump() {
  for (var i = state.queue.length - 1; i >= 0; i--) {
    if (state.running >= MAX_PARALLEL) { return; }
    var t = state.queue[i];
    if (t.state === 'queued') { startTask(t); }
  }
}

function startTask(t) {
  t.state = 'running';
  t.error = '';
  state.running++;
  var q = '/api/download?url=' + encodeURIComponent(t.url) +
    '&cookie=' + encodeURIComponent(t.cookie) +
    '&quality=' + encodeURIComponent(t.quality);
  var es = new EventSource(q);
  streams[t.id] = es;

  es.addEventListener('progress', function (ev) {
    var p = {};
    try { p = JSON.parse(ev.data); } catch (e) { return; }
    var pct = parseFloat(String(p.percent || '').replace('%', '').trim());
    if (!isNaN(pct)) { t.percent = Math.max(0, Math.min(100, pct)); }
    t.speed = p.speed && p.speed !== 'Unknown' ? p.speed : '';
    t.eta = p.eta && p.eta !== 'Unknown' ? p.eta : '';
    scheduleQueueRender();
  });

  es.addEventListener('done', function (ev) {
    var d = {};
    try { d = JSON.parse(ev.data); } catch (e) { /* пустое done тоже успех */ }
    t.state = 'done';
    t.percent = 100;
    t.file = d.file || '';
    finishTask(t);
    addHistory(t);
    render();
  });

  es.addEventListener('error', function (ev) {
    /* Событие error приходит и от сервера (с текстом), и от самого
       EventSource при обрыве. Во втором случае поток обязательно закрываем:
       иначе браузер переподключится и начнёт качать заново. */
    if (t.state !== 'running') { return; }
    t.state = 'error';
    t.error = (ev && ev.data) ? String(ev.data)
      : 'Соединение с движком прервано. Проверьте, что программа запущена.';
    finishTask(t);
    render();
  });
}

function finishTask(t) {
  var es = streams[t.id];
  if (es) { es.close(); delete streams[t.id]; }
  if (state.running > 0) { state.running--; }
  pump();
}

function cancelTask(t) {
  /* Закрытие EventSource обрывает HTTP-запрос, сервер отменяет контекст
     и гасит yt-dlp — отдельная ручка отмены не нужна. */
  t.state = 'canceled';
  t.speed = '';
  t.eta = '';
  finishTask(t);
  render();
}

function retryTask(t) {
  t.state = 'queued';
  t.percent = 0;
  t.error = '';
  pump();
  render();
}

function removeTask(t) {
  if (t.state === 'running') { cancelTask(t); }
  state.queue = state.queue.filter(function (x) { return x.id !== t.id; });
  render();
}

var TASK_LABEL = {
  queued:   { chip: '', text: 'В очереди' },
  running:  { chip: 'work', text: 'Скачивается' },
  done:     { chip: 'ok', text: 'Готово' },
  error:    { chip: 'err', text: 'Ошибка' },
  canceled: { chip: '', text: 'Отменено' }
};

function renderQueue() {
  var host = $('qbody');
  var badge = $('qcount');
  var caps = visibleCaps();
  var active = state.queue.filter(function (t) {
    return t.state === 'queued' || t.state === 'running';
  }).length + caps.filter(capBusy).length;
  badge.hidden = active === 0;
  badge.textContent = active;

  clear(host);
  /* Пустое состояние показываем, только когда пусто и здесь, и у расширения:
     иначе «очередь пуста» стояло бы прямо над списком задач. */
  if (!state.queue.length && caps.length) {
    renderCaptureSection(host);
    return;
  }
  if (!state.queue.length) {
    var card = el('div', 'card');
    var em = el('div', 'empty');
    var ic = icon('i-queue', 40); ic.classList.add('icon');
    em.appendChild(ic);
    em.appendChild(el('b', null, 'Очередь пуста'));
    em.appendChild(el('p', null,
      'Разберите ссылку в разделе «Скачать» и добавьте видео сюда — задачи пойдут по две за раз.'));
    var b = el('button', 'btn btn-primary');
    b.type = 'button';
    b.style.marginTop = '16px';
    b.appendChild(el('span', null, 'Перейти к загрузке'));
    b.addEventListener('click', function () { go('download'); });
    em.appendChild(b);
    card.appendChild(em);
    host.appendChild(card);
    return;
  }

  var list = el('div', 'list');
  state.queue.forEach(function (t, i) {
    list.appendChild(taskRow(t, i));
  });
  host.appendChild(list);
  renderCaptureSection(host);
}

function taskRow(t, index) {
  var item = el('div', 'item');
  if (seenRows[t.id]) {
    item.style.animation = 'none';
  } else {
    seenRows[t.id] = true;
    item.style.animationDelay = Math.min(index, 10) * 0.03 + 's'; /* motion.csv: stagger 0.03 */
  }

  if (t.thumb) {
    var img = document.createElement('img');
    img.className = 'thumb';
    img.width = 64; img.height = 36;
    img.alt = '';
    img.src = t.thumb;
    img.addEventListener('error', function () { img.remove(); });
    item.appendChild(img);
  }

  var body = el('div', 'body');
  var name = el('div', 'name', t.title);
  name.title = t.title;
  body.appendChild(name);

  var meta = TASK_LABEL[t.state] || TASK_LABEL.queued;
  var sub = el('div', 'sub');
  var chip = el('span', 'chip ' + meta.chip, meta.text);
  chip.style.marginRight = '8px';
  sub.appendChild(chip);
  var parts = [];
  if (t.qualityLabel) { parts.push(t.qualityLabel); }
  if (t.state === 'running') {
    parts.push(Math.round(t.percent) + '%');
    if (t.speed) { parts.push(t.speed); }
    if (t.eta) { parts.push('осталось ' + t.eta); }
  }
  if (t.state === 'done' && t.file) { parts.push(baseName(t.file)); }
  if (t.state === 'error' && t.error) { parts.push(t.error); }
  sub.appendChild(document.createTextNode(parts.join('  ·  ')));
  sub.title = parts.join('  ·  ');
  body.appendChild(sub);

  if (t.state === 'running' || t.state === 'queued') {
    var bar = el('div', 'bar' + (t.state === 'running' ? ' work' : ''));
    if (t.state === 'queued' || t.percent <= 0) { bar.classList.add('indet'); }
    bar.setAttribute('role', 'progressbar');
    bar.setAttribute('aria-valuemin', '0');
    bar.setAttribute('aria-valuemax', '100');
    bar.setAttribute('aria-valuenow', Math.round(t.percent));
    bar.setAttribute('aria-label', 'Прогресс загрузки: ' + t.title);
    var fill = el('i');
    if (!bar.classList.contains('indet')) { fill.style.width = t.percent + '%'; }
    bar.appendChild(fill);
    body.appendChild(bar);
  }
  item.appendChild(body);

  var acts = el('div', 'acts');
  function act(iconId, label, cls, run) {
    var b = el('button', 'btn btn-icon ' + (cls || ''));
    b.type = 'button';
    b.title = label;
    b.setAttribute('aria-label', label);
    b.appendChild(icon(iconId, 16));
    b.addEventListener('click', run);
    acts.appendChild(b);
  }
  if (t.state === 'running' || t.state === 'queued') {
    act('i-x', 'Отменить', '', function () { cancelTask(t); });
  }
  if (t.state === 'error' || t.state === 'canceled') {
    act('i-retry', 'Повторить', '', function () { retryTask(t); });
  }
  if (t.state === 'done') {
    act('i-folder', 'Показать в папке', '', function () { native.reveal(t.file); });
    act('i-text', 'Транскрибировать', '', function () { pickForText(t.file, t.title); });
  }
  if (t.state !== 'running') {
    act('i-trash', 'Убрать из списка', '', function () { removeTask(t); });
  }
  item.appendChild(acts);
  return item;
}

/* ==================== Очередь из расширения браузера =================== */
/* Показана отдельной секцией внутри «Очереди», а не вперемешку с обычными
   задачами. Причина не косметическая: у этих задач другой владелец. Обычную
   задачу ведёт вкладка (SSE, отмена = закрыть поток, можно повторить, при
   перезагрузке окна она исчезает), задачу из расширения ведёт сервер (живёт
   без интерфейса, отменяется ручкой, повторить нечем). В общем списке
   половина строк молча теряла бы кнопки, а часть задач необъяснимо
   переживала бы перезагрузку — разделение честнее. */

var CAP_LABEL = {
  queued:      { chip: '',     text: 'В очереди' },
  downloading: { chip: 'work', text: 'Скачивается' },
  done:        { chip: 'ok',   text: 'Готово' },
  error:       { chip: 'err',  text: 'Ошибка' },
  canceled:    { chip: '',     text: 'Отменено' }
};
var CAP_KIND = { hls: 'HLS-поток', dash: 'DASH-поток', file: 'Файл' };

function capBusy(j) { return j.state === 'queued' || j.state === 'downloading'; }

/* Завершённые задачи расширения убираются только из списка на экране: удалять
   их у сервера нечем, а кнопка «Убрать завершённые» обязана убирать всё, что
   видно, — иначе она наполовину не работает. Список убранных переживает
   перезагрузку окна (иначе строки вернулись бы сами) и чистится по факту:
   в нём остаются только те задачи, которые сервер ещё помнит. */
var capHidden = lsGet('vd.caphidden', {}) || {};
function visibleCaps() {
  return (state.caps.jobs || []).filter(function (j) { return !capHidden[j.id]; });
}
function hideFinishedCaps() {
  var changed = false;
  (state.caps.jobs || []).forEach(function (j) {
    if (!capBusy(j) && !capHidden[j.id]) { capHidden[j.id] = true; changed = true; }
  });
  if (changed) { lsSet('vd.caphidden', capHidden); }
}
function pruneHidden(jobs) {
  var next = {}, changed = false;
  jobs.forEach(function (j) { if (capHidden[j.id]) { next[j.id] = true; } });
  for (var k in capHidden) { if (!next[k]) { changed = true; } }
  capHidden = next;
  if (changed) { lsSet('vd.caphidden', capHidden); }
}

var capTimer = null;
/* Интервал опроса подстраивается под то, что происходит: качается — часто,
   раздел закрыт или окно свёрнуто — редко. Сервер локальный, но крутить
   лишний запрос в секунду в фоне всё равно незачем. */
function capDelay() {
  if (document.hidden) { return 6000; }
  if (!state.caps.ok) { return 15000; }
  var busy = (state.caps.jobs || []).filter(capBusy).length > 0;
  if (busy) { return state.section === 'queue' ? 1200 : 2500; }
  return state.section === 'queue' ? 3000 : 8000;
}

function loadCaps() {
  if (capTimer) { clearTimeout(capTimer); capTimer = null; }
  fetch('/api/capture/jobs').then(function (r) {
    if (!r.ok) { throw new Error('HTTP ' + r.status); }
    return r.json();
  }).then(function (d) {
    state.caps.ok = true;
    state.caps.jobs = d.jobs || [];
    state.caps.connected = !!d.connected;
    pruneHidden(state.caps.jobs);
    renderQueue();
    capTimer = setTimeout(loadCaps, capDelay());
  }).catch(function () {
    /* Ручки нет (старая сборка) или интерфейс открыт не с этой машины —
       тогда секции просто нет. Баннером не мешаем: пользователь этой очереди
       не заводил и чинить ему нечего. */
    state.caps.ok = false;
    state.caps.jobs = [];
    renderQueue();
    capTimer = setTimeout(loadCaps, capDelay());
  });
}

function cancelCapture(j) {
  j.state = 'canceled';        /* оптимистично: ответ сервера всё равно перекроет */
  j.speed = '';
  j.eta = '';
  renderQueue();
  fetch('/api/capture/cancel', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id: j.id })
  }).then(function (r) {
    /* 404 — задача уже завершилась сама, это не ошибка. */
    if (!r.ok && r.status !== 404) { throw new Error('HTTP ' + r.status); }
    loadCaps();
  }).catch(function () {
    banner('err', 'Задача не отменилась',
      'Движок не ответил на отмену. Проверьте, что программа запущена.', null);
    loadCaps();
  });
}

function renderCaptureSection(host) {
  var jobs = visibleCaps();
  if (!jobs.length) { return; }

  var head = el('div', 'subhead');
  head.appendChild(icon('i-ext', 14));
  head.appendChild(el('span', null, 'Поймано расширением'));
  host.appendChild(head);

  var list = el('div', 'list');
  jobs.forEach(function (j, i) { list.appendChild(captureRow(j, i)); });
  host.appendChild(list);
  host.appendChild(el('div', 'hint',
    'Эти задачи ведёт сама программа: список не пропадёт при перезагрузке окна ' +
    'и хранит последние 50 записей. Кнопка «Убрать завершённые» скрывает их с экрана.'));
}

function captureRow(j, index) {
  var item = el('div', 'item');
  var key = 'c' + j.id;
  if (seenRows[key]) {
    item.style.animation = 'none';
  } else {
    seenRows[key] = true;
    item.style.animationDelay = Math.min(index, 10) * 0.03 + 's';
  }

  var ic = icon('i-ext', 20);
  ic.style.color = 'var(--fg-muted)';
  ic.style.flex = 'none';
  item.appendChild(ic);

  var body = el('div', 'body');
  var title = j.title || baseName(j.file) || j.url || 'Без названия';
  var name = el('div', 'name', title);
  name.title = title;
  body.appendChild(name);

  var meta = CAP_LABEL[j.state] || CAP_LABEL.queued;
  var sub = el('div', 'sub');
  var chip = el('span', 'chip ' + meta.chip, meta.text);
  chip.style.marginRight = '8px';
  sub.appendChild(chip);

  var parts = [];
  if (CAP_KIND[j.kind]) { parts.push(CAP_KIND[j.kind]); }
  var from = hostOf(j.pageUrl);
  if (from) { parts.push('со страницы ' + from); }
  if (j.state === 'downloading') {
    parts.push(Math.round(j.percent || 0) + '%');
    if (j.speed) { parts.push(j.speed); }
    if (j.eta) { parts.push('осталось ' + j.eta); }
  }
  if (j.state === 'done' && j.file) { parts.push(baseName(j.file)); }
  if (j.state === 'error' && j.error) { parts.push(j.error); }
  sub.appendChild(document.createTextNode(parts.join('  ·  ')));
  sub.title = parts.join('  ·  ');
  body.appendChild(sub);

  if (capBusy(j)) {
    var bar = el('div', 'bar' + (j.state === 'downloading' ? ' work' : ''));
    if (j.state === 'queued' || !(j.percent > 0)) { bar.classList.add('indet'); }
    bar.setAttribute('role', 'progressbar');
    bar.setAttribute('aria-valuemin', '0');
    bar.setAttribute('aria-valuemax', '100');
    bar.setAttribute('aria-valuenow', Math.round(j.percent || 0));
    bar.setAttribute('aria-label', 'Прогресс загрузки: ' + title);
    var fill = el('i');
    if (!bar.classList.contains('indet')) { fill.style.width = (j.percent || 0) + '%'; }
    bar.appendChild(fill);
    body.appendChild(bar);
  }
  item.appendChild(body);

  var acts = el('div', 'acts');
  function act(iconId, label, run) {
    var b = el('button', 'btn btn-icon');
    b.type = 'button';
    b.title = label;
    b.setAttribute('aria-label', label);
    b.appendChild(icon(iconId, 16));
    b.addEventListener('click', run);
    acts.appendChild(b);
  }
  if (capBusy(j)) {
    act('i-x', 'Отменить', function () { cancelCapture(j); });
  }
  if (j.state === 'done' && j.file) {
    act('i-folder', 'Показать в папке', function () { native.reveal(j.file); });
    act('i-text', 'Транскрибировать', function () { pickForText(j.file, title); });
  }
  item.appendChild(acts);
  return item;
}

/* Домен страницы, на которой поймали видео: полный адрес в строку не влезает,
   а «со страницы rutube.ru» сразу объясняет, откуда взялась задача. */
function hostOf(raw) {
  if (!raw) { return ''; }
  var m = String(raw).match(/^[a-z]+:\/\/([^/?#]+)/i);
  if (!m) { return ''; }
  return m[1].replace(/^www\./i, '');
}

/* ============================== История =============================== */

function addHistory(t) {
  state.history.unshift({
    title: t.title,
    path: t.file,
    quality: t.qualityLabel || '',
    at: Date.now()
  });
  state.history = state.history.slice(0, 200);
  lsSet('vd.history', state.history);
}

function renderHistory() {
  var host = $('hbody');
  var badge = $('hcount');
  badge.hidden = state.history.length === 0;
  badge.textContent = state.history.length;

  clear(host);
  if (!state.history.length) {
    var card = el('div', 'card');
    var em = el('div', 'empty');
    var ic = icon('i-hist', 40); ic.classList.add('icon');
    em.appendChild(ic);
    em.appendChild(el('b', null, 'История пуста'));
    em.appendChild(el('p', null,
      'Здесь появятся скачанные файлы: можно будет открыть папку с ними или отправить видео на транскрибацию.'));
    card.appendChild(em);
    host.appendChild(card);
    return;
  }

  var list = el('div', 'list');
  state.history.forEach(function (h, i) {
    var item = el('div', 'item');
    var key = 'h' + (h.at || 0) + (h.path || '');
    if (seenRows[key]) {
      item.style.animation = 'none';
    } else {
      seenRows[key] = true;
      item.style.animationDelay = Math.min(i, 10) * 0.03 + 's';
    }
    var fi = icon('i-file', 20);
    fi.style.color = 'var(--fg-muted)';
    fi.style.flex = 'none';
    item.appendChild(fi);

    var body = el('div', 'body');
    var name = el('div', 'name', h.title || baseName(h.path));
    name.title = h.title || '';
    body.appendChild(name);
    var when = new Date(h.at || Date.now());
    var sub = el('div', 'sub', [h.quality, when.toLocaleString('ru-RU'), baseName(h.path)]
      .filter(Boolean).join('  ·  '));
    sub.title = h.path || '';
    body.appendChild(sub);
    item.appendChild(body);

    var acts = el('div', 'acts');
    function act(iconId, label, run) {
      var b = el('button', 'btn btn-icon');
      b.type = 'button';
      b.title = label;
      b.setAttribute('aria-label', label);
      b.appendChild(icon(iconId, 16));
      b.addEventListener('click', run);
      acts.appendChild(b);
    }
    act('i-folder', 'Показать в папке', function () { native.reveal(h.path); });
    act('i-text', 'Транскрибировать', function () { pickForText(h.path, h.title); });
    act('i-trash', 'Убрать из истории', function () {
      state.history = state.history.filter(function (x) { return x !== h; });
      lsSet('vd.history', state.history);
      render();
    });
    item.appendChild(acts);
    list.appendChild(item);
  });
  host.appendChild(list);
}

/* =========================== Транскрибация ============================= */

var T_STATE = {
  queued:       { chip: '',     text: 'В очереди' },
  extracting:   { chip: 'work', text: 'Извлекаю звук' },
  transcribing: { chip: 'work', text: 'Распознаю речь' },
  done:         { chip: 'ok',   text: 'Готово' },
  error:        { chip: 'err',  text: 'Ошибка' }
};

function loadWhisper() {
  if (state.whisper.state === 'loading') { return; }
  /* Скелетон показываем только пока данных нет: при обновлении уже
     открытой панели интерфейс не должен «моргать». */
  if (!state.whisper.data) { state.whisper.state = 'loading'; render(); }
  fetch('/api/whisper/status').then(function (r) {
    if (r.status === 404) { throw new Error('нет ручки'); }
    if (!r.ok) { throw new Error('HTTP ' + r.status); }
    return r.json();
  }).then(function (d) {
    state.whisper = { state: 'ready', data: d, error: '' };
    render();
  }).catch(function () {
    /* Бэкенд транскрибации может быть ещё не собран — это отдельное
       состояние интерфейса, а не ошибка пользователя. */
    state.whisper = { state: 'absent', data: null, error: '' };
    render();
  });
}

function pickForText(path, title) {
  state.tfile = path || '';
  state.tfileTitle = title || '';
  go('text');
}

function selectedModel() {
  var sel = $('tmodel');
  return sel ? sel.value : 'small';
}
function modelInfo(name) {
  var d = state.whisper.data;
  if (!d || !d.models) { return null; }
  var found = null;
  d.models.forEach(function (m) { if (m.name === name) { found = m; } });
  return found;
}

function startTranscribe() {
  if (!state.tfile) {
    banner('err', 'Файл не выбран', 'Укажите видео или аудио для распознавания.', null);
    return;
  }
  var body = {
    path: state.tfile,
    lang: $('tlang').value,
    model: selectedModel(),
    format: $('tformat').value
  };
  state.tjob = { id: '', state: 'queued', percent: 0, eta: 0, error: '', outPath: '', preview: '' };
  render();

  fetch('/api/transcribe', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body)
  }).then(function (r) {
    return r.json().then(function (d) { return { ok: r.ok, d: d }; });
  }).then(function (res) {
    if (!res.ok || !res.d.jobId) {
      state.tjob = { id: '', state: 'error', percent: 0,
        error: res.d.error || 'Сервер отказался принять файл.' };
      render();
      return;
    }
    state.tjob.id = res.d.jobId;
    pollTranscribe();
  }).catch(function () {
    state.tjob = { id: '', state: 'error', percent: 0,
      error: 'Модуль транскрибации недоступен — движок не отвечает.' };
    render();
  });
}

function pollTranscribe() {
  if (tpoll) { clearTimeout(tpoll); tpoll = null; }
  var job = state.tjob;
  if (!job || !job.id) { return; }
  fetch('/api/transcribe/progress?id=' + encodeURIComponent(job.id))
    .then(function (r) { return r.json(); })
    .then(function (d) {
      if (!state.tjob || state.tjob.id !== job.id) { return; }
      state.tjob.state = d.state || 'queued';
      state.tjob.percent = Math.max(0, Math.min(100, d.percent || 0));
      state.tjob.eta = d.eta || 0;
      state.tjob.error = d.error || '';
      state.tjob.outPath = d.outPath || '';
      state.tjob.preview = d.preview || '';
      render();
      if (d.state !== 'done' && d.state !== 'error') {
        tpoll = setTimeout(pollTranscribe, 700);
      }
    }).catch(function () {
      if (!state.tjob || state.tjob.id !== job.id) { return; }
      state.tjob.state = 'error';
      state.tjob.error = 'Потеряна связь с движком.';
      render();
    });
}

function cancelTranscribe() {
  var job = state.tjob;
  if (!job || !job.id) { state.tjob = null; render(); return; }
  fetch('/api/transcribe/cancel', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id: job.id })
  }).catch(function () {});
  if (tpoll) { clearTimeout(tpoll); tpoll = null; }
  state.tjob = null;
  render();
}

function installModel(name) {
  state.tmodelJob = { id: 'model:' + name, percent: 0, state: 'queued', error: '' };
  render();
  fetch('/api/whisper/install', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ model: name })
  }).then(function (r) {
    if (!r.ok) { throw new Error('HTTP ' + r.status); }
    pollModel();
  }).catch(function () {
    state.tmodelJob = { id: '', percent: 0, state: 'error',
      error: 'Не удалось начать скачивание модели.' };
    render();
  });
}

function pollModel() {
  var job = state.tmodelJob;
  if (!job || !job.id) { return; }
  fetch('/api/transcribe/progress?id=' + encodeURIComponent(job.id))
    .then(function (r) { return r.json(); })
    .then(function (d) {
      if (!state.tmodelJob || state.tmodelJob.id !== job.id) { return; }
      state.tmodelJob.percent = Math.max(0, Math.min(100, d.percent || 0));
      state.tmodelJob.state = d.state || 'queued';
      state.tmodelJob.error = d.error || '';
      if (d.state === 'done') {
        state.tmodelJob = null;
        state.whisper.state = 'unknown';
        loadWhisper();
        return;
      }
      if (d.state === 'error') { render(); return; }
      render();
      setTimeout(pollModel, 700);
    }).catch(function () {
      state.tmodelJob = null;
      render();
    });
}

function renderText() {
  var host = $('tbody');
  clear(host);

  if (state.whisper.state === 'loading' || state.whisper.state === 'unknown') {
    var s = el('div', 'card');
    [40, 70, 30].forEach(function (w) {
      var l = el('div', 'skel skel-line');
      l.style.width = w + '%';
      s.appendChild(l);
    });
    host.appendChild(s);
    return;
  }

  if (state.whisper.state === 'absent') {
    var card = el('div', 'card');
    var em = el('div', 'empty');
    var ic = icon('i-text', 40); ic.classList.add('icon');
    em.appendChild(ic);
    em.appendChild(el('b', null, 'Транскрибация ещё не подключена'));
    em.appendChild(el('p', null,
      'Движок распознавания речи в этой сборке отсутствует. Обновите программу — ' +
      'интерфейс подхватит модуль сам, как только он появится.'));
    var b = el('button', 'btn btn-ghost');
    b.type = 'button';
    b.style.marginTop = '16px';
    b.appendChild(icon('i-retry', 16));
    b.appendChild(el('span', null, 'Проверить ещё раз'));
    b.addEventListener('click', function () { state.whisper.state = 'unknown'; loadWhisper(); });
    em.appendChild(b);
    card.appendChild(em);
    host.appendChild(card);
    return;
  }

  var w = state.whisper.data || {};
  if (!w.installed) {
    var nb = el('div', 'banner warn');
    var ni = icon('i-alert', 20); ni.classList.add('icon');
    nb.appendChild(ni);
    var nt = el('div', 'txt');
    nt.appendChild(el('b', null, 'whisper не установлен'));
    nt.appendChild(document.createTextNode(
      'Движок распознавания скачается при первом запуске задачи — понадобится интернет.'));
    nb.appendChild(nt);
    host.appendChild(nb);
  }
  if (w.busy) {
    var bb = el('div', 'banner info');
    var bi = icon('i-alert', 20); bi.classList.add('icon');
    bb.appendChild(bi);
    var bt = el('div', 'txt');
    bt.appendChild(el('b', null, 'Движок занят'));
    bt.appendChild(document.createTextNode('Идёт другая задача распознавания — ваша встанет в очередь.'));
    bb.appendChild(bt);
    host.appendChild(bb);
  }

  /* --- Выбор файла --- */
  var fileCard = el('div', 'card');
  fileCard.appendChild(el('h2', null, 'Файл'));

  var drop = el('div', 'drop');
  drop.id = 'tdrop';
  /* Путь перетащенного файла отдаёт только оболочка macOS: в браузере и в
     WebView2 страница видит одно имя без пути. Не обещаем того, чего нет. */
  drop.appendChild(el('b', null, native.mac
    ? 'Перетащите сюда видео или аудио'
    : 'Выберите видео или аудио'));
  drop.appendChild(el('div', null, native.ok
    ? 'или выберите файл кнопкой ниже'
    : 'укажите путь ниже или возьмите файл из истории загрузок'));

  var btns = el('div');
  btns.style.marginTop = '16px';
  btns.style.display = 'flex';
  btns.style.gap = '8px';
  btns.style.justifyContent = 'center';
  btns.style.flexWrap = 'wrap';
  if (native.ok) {
    var pick = el('button', 'btn btn-ghost');
    pick.type = 'button';
    pick.appendChild(icon('i-file', 16));
    pick.appendChild(el('span', null, 'Выбрать файл...'));
    pick.addEventListener('click', function () { native.pickFile(); });
    btns.appendChild(pick);
  }
  if (state.history.length) {
    var fromHist = el('button', 'btn btn-ghost');
    fromHist.type = 'button';
    fromHist.appendChild(icon('i-hist', 16));
    fromHist.appendChild(el('span', null, 'Из истории загрузок'));
    fromHist.addEventListener('click', function () { go('history'); });
    btns.appendChild(fromHist);
  }
  drop.appendChild(btns);

  if (state.tfile) {
    var picked = el('div', 'picked');
    picked.appendChild(icon('i-check', 14));
    picked.appendChild(el('span', null, state.tfile));
    drop.appendChild(picked);
  }
  fileCard.appendChild(drop);

  /* Поле пути есть всегда: в браузере это единственный способ указать файл,
     а в приложении — быстрый ввод пути, который уже есть в буфере обмена. */
  var manual = el('div', 'field');
  manual.style.marginTop = '16px';
  var ml = el('label', null, 'Или укажите путь к файлу вручную');
  ml.setAttribute('for', 'tpath');
  manual.appendChild(ml);
  var mi = document.createElement('input');
  mi.type = 'text';
  mi.id = 'tpath';
  mi.spellcheck = false;
  mi.placeholder = 'downloads/video.mp4';
  mi.value = state.tfile;
  mi.addEventListener('change', function () { state.tfile = mi.value.trim(); render(); });
  manual.appendChild(mi);
  if (!native.ok) {
    manual.appendChild(el('div', 'hint',
      'В браузере системный путь к перетащенному файлу недоступен — его знает только приложение.'));
  }
  fileCard.appendChild(manual);
  host.appendChild(fileCard);

  /* --- Параметры --- */
  var optCard = el('div', 'card');
  optCard.appendChild(el('h2', null, 'Параметры распознавания'));
  var grid = el('div', 'grid3');

  grid.appendChild(selectField('tlang', 'Язык', [
    ['ru', 'Русский'], ['auto', 'Определить автоматически'], ['en', 'Английский']
  ], 'ru'));

  var models = (w.models && w.models.length) ? w.models : [
    { name: 'base', size: 148000000, downloaded: false },
    { name: 'small', size: 488000000, downloaded: false },
    { name: 'medium', size: 1530000000, downloaded: false },
    { name: 'large-v3', size: 3100000000, downloaded: false }
  ];
  grid.appendChild(selectField('tmodel', 'Модель', models.map(function (m) {
    var label = m.name + (m.size ? ' · ' + fmtSize(m.size) : '') + (m.downloaded ? '' : ' · не скачана');
    return [m.name, label];
  }), w['default'] || 'small'));

  grid.appendChild(selectField('tformat', 'Формат вывода', [
    ['srt', 'SRT — с таймкодами'],
    ['vtt', 'VTT — для веба'],
    ['txt', 'TXT — сплошной текст']
  ], 'srt'));
  optCard.appendChild(grid);

  /* Предупреждение про отсутствие диаризации сервер может прислать сам
     (поле note) — тогда показываем его текст, иначе свой. */
  var hint = el('div', 'hint');
  if (w.note) {
    hint.appendChild(el('b', null, w.note));
  } else {
    hint.appendChild(el('b', null, 'Whisper не различает говорящих. '));
    hint.appendChild(document.createTextNode(
      'Текст идёт одним потоком, без пометок «кто сказал».'));
  }
  hint.appendChild(document.createTextNode(
    ' Для русского языка small — разумный компромисс между скоростью и качеством, ' +
    'medium заметно точнее, но и медленнее.'));
  optCard.appendChild(hint);
  host.appendChild(optCard);

  /* --- Установка модели --- */
  var mi2 = modelInfo(selectedModelSafe(models));
  if (state.tmodelJob) {
    var mc = el('div', 'card');
    mc.appendChild(el('h2', null, 'Скачивание модели'));
    if (state.tmodelJob.state === 'error') {
      mc.appendChild(el('p', null, state.tmodelJob.error));
    } else {
      var mbar = el('div', 'bar work');
      mbar.setAttribute('role', 'progressbar');
      mbar.setAttribute('aria-valuenow', Math.round(state.tmodelJob.percent));
      var mfill = el('i');
      mfill.style.width = state.tmodelJob.percent + '%';
      mbar.appendChild(mfill);
      mc.appendChild(mbar);
      mc.appendChild(el('div', 'hint', Math.round(state.tmodelJob.percent) + '% — модель качается один раз'));
    }
    host.appendChild(mc);
  } else if (mi2 && !mi2.downloaded) {
    var ib = el('div', 'banner warn');
    var ii2 = icon('i-alert', 20); ii2.classList.add('icon');
    ib.appendChild(ii2);
    var it = el('div', 'txt');
    it.appendChild(el('b', null, 'Модель ' + mi2.name + ' не скачана'));
    it.appendChild(document.createTextNode('Понадобится ' + fmtSize(mi2.size) + ' и интернет — один раз.'));
    ib.appendChild(it);
    var ibtn = el('button', 'btn');
    ibtn.type = 'button';
    ibtn.appendChild(el('span', null, 'Установить'));
    ibtn.addEventListener('click', function () { installModel(mi2.name); });
    ib.appendChild(ibtn);
    host.appendChild(ib);
  }

  /* --- Ход задачи --- */
  var runCard = el('div', 'card');
  var job = state.tjob;
  if (!job) {
    var start = el('button', 'btn btn-primary btn-wide');
    start.type = 'button';
    start.disabled = !state.tfile;
    start.appendChild(icon('i-play', 16));
    start.appendChild(el('span', null, 'Распознать речь'));
    start.addEventListener('click', startTranscribe);
    runCard.appendChild(start);
    if (!state.tfile) {
      runCard.appendChild(el('div', 'hint', 'Сначала выберите файл.'));
    }
  } else {
    var meta = T_STATE[job.state] || T_STATE.queued;
    var head = el('div');
    head.style.display = 'flex';
    head.style.alignItems = 'center';
    head.style.gap = '8px';
    head.appendChild(el('span', 'chip ' + meta.chip, meta.text));
    if (job.state !== 'done' && job.state !== 'error') {
      head.appendChild(el('span', 'mono', Math.round(job.percent) + '%' +
        (job.eta ? '  ·  осталось ' + fmtTime(job.eta) : '')));
    }
    var sp = el('div'); sp.style.flex = '1';
    head.appendChild(sp);
    if (job.state === 'done' || job.state === 'error') {
      var again = el('button', 'btn btn-ghost');
      again.type = 'button';
      again.appendChild(icon('i-retry', 16));
      again.appendChild(el('span', null, 'Ещё раз'));
      again.addEventListener('click', function () { state.tjob = null; render(); });
      head.appendChild(again);
    } else {
      var stop = el('button', 'btn btn-danger');
      stop.type = 'button';
      stop.appendChild(icon('i-x', 16));
      stop.appendChild(el('span', null, 'Отменить'));
      stop.addEventListener('click', cancelTranscribe);
      head.appendChild(stop);
    }
    runCard.appendChild(head);

    if (job.state !== 'error') {
      var bar = el('div', 'bar ' + (job.state === 'done' ? 'ok' : 'work'));
      if (job.state !== 'done' && job.percent <= 0) { bar.classList.add('indet'); }
      bar.setAttribute('role', 'progressbar');
      bar.setAttribute('aria-valuemin', '0');
      bar.setAttribute('aria-valuemax', '100');
      bar.setAttribute('aria-valuenow', Math.round(job.percent));
      var fill = el('i');
      if (!bar.classList.contains('indet')) { fill.style.width = (job.state === 'done' ? 100 : job.percent) + '%'; }
      bar.appendChild(fill);
      runCard.appendChild(bar);
    }

    if (job.state === 'error') {
      var eb = el('div', 'banner err');
      eb.setAttribute('role', 'alert');
      eb.style.marginTop = '16px';
      eb.style.marginBottom = '0';
      var ei2 = icon('i-alert', 20); ei2.classList.add('icon');
      eb.appendChild(ei2);
      var et = el('div', 'txt');
      et.appendChild(el('b', null, 'Распознать не удалось'));
      et.appendChild(document.createTextNode(job.error || 'Неизвестная ошибка.'));
      eb.appendChild(et);
      runCard.appendChild(eb);
    }

    if (job.state === 'done') {
      var okRow = el('div');
      okRow.style.marginTop = '16px';
      okRow.style.display = 'flex';
      okRow.style.gap = '8px';
      okRow.style.flexWrap = 'wrap';
      var open = el('button', 'btn btn-ghost');
      open.type = 'button';
      open.appendChild(icon('i-folder', 16));
      open.appendChild(el('span', null, native.ok ? 'Показать в Finder' : 'Скопировать путь'));
      open.addEventListener('click', function () { native.reveal(job.outPath); });
      okRow.appendChild(open);
      runCard.appendChild(okRow);
      if (job.outPath) {
        runCard.appendChild(el('div', 'hint', job.outPath));
      }
    }

    if (job.preview) {
      var pv = el('div');
      pv.style.marginTop = '16px';
      pv.appendChild(el('div', 'lbl', 'Первые строки'));
      pv.appendChild(el('pre', 'preview-text', job.preview));
      runCard.appendChild(pv);
    }
  }
  host.appendChild(runCard);
}

function selectedModelSafe(models) {
  var sel = document.getElementById('tmodel');
  if (sel && sel.value) { return sel.value; }
  return models.length ? models[Math.min(1, models.length - 1)].name : 'small';
}

function selectField(id, label, options, def) {
  var f = el('div', 'field');
  f.style.marginBottom = '0';
  var l = el('label', null, label);
  l.setAttribute('for', id);
  f.appendChild(l);
  var wrap = el('div', 'sel');
  var sel = document.createElement('select');
  sel.id = id;
  options.forEach(function (o) {
    var opt = el('option', null, o[1]);
    opt.value = o[0];
    sel.appendChild(opt);
  });
  var saved = lsGet('vd.' + id, def);
  sel.value = saved;
  if (!sel.value) { sel.value = def; }
  sel.addEventListener('change', function () {
    lsSet('vd.' + id, sel.value);
    if (id === 'tmodel') { render(); }
  });
  wrap.appendChild(sel);
  f.appendChild(wrap);
  return f;
}

/* ====================== Проверка зависимостей ========================== */
/* Без yt-dlp и ffmpeg скачивание не заработает вовсе, поэтому состояние
   показывается баннером поверх всех разделов. Установка на сервере идёт
   в фоне — её ход опрашиваем отдельной ручкой. */

/* Ошибка от сервера — это текст Go («... connection refused»), без точки
   в конце. Приклеенная к нему подсказка читается как одно предложение,
   поэтому закрываем его сами. */
function sentence(s) {
  s = String(s || '').trim();
  if (!s) { return ''; }
  return (/[.!?…]$/.test(s) ? s : s + '.') + ' ';
}
function loadDeps() {
  fetch('/api/deps').then(function (r) {
    if (!r.ok) { throw new Error('нет ручки'); }
    return r.json();
  }).then(function (d) {
    state.deps = d;
    if (d.installing) { pollDeps(); return; }
    var missing = (d.missing || []).slice();
    if (!missing.length && d.ok !== false) {
      dropBanner('Не хватает зависимостей');
      return;
    }
    banner('warn', 'Не хватает зависимостей',
      'Не найдены: ' + missing.join(', ') + '. Без них скачивание не заработает.',
      { label: 'Установить', run: installDeps });
  }).catch(function () { state.deps = null; });
}

function installDeps() {
  dropBanner('Не хватает зависимостей');
  banner('info', 'Устанавливаю зависимости', 'Начинаю...', null);
  fetch('/api/deps/install', { method: 'POST' }).then(function (r) {
    /* 409 — установка уже идёт: это не ошибка, просто следим за ней. */
    if (!r.ok && r.status !== 409) { throw new Error('HTTP ' + r.status); }
    pollDeps();
  }).catch(function () {
    dropBanner('Устанавливаю зависимости');
    banner('err', 'Установка не удалась',
      'Поставьте yt-dlp и ffmpeg вручную и перезапустите программу.', null);
  });
}

function pollDeps() {
  fetch('/api/deps/progress').then(function (r) { return r.json(); }).then(function (d) {
    if (d.state === 'installing') {
      banner('info', 'Устанавливаю зависимости', d.stage || 'Скачиваю...', null);
      setTimeout(pollDeps, 800);
      return;
    }
    dropBanner('Устанавливаю зависимости');
    if (d.state === 'error') {
      banner('err', 'Установка не удалась',
        sentence(d.error) + 'Поставьте yt-dlp и ffmpeg вручную и перезапустите программу.', null);
      return;
    }
    loadDeps();
  }).catch(function () { dropBanner('Устанавливаю зависимости'); });
}

/* ========================= Раздел «Расширение» ========================= */
/* Установить расширение за пользователя нельзя: браузеры на движке Chromium
   с версии 137 игнорируют --load-extension, и это сделано специально. Поэтому
   программа берёт на себя всё, что может (распаковать папку, вписать ключ,
   открыть страницу расширений и папку в Finder), а оставшиеся действия
   показывает нумерованным списком — с адресом и путём, которые можно
   скопировать, если браузер проигнорировал команду открыть вкладку. */

var EXT_POLL_MS = 3000;
var extTimer = null;
/* Подпись состояния: перерисовываем раздел, только когда изменилось что-то
   структурное. Секунды с последнего сигнала меняются постоянно — если гнать
   из-за них полную перерисовку, у пользователя будет пропадать фокус
   с кнопки, до которой он только что дошёл табом. */
var extSig = '';

function startExtPoll() {
  stopExtPoll();
  loadExtStatus();
  extTimer = setInterval(function () {
    if (document.hidden) { return; }
    loadExtStatus();
  }, EXT_POLL_MS);
}
function stopExtPoll() {
  if (extTimer) { clearInterval(extTimer); extTimer = null; }
}

function loadExtStatus() {
  if (!state.ext.data) { state.ext.state = 'loading'; renderExt(); }
  fetch('/api/extension/status').then(function (r) {
    if (!r.ok) { throw new Error('HTTP ' + r.status); }
    return r.json();
  }).then(function (d) {
    state.ext.state = 'ready';
    state.ext.data = d;
    var sig = [d.ready, d.connected, d.mismatch, (d.browsers || []).length,
      d.extVersion, d.port].join('|');
    if (sig !== extSig) { extSig = sig; renderExt(); } else { updateExtPing(); }
  }).catch(function () {
    state.ext.state = 'error';
    state.ext.error = 'Движок не ответил. Возможно, интерфейс открыт не с этого ' +
      'компьютера — служебные ручки отвечают только на 127.0.0.1.';
    extSig = '';
    renderExt();
  });
}

function installExt(browserId) {
  state.ext.installing = browserId || '*';
  state.ext.installError = '';
  extSig = '';
  renderExt();
  fetch('/api/extension/install', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ browser: browserId || '', openPage: true, openFolder: true })
  }).then(function (r) {
    return r.json().then(function (d) { return { ok: r.ok, d: d }; });
  }).then(function (res) {
    state.ext.installing = '';
    if (!res.ok) {
      state.ext.installError = res.d.error || 'Программа не смогла подготовить папку расширения.';
      extSig = '';
      renderExt();
      return;
    }
    state.ext.install = res.d;
    extSig = '';
    loadExtStatus();
    renderExt();
    /* Инструкция появилась ниже кнопки — уводим на неё и экран, и фокус,
       иначе с клавиатуры и в экранном дикторе она останется незамеченной. */
    var head = $('extsteps');
    if (head) {
      head.focus();
      head.scrollIntoView({ block: 'nearest' });
    }
  }).catch(function () {
    state.ext.installing = '';
    state.ext.installError = 'Нет связи с движком программы. Проверьте, что приложение не закрыто.';
    extSig = '';
    renderExt();
  });
}

function fmtAgo(sec) {
  if (typeof sec !== 'number' || sec < 0) { return ''; }
  if (sec < 60) { return Math.max(1, Math.round(sec)) + ' с назад'; }
  if (sec < 3600) { return Math.round(sec / 60) + ' мин назад'; }
  return 'больше часа назад';
}

function updateExtPing() {
  var node = $('extping');
  if (!node) { return; }
  node.textContent = extPingText(state.ext.data || {});
}

function extPingText(d) {
  if (d.connected) {
    var ago = fmtAgo(d.lastPing);
    return 'Сигнал от расширения' + (ago ? ' — ' + ago : ' получен') +
      (d.extVersion ? ', версия ' + d.extVersion : '') +
      (d.port ? '. Программа слушает порт ' + d.port + '.' : '.');
  }
  if (d.ready) {
    return 'Папка расширения готова, но браузер ещё не отзывался. Откройте значок ' +
      'расширения в браузере — он сам постучится в программу. Если значка нет, ' +
      'пройдите шаги ниже.';
  }
  return 'Пока ставить нечего: нажмите «Установить» у нужного браузера — ' +
    'программа распакует папку с ключом доступа и покажет, что делать дальше.';
}

/* Раздел перерисовывается по опросу состояния. Если в этот момент фокус стоял
   на кнопке внутри раздела, после clear() он свалился бы на body — и человек,
   который дошёл сюда с клавиатуры, потерял бы место. Поэтому запоминаем, что
   было в фокусе, и возвращаем фокус на тот же элемент. */
function renderExt() {
  var host = $('extbody');
  if (!host) { return; }
  var active = document.activeElement;
  var focusId = (active && active.id && host.contains(active)) ? active.id : '';
  buildExt(host);
  if (focusId) {
    var back = document.getElementById(focusId);
    if (back) { back.focus(); }
  }
}

function buildExt(host) {
  clear(host);
  var s = state.ext;

  if (s.state === 'loading') {
    var sk = el('div', 'card');
    [45, 80, 60].forEach(function (w) {
      var l = el('div', 'skel skel-line');
      l.style.width = w + '%';
      sk.appendChild(l);
    });
    host.appendChild(sk);
    return;
  }

  if (s.state === 'error') {
    var errCard = el('div', 'card');
    var em = el('div', 'empty');
    var ei = icon('i-alert', 40); ei.classList.add('icon');
    em.appendChild(ei);
    em.appendChild(el('b', null, 'Состояние расширения недоступно'));
    em.appendChild(el('p', null, s.error));
    var again = el('button', 'btn btn-primary');
    again.type = 'button';
    again.style.marginTop = '16px';
    again.appendChild(icon('i-retry', 16));
    again.appendChild(el('span', null, 'Проверить ещё раз'));
    again.addEventListener('click', loadExtStatus);
    em.appendChild(again);
    errCard.appendChild(em);
    host.appendChild(errCard);
    return;
  }

  var d = s.data || {};
  host.appendChild(extStatusCard(d));
  if (d.mismatch) { host.appendChild(extMismatchBanner(d)); }
  host.appendChild(extBrowsersCard(d));
  if (s.install) { host.appendChild(extStepsCard(s.install)); }
}

function extStatusCard(d) {
  var card = el('div', 'card');
  var row = el('div');
  row.style.display = 'flex';
  row.style.alignItems = 'flex-start';
  row.style.gap = '12px';

  /* Состояние не только цветом: в плашке есть и значок, и слово
     (ux-guidelines: Color Only). */
  var chip = el('span', 'chip ' + (d.connected ? 'ok' : ''));
  chip.appendChild(icon(d.connected ? 'i-check' : 'i-alert', 12));
  chip.appendChild(el('span', null, d.connected ? 'Подключено' : 'Не подключено'));
  chip.style.flex = 'none';
  chip.style.marginTop = '2px';
  row.appendChild(chip);

  var txt = el('div');
  txt.style.flex = '1';
  txt.style.minWidth = '0';
  var head = el('div', null, d.connected
    ? 'Расширение на связи'
    : (d.ready ? 'Расширение распаковано, но ещё не отозвалось' : 'Расширение не установлено'));
  head.style.fontWeight = '600';
  txt.appendChild(head);
  var line = el('div', 'hint', extPingText(d));
  line.id = 'extping';
  line.style.marginTop = '4px';
  /* Строка обновляется опросом сама: экранный диктор должен об этом узнать. */
  line.setAttribute('aria-live', 'polite');
  txt.appendChild(line);
  row.appendChild(txt);

  var refresh = el('button', 'btn btn-ghost');
  refresh.type = 'button';
  refresh.id = 'extrefresh';       /* id — якорь для возврата фокуса, см. renderExt */
  refresh.style.flex = 'none';
  refresh.appendChild(icon('i-retry', 16));
  refresh.appendChild(el('span', null, 'Проверить'));
  refresh.addEventListener('click', loadExtStatus);
  row.appendChild(refresh);

  card.appendChild(row);
  return card;
}

function extMismatchBanner(d) {
  var b = el('div', 'banner warn');
  b.setAttribute('role', 'status');
  var ic = icon('i-alert', 20); ic.classList.add('icon');
  b.appendChild(ic);
  var txt = el('div', 'txt');
  txt.appendChild(el('b', null, 'Программа и расширение разной версии'));
  txt.appendChild(document.createTextNode(
    'Программа говорит по протоколу ' + (d.protocol || '?') + ', расширение — по ' +
    (d.extProtocol || '?') + '. Нажмите «Установить» ещё раз, затем «Обновить» ' +
    'на карточке расширения в браузере.'));
  b.appendChild(txt);
  return b;
}

function extBrowsersCard(d) {
  var card = el('div', 'card');
  card.appendChild(el('h2', null, 'Куда установить'));
  var list = (d.browsers || []);
  var busy = !!state.ext.installing;

  if (!list.length) {
    var em = el('div', 'empty');
    var ic = icon('i-ext', 40); ic.classList.add('icon');
    em.appendChild(ic);
    em.appendChild(el('b', null, 'Браузеры не найдены'));
    em.appendChild(el('p', null,
      'Программа не нашла на этом компьютере ни одного знакомого браузера. ' +
      'Папку расширения всё равно можно подготовить и загрузить её вручную.'));
    var prep = el('button', 'btn btn-primary');
    prep.type = 'button';
    prep.id = 'extprepare';
    prep.style.marginTop = '16px';
    prep.disabled = busy;
    prep.appendChild(icon('i-folder', 16));
    prep.appendChild(el('span', null, busy ? 'Готовлю...' : 'Подготовить папку'));
    prep.addEventListener('click', function () { installExt(''); });
    em.appendChild(prep);
    card.appendChild(em);
  } else {
    var rows = el('div', 'list');
    list.forEach(function (b) {
      var item = el('div', 'item');
      item.style.animation = 'none';
      var bi = icon('i-ext', 20);
      bi.style.color = 'var(--fg-muted)';
      bi.style.flex = 'none';
      item.appendChild(bi);

      var body = el('div', 'body');
      body.appendChild(el('div', 'name', b.name));
      var parts = [b.engine === 'firefox' ? 'движок Firefox' : 'движок Chromium'];
      if (b.temporary) { parts.push('расширение живёт до перезапуска браузера'); }
      if (b.path) { parts.push(b.path); }
      var sub = el('div', 'sub', parts.join('  ·  '));
      sub.title = b.path || '';
      body.appendChild(sub);
      item.appendChild(body);

      var go2 = el('button', 'btn btn-primary');
      go2.type = 'button';
      go2.id = 'extinstall-' + b.id;
      go2.style.flex = 'none';
      go2.disabled = busy;
      go2.appendChild(el('span', null,
        state.ext.installing === b.id ? 'Готовлю...' : 'Установить'));
      go2.addEventListener('click', function () { installExt(b.id); });
      item.appendChild(go2);
      rows.appendChild(item);
    });
    card.appendChild(rows);
    card.appendChild(el('div', 'hint',
      'Кнопку можно нажимать повторно: папка перезапишется свежей версией и новым ' +
      'ключом доступа, а в браузере останется нажать «Обновить» на карточке расширения.'));
  }

  if (state.ext.installError) {
    var eb = el('div', 'banner err');
    eb.setAttribute('role', 'alert');
    eb.style.marginTop = '16px';
    eb.style.marginBottom = '0';
    var ei = icon('i-alert', 20); ei.classList.add('icon');
    eb.appendChild(ei);
    var et = el('div', 'txt');
    et.appendChild(el('b', null, 'Подготовить расширение не вышло'));
    et.appendChild(document.createTextNode(state.ext.installError));
    eb.appendChild(et);
    card.appendChild(eb);
  }

  card.appendChild(el('div', 'hint',
    'Видео с защитой DRM (Netflix, Кинопоиск и подобные) расширение помечает и ' +
    'скачивать не предлагает — обхода защиты здесь нет и не будет.'));
  return card;
}

function extStepsCard(r) {
  var card = el('div', 'card');
  var h = el('h2', null, 'Что сделать в браузере');
  /* tabindex=-1: после нажатия «Установить» фокус уводится сюда программно,
     в обычный порядок обхода табом заголовок не попадает. */
  h.id = 'extsteps';
  h.tabIndex = -1;
  card.appendChild(h);

  var ol = el('ol', 'steps');
  (r.steps || []).forEach(function (s) { ol.appendChild(el('li', null, s)); });
  card.appendChild(ol);

  if (r.dir) {
    card.appendChild(pathRow(r.dir, 'Папка расширения', function () { native.reveal(r.dir); },
      'i-folder', 'Показать папку'));
  }
  if (r.extPage) {
    card.appendChild(pathRow(r.extPage, 'Адрес страницы расширений', null, null, null));
  }

  var notes = [];
  if (r.extPage && r.pageOpened === false) {
    notes.push('Браузер не открыл страницу расширений сам — скопируйте адрес выше ' +
      'и вставьте его в адресную строку.');
  } else if (r.note) {
    notes.push(r.note);
  }
  if (r.dir && r.folderOpened === false) {
    notes.push('Окно с папкой тоже не открылось — путь выше можно скопировать и вставить ' +
      'в диалог выбора папки.');
  }
  if (r.files) {
    notes.push('Распаковано файлов: ' + r.files + '. Ключ доступа уже внутри, копировать ничего не нужно.');
  }
  notes.forEach(function (n) { card.appendChild(el('div', 'hint', n)); });
  return card;
}

/* Строка с путём или адресом: сам текст, кнопка «скопировать» и, если есть
   куда вести, кнопка действия. Скопировать нужно обязательно — Chromium умеет
   проигнорировать внутренний адрес в аргументах запуска. */
function pathRow(value, label, run, iconId, runLabel) {
  var wrap = el('div');
  var lab = el('div', 'lbl', label);
  lab.style.marginTop = '16px';
  wrap.appendChild(lab);
  var row = el('div', 'pathline');
  row.appendChild(el('span', null, value));
  var copy = el('button', 'btn btn-icon');
  copy.type = 'button';
  copy.title = 'Скопировать';
  copy.setAttribute('aria-label', 'Скопировать: ' + label);
  copy.appendChild(icon('i-copy', 16));
  copy.addEventListener('click', function () { copyText(value, 'Скопировано'); });
  row.appendChild(copy);
  if (run) {
    var act = el('button', 'btn btn-icon');
    act.type = 'button';
    act.title = runLabel;
    act.setAttribute('aria-label', runLabel);
    act.appendChild(icon(iconId, 16));
    act.addEventListener('click', run);
    row.appendChild(act);
  }
  wrap.appendChild(row);
  return wrap;
}

/* ============================== Отрисовка ============================== */

function render() {
  renderDownload();
  renderQueue();
  renderHistory();
  if (state.section === 'text') { renderText(); }
  if (state.section === 'ext') { renderExt(); }
}

/* ======================== Мост: события снаружи ======================== */

window.__nativePicked = function (path) {
  if (!path) { return; }
  state.tfile = path;
  go('text');
};
window.__nativeDrop = function (paths) {
  if (!paths || !paths.length) { return; }
  state.tfile = paths[0];
  go('text');
};
window.__nativeDragActive = function (on) {
  var d = document.getElementById('tdrop');
  if (d) { d.classList.toggle('hot', !!on); }
};

/* Демонстрация состояний для скриншотов документации. */
window.__demo = function (name) {
  if (name === 'deps') {
    banner('warn', 'Не хватает зависимостей',
      'Не найдены: yt-dlp, ffmpeg. Без них скачивание не заработает.',
      { label: 'Установить', run: function () {} });
  }
  if (name === 'error') {
    state.infoState = 'error';
    state.infoError = 'ERROR: Sign in to confirm you are not a bot. Выберите источник cookies и повторите.';
  }
  if (name === 'loading') { state.infoState = 'loading'; }
  render();
};

/* ============================== Запуск ================================= */

/* Перетаскивание файла в обычном браузере: полный путь недоступен, но
   подсветку зоны и подсказку показать можно. В приложении путь приходит
   из нативной части через window.__nativeDrop. */
window.addEventListener('dragover', function (e) {
  e.preventDefault();
  window.__nativeDragActive(true);
});
window.addEventListener('dragleave', function () { window.__nativeDragActive(false); });
window.addEventListener('drop', function (e) {
  e.preventDefault();
  window.__nativeDragActive(false);
  if (native.ok) { return; }            /* путь придёт из нативной части */
  var f = e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0];
  if (!f) { return; }
  banner('warn', 'Нужен полный путь',
    'Браузер не сообщает, где лежит файл «' + f.name + '». Впишите путь вручную ниже.', null);
  go('text');
});

document.querySelectorAll('.navbtn[data-go]').forEach(function (b) {
  b.addEventListener('click', function () { go(b.dataset.go); });
});
$('theme').addEventListener('click', cycleTheme);
$('check').addEventListener('click', analyze);
$('url').addEventListener('keydown', function (e) { if (e.key === 'Enter') { analyze(); } });
$('qclear').addEventListener('click', function () {
  state.queue = state.queue.filter(function (t) {
    return t.state === 'queued' || t.state === 'running';
  });
  hideFinishedCaps();
  render();
});
$('hclear').addEventListener('click', function () {
  if (!state.history.length) { return; }
  if (!window.confirm('Очистить историю загрузок? Сами файлы останутся на диске.')) { return; }
  state.history = [];
  lsSet('vd.history', state.history);
  render();
});
$('hopen').addEventListener('click', function () {
  if (!native.send({ action: 'revealDownloads' })) {
    banner('info', 'Папка загрузок',
      'Файлы лежат в папке downloads рядом с программой.', null);
  }
});

/* Окно свернули или перекрыли — опрос притормаживает сам (см. capDelay);
   вернулись — сразу показываем свежее, а не ждём следующий тик. */
document.addEventListener('visibilitychange', function () {
  if (document.hidden) { return; }
  loadCaps();
  if (state.section === 'ext') { loadExtStatus(); }
});

/* Уходя, гасим потоки: иначе сервер продолжит качать в никуда. */
window.addEventListener('beforeunload', function () {
  Object.keys(streams).forEach(function (k) { streams[k].close(); });
});

applyTheme(lsGet('vd.theme', 'auto'));
loadBrowsers();
loadDeps();
/* Задачи из расширения могли появиться до открытия окна — счётчик в панели
   должен показывать их сразу, а не после захода в «Очередь». */
loadCaps();
render();
$('url').focus();
</script>
</body>
</html>`
