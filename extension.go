package main

// Расширение браузера: хранение токена, распаковка папки расширения рядом
// с зависимостями и помощь в установке. Само расширение лежит в extension/
// и вшито в бинарник — программа должна уметь поставить его без интернета.
//
// Границы безопасности:
//   * токен — общий секрет программы и расширения; без него /api/capture отдаёт 403,
//     иначе любой открытый сайт мог бы ставить задачи в очередь;
//   * служебные ручки (/api/extension/*, /api/capture/jobs) отвечают только на
//     обращения по локальному адресу, отвергают запросы с чужим Origin и не
//     отдают CORS-заголовков — вкладка сайта не может ни дёрнуть их вслепую,
//     ни прочитать ответ (см. requireLocal в web.go).

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed all:extension
var extensionFS embed.FS

// extProtocol — версия протокола обмена с расширением. Расхождение показывается
// в popup предупреждением, а не молчаливой поломкой.
const extProtocol = 1

// extPortRange — диапазон, который расширение перебирает в поисках программы.
// Совпадает со списком в extension/config.js.
var extPortRange = []int{8080, 8081, 8082, 8083, 8084, 8085, 8086, 8087, 8088, 8089, 8090}

// serverPort — порт, на котором реально слушает сервер (может отличаться от 8080,
// если тот занят). Пишется один раз в runWeb до старта обработчиков.
var serverPort int

// --- токен ---

var (
	tokenOnce  sync.Once
	tokenValue string
	tokenErr   error
)

// tokenPath — файл с токеном рядом с зависимостями (tools/), как и всё остальное,
// что программа хранит у себя.
func tokenPath() string {
	return filepath.Join(toolsDir(), "extension-token")
}

// extensionToken возвращает токен, создавая его при первом обращении.
func extensionToken() (string, error) {
	tokenOnce.Do(func() {
		p := tokenPath()
		if b, err := os.ReadFile(p); err == nil {
			if s := strings.TrimSpace(string(b)); len(s) >= 32 {
				tokenValue = s
				return
			}
		}
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			tokenErr = err
			return
		}
		tokenValue = hex.EncodeToString(buf)
		// 0600: файл читает только владелец — это ключ доступа к очереди загрузок.
		if err := os.WriteFile(p, []byte(tokenValue), 0o600); err != nil {
			tokenErr = err
		}
	})
	return tokenValue, tokenErr
}

// secureEqual сравнивает токены за постоянное время: обычное == выходит из цикла
// на первом несовпавшем байте, и по времени ответа секрет можно подобрать.
func secureEqual(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// --- распаковка ---

func extensionDir() string {
	return filepath.Join(toolsDir(), "extension")
}

// unpackExtension кладёт расширение в tools/extension и впечатывает в config.js
// токен, порт и версию программы — пользователю копировать ничего не нужно.
// Вызывается при каждой установке и при старте, если папка уже существует:
// так расширение не отстаёт от обновлённой программы.
func unpackExtension() (dir string, files int, err error) {
	token, err := extensionToken()
	if err != nil {
		return "", 0, fmt.Errorf("не удалось создать ключ доступа: %w", err)
	}
	dir = extensionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, err
	}

	written := map[string]bool{}
	err = fs.WalkDir(extensionFS, "extension", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, "extension")
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			return nil
		}
		// Пути приходят из embed и уже очищены, но лишняя проверка дешевле разбора инцидента.
		if strings.Contains(rel, "..") {
			return nil
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if d.IsDir() {
			written[rel] = true
			return os.MkdirAll(target, 0o755)
		}
		data, err := extensionFS.ReadFile(p)
		if err != nil {
			return err
		}
		if rel == "config.js" {
			data = []byte(renderConfigJS(string(data), token, serverPortOrDefault()))
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
		written[rel] = true
		files++
		return nil
	})
	if err != nil {
		return "", 0, err
	}

	// Файлы из прошлых версий убираем: иначе браузер продолжит грузить удалённый скрипт.
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == dir {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return nil
		}
		if !written[filepath.ToSlash(rel)] && !info.IsDir() {
			_ = os.Remove(path)
		}
		return nil
	})
	return dir, files, nil
}

// renderConfigJS подставляет реальные значения в шаблон config.js.
func renderConfigJS(tpl, token string, port int) string {
	out := strings.ReplaceAll(tpl, "__VDOWN_TOKEN__", token)
	out = strings.ReplaceAll(out, `"__VDOWN_PORT__"`, strconv.Itoa(port))
	out = strings.ReplaceAll(out, "__VDOWN_APP_VERSION__", version)
	return out
}

func serverPortOrDefault() int {
	if serverPort > 0 {
		return serverPort
	}
	return extPortRange[0]
}

// refreshExtensionIfInstalled обновляет уже установленную папку при старте программы.
// Если пользователь расширением не пользуется — ничего не создаём.
func refreshExtensionIfInstalled() {
	if _, err := os.Stat(filepath.Join(extensionDir(), "manifest.json")); err != nil {
		return
	}
	if _, _, err := unpackExtension(); err != nil {
		fmt.Printf("  %s⚠ Не удалось обновить папку расширения: %v%s\n", cYellow, err, cReset)
	}
}

// --- состояние связи с расширением ---

type extLinkState struct {
	mu       sync.Mutex
	lastPing time.Time
	version  string
	protocol int
}

var extLink extLinkState

func (s *extLinkState) seen(version string, protocol int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastPing = time.Now()
	if version != "" {
		s.version = version
	}
	if protocol > 0 {
		s.protocol = protocol
	}
}

// snapshot отдаёт состояние подключения. connected — расширение отзывалось
// последние две минуты (popup пингует программу каждые пару секунд, пока открыт).
func (s *extLinkState) snapshot() (connected bool, ago int, version string, protocol int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastPing.IsZero() {
		return false, -1, "", 0
	}
	d := time.Since(s.lastPing)
	return d < 2*time.Minute, int(d.Seconds()), s.version, s.protocol
}

// --- проверки запросов от расширения ---

// extensionOrigin разрешает CORS только расширениям браузера.
// Пустой Origin (запрос из service worker с host-разрешением) тоже допустим:
// настоящая защита — токен, а Origin отсекает попытки с обычных страниц.
func extensionOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if strings.HasPrefix(origin, "chrome-extension://") ||
		strings.HasPrefix(origin, "moz-extension://") ||
		strings.HasPrefix(origin, "extension://") {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Vary", "Origin")
		return true
	}
	return false
}

// --- HTTP-обработчики ---

// handlePing — проверка живости для popup: без токена, чтобы расширение могло
// отличить «программа не запущена» от «программа не приняла ключ».
func handlePing(w http.ResponseWriter, r *http.Request) {
	if !extensionOrigin(w, r) {
		writeErr(w, http.StatusForbidden, "Источник не разрешён")
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Query().Get("from") == "ext" {
		p, _ := strconv.Atoi(r.URL.Query().Get("p"))
		extLink.seen(r.URL.Query().Get("v"), p)
	}
	writeJSON(w, map[string]any{
		"app":      "go_yt_dwn",
		"version":  version,
		"protocol": extProtocol,
		"port":     serverPortOrDefault(),
	})
}

// extensionStatus — всё, что нужно кнопке «Расширение» в интерфейсе.
type extensionStatus struct {
	Ready     bool               `json:"ready"`      // папка распакована
	Dir       string             `json:"dir"`        // куда её загружать в браузере
	Browsers  []installedBrowser `json:"browsers"`   // что нашли на машине
	Connected bool               `json:"connected"`  // расширение отзывалось недавно
	LastPing  int                `json:"lastPing"`   // секунд назад (-1 — ни разу)
	ExtVer    string             `json:"extVersion"` // версия расширения из ping
	Protocol  int                `json:"protocol"`   // версия протокола программы
	ExtProto  int                `json:"extProtocol"`
	Mismatch  bool               `json:"mismatch"` // версии протокола разошлись
	Port      int                `json:"port"`
}

func handleExtensionStatus(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	connected, ago, extVer, extProto := extLink.snapshot()
	_, err := os.Stat(filepath.Join(extensionDir(), "manifest.json"))
	writeJSON(w, extensionStatus{
		Ready:     err == nil,
		Dir:       extensionDir(),
		Browsers:  detectInstalledBrowsers(),
		Connected: connected,
		LastPing:  ago,
		ExtVer:    extVer,
		Protocol:  extProtocol,
		ExtProto:  extProto,
		Mismatch:  extProto > 0 && extProto != extProtocol,
		Port:      serverPortOrDefault(),
	})
}

// installResult — ответ на подготовку расширения к установке.
//
// PageLaunched / FolderLaunched названы именно так, а не …Opened: программа знает
// только то, что команду запуска удалось выполнить. Открылась ли на самом деле
// вкладка со страницей расширений — не знает никто, кроме браузера: Chromium
// намеренно умеет игнорировать внутренние адреса в аргументах запуска. Поэтому
// false здесь означает «запустить не вышло вообще», а не «вкладки нет».
//
// Указатель, а не bool: поля вовсе нет, если запускать не просили (openPage:false).
// Иначе интерфейс не отличил бы «не пытались» от «пытались и не смогли» и показал
// бы человеку ложное «браузер не запустился».
type installResult struct {
	Dir            string           `json:"dir"`
	Files          int              `json:"files"`
	Browser        installedBrowser `json:"browser"`
	ExtPage        string           `json:"extPage"`
	PageLaunched   *bool            `json:"pageLaunched,omitempty"`
	FolderLaunched *bool            `json:"folderLaunched,omitempty"`
	Note           string           `json:"note"`
	Steps          []string         `json:"steps"`
}

// launched переводит ошибку запуска в поле ответа.
func launched(err error) *bool {
	ok := err == nil
	return &ok
}

// handleExtensionInstall распаковывает расширение и открывает всё, что помогает
// его поставить: страницу расширений браузера и папку в файловом менеджере.
func handleExtensionInstall(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "Нужен POST")
		return
	}
	var req struct {
		Browser    string `json:"browser"`
		OpenPage   *bool  `json:"openPage"`
		OpenFolder *bool  `json:"openFolder"`
	}
	// Пустое тело — допустимо: просто подготовить папку.
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req)

	dir, files, err := unpackExtension()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Не удалось распаковать расширение: "+err.Error())
		return
	}

	res := installResult{Dir: dir, Files: files}
	wantPage := req.OpenPage == nil || *req.OpenPage
	wantFolder := req.OpenFolder == nil || *req.OpenFolder

	if req.Browser != "" {
		b, ok := findBrowser(req.Browser)
		if !ok {
			writeErr(w, http.StatusBadRequest, "Браузер не найден: "+req.Browser)
			return
		}
		res.Browser = b
		res.ExtPage = b.ExtPage
		res.Steps = installSteps(b, dir)
		if wantPage {
			// Chromium из соображений безопасности умеет игнорировать внутренние
			// адреса в аргументах запуска. Поэтому: запускаем и всегда показываем
			// адрес текстом — если вкладка не открылась, его вставят вручную.
			res.PageLaunched = launched(launchBrowser(b, b.ExtPage))
			res.Note = "Если страница расширений не открылась сама — вставьте в адресную строку: " + b.ExtPage
		}
	} else {
		res.Steps = installSteps(installedBrowser{Engine: "chromium", ExtPage: "chrome://extensions"}, dir)
	}

	if wantFolder {
		res.FolderLaunched = launched(openFolder(dir))
	}
	writeJSON(w, res)
}

// installSteps — инструкция под конкретный движок.
func installSteps(b installedBrowser, dir string) []string {
	if b.Engine == "firefox" {
		return []string{
			"Откройте " + b.ExtPage,
			"Нажмите «Загрузить временное дополнение…»",
			"Выберите файл manifest.json в папке " + dir,
			"Важно: в Firefox такое дополнение живёт только до перезапуска браузера — после него шаги придётся повторить",
		}
	}
	page := b.ExtPage
	if page == "" {
		page = "chrome://extensions"
	}
	return []string{
		"Откройте " + page,
		"Включите «Режим разработчика» (переключатель в правом верхнем углу)",
		"Нажмите «Загрузить распакованное расширение»",
		"Выберите папку " + dir,
		"Значок расширения покажет «программа на :" + strconv.Itoa(serverPortOrDefault()) + "» — значит связь есть",
	}
}
