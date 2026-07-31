//go:build windows

// VideoDownloader.exe — нативная оболочка для Windows (бриф 01, зона A).
// Окно WebView2 (чистый Go, без cgo → кросс-сборка с мака), Go-сервер живёт
// дочерним процессом: соседний v-down.exe стартует без окна консоли, адрес
// берётся из его вывода, готовность — опрос /api/browsers. Закрытие окна
// убивает сервер, второй запуск поднимает уже открытое окно.
//
// Сборка: make windows-app (GOOS=windows, -H windowsgui).

package main

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
)

const (
	appTitle = "Video Downloader"

	// Логические пиксели: физические считаются как логические * dpi / 96.
	minWidth, minHeight = 900, 620
	defWidth, defHeight = 1120, 760
)

// Текущий сервер: хранится, чтобы погасить его после закрытия окна.
var server atomic.Pointer[serverProc]

func main() {
	// Окно и цикл сообщений обязаны жить на одном потоке ОС, а горутина без
	// привязки может переехать. Библиотека делает это в своём init, но такая
	// зависимость слишком незаметна, чтобы полагаться на неё молча.
	runtime.LockOSThread()

	enablePerMonitorDPI()

	release, ok := acquireSingleInstance()
	if !ok {
		return
	}
	defer release()

	closeLog := openLog()
	defer closeLog()

	setupJob()

	if v, err := webviewloader.GetInstalledVersion(); err != nil || v == "" {
		log.Printf("WebView2 Runtime не найден: %v", err)
		messageBox(0, "Не найден компонент «Microsoft Edge WebView2 Runtime».\n\n"+
			"В Windows 11 он есть из коробки; на Windows 10 его ставят отдельно:\n"+
			"https://developer.microsoft.com/microsoft-edge/webview2/",
			appTitle, mbOK|mbIconError)
		return
	}

	// VDOWN_DEBUG=1 включает devtools (F12) и контекстное меню WebView2:
	// живьём приложение проверяет владелец, ему нужен способ снять ошибку.
	debug := os.Getenv("VDOWN_DEBUG") == "1"

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     debug,
		AutoFocus: true,
		DataPath:  filepath.Join(dataDir(), "webview2"),
		WindowOptions: webview2.WindowOptions{
			Title:  appTitle,
			Width:  defWidth,
			Height: defHeight,
			Center: true,
		},
	})
	if w == nil {
		log.Print("не удалось создать окно WebView2")
		messageBox(0, "Не удалось создать окно WebView2.\n\nПодробности — в файле\n"+logPath(),
			appTitle, mbOK|mbIconError)
		return
	}

	hwnd := uintptr(w.Window())
	applyMinSize(w, hwnd)
	setWindowIcon(hwnd)
	applyGeometry(hwnd, loadGeometry())
	publishWindow(hwnd)
	bindNative(w)
	w.SetHtml(splashHTML("Запуск движка…"))

	stop, watcherDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watcherDone)
		watchWindow(w, hwnd, stop)
	}()
	go boot(w, hwnd)

	w.Run() // выходит, когда окно закрыли

	// Дожидаемся вотчера: он сохраняет геометрию окна на выходе, а процесс,
	// завершившись раньше, оборвал бы запись файла на середине.
	close(stop)
	select {
	case <-watcherDone:
	case <-time.After(2 * time.Second):
		log.Print("вотчер окна не завершился за 2 с")
	}

	if s := server.Swap(nil); s != nil {
		s.stop()
	}
	log.Print("--- выход ---")
}

// boot поднимает сервер в фоне и переводит окно на его адрес. Всё, что трогает
// окно, уходит через Dispatch: у WebView2 один поток — тот, где крутится Run.
func boot(w webview2.WebView, hwnd uintptr) {
	s, err := startServer(func() { serverDied(w, hwnd) })
	if err != nil {
		log.Printf("движок не запустился: %v", err)
		w.Dispatch(func() {
			w.SetHtml(splashHTML("Движок не запустился"))
			text := fmt.Sprintf("Не удалось запустить движок Video Downloader.\n\n%v\n\nЛог: %s", err, logPath())
			if messageBox(hwnd, text, appTitle, mbRetryCancel|mbIconError) == idRetry {
				w.SetHtml(splashHTML("Запуск движка…"))
				go boot(w, hwnd)
			} else {
				w.Terminate()
			}
		})
		return
	}
	server.Store(s)
	w.Dispatch(func() { w.Navigate(s.base) })
}

// serverDied зовётся, когда сервер завершился сам (не по нашей команде).
func serverDied(w webview2.WebView, hwnd uintptr) {
	server.Store(nil)
	w.Dispatch(func() {
		w.SetHtml(splashHTML("Движок остановился"))
		text := "Движок Video Downloader неожиданно завершился.\n\nЛог: " + logPath() + "\n\nПерезапустить?"
		if messageBox(hwnd, text, appTitle, mbRetryCancel|mbIconWarning) == idRetry {
			w.SetHtml(splashHTML("Запуск движка…"))
			go boot(w, hwnd)
		} else {
			w.Terminate()
		}
	})
}

// bindNative выдаёт странице то, чего у веб-страницы быть не может: показать
// файл в Проводнике и выбрать файл системным диалогом. Интерфейс (ui.go) зовёт
// window.__native(JSON) и ждёт ответа в window.__nativePicked.
func bindNative(w webview2.WebView) {
	err := w.Bind("__native", func(payload string) {
		var msg struct {
			Action string `json:"action"`
			Path   string `json:"path"`
		}
		if err := json.Unmarshal([]byte(payload), &msg); err != nil {
			log.Printf("непонятное сообщение от страницы: %v", err)
			return
		}
		switch msg.Action {
		case "reveal":
			revealInExplorer(msg.Path)
		case "revealDownloads":
			dir := downloadsDir()
			_ = os.MkdirAll(dir, 0o755)
			openInExplorer(dir)
		case "pickFile":
			// Диалог модальный: держим его вне потока цикла сообщений, иначе
			// окно замрёт вместе с ним.
			go func() {
				path, ok := pickMediaFile(downloadsDir())
				if !ok {
					return
				}
				w.Dispatch(func() {
					js, err := json.Marshal(path)
					if err != nil {
						return
					}
					w.Eval("window.__nativePicked(" + string(js) + ")")
				})
			}()
		}
	})
	if err != nil {
		log.Printf("не удалось привязать мост к странице: %v", err)
	}
}

// revealInExplorer открывает Проводник с выделенным файлом. Относительные пути
// сервер отдаёт от своей рабочей папки — разворачиваем их оттуда же.
func revealInExplorer(path string) {
	if path == "" {
		return
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(serverWorkDir(), path)
	}
	if _, err := os.Stat(path); err != nil {
		openInExplorer(filepath.Dir(path))
		return
	}
	// explorer.exe возвращает ненулевой код даже при успехе — ошибку не смотрим.
	_ = exec.Command("explorer.exe", "/select,"+path).Start()
}

func openInExplorer(dir string) {
	if dir == "" {
		return
	}
	_ = exec.Command("explorer.exe", dir).Start()
}

// splashHTML — заставка на время старта движка. Цвета взяты из токенов
// интерфейса (ui.go): светлая тема File Manager & Transfer, тёмная Developer
// Tool / IDE; выбор — по системной теме, анимация гаснет при
// prefers-reduced-motion.
func splashHTML(text string) string {
	const tmpl = `<!doctype html><html lang="ru"><head><meta charset="utf-8"><title>Video Downloader</title>
<style>
:root{color-scheme:light dark;--bg:#F8FAFC;--fg:#0F172A;--muted:#64748B;--primary:#2563EB}
@media (prefers-color-scheme:dark){:root{--bg:#0F172A;--fg:#F8FAFC;--muted:#94A3B8;--primary:#3B82F6}}
html,body{height:100%%;margin:0}
body{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:18px;
background:var(--bg);color:var(--fg);
font:15px/1.5 "Segoe UI Variable Text","Segoe UI",system-ui,sans-serif}
.ring{width:28px;height:28px;border-radius:50%%;border:3px solid var(--muted);
border-top-color:var(--primary);animation:spin .9s linear infinite}
@media (prefers-reduced-motion:reduce){.ring{animation:none}}
@keyframes spin{to{transform:rotate(360deg)}}
p{margin:0;color:var(--muted)}
</style></head><body><div class="ring"></div><p>%s</p></body></html>`
	return fmt.Sprintf(tmpl, html.EscapeString(text))
}
