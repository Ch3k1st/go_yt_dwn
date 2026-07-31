package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeHeader(t *testing.T) {
	// Перевод строки в значении заголовка — попытка подсунуть лишний заголовок.
	if got := sanitizeHeader("https://site\r\nX-Evil: 1", 100); got != "https://siteX-Evil: 1" {
		t.Fatalf("перевод строки не вырезан: %q", got)
	}
	if got := sanitizeHeader(strings.Repeat("a", 50), 10); len(got) != 10 {
		t.Fatalf("длина не ограничена: %d", len(got))
	}
	if got := sanitizeHeader("  x  ", 10); got != "x" {
		t.Fatalf("пробелы не убраны: %q", got)
	}
}

func TestSanitizeTitle(t *testing.T) {
	cases := map[string]string{
		"обычное имя":            "обычное имя",
		"a/b\\c":                 "abc",
		"100% видео":             "100 видео",
		"..скрытый":              "скрытый",
		"a\x00b":                 "ab",
		`к:в*о?п"р<с>т|у`:        "квопрсту",
		strings.Repeat("я", 200): strings.Repeat("я", maxTitleLen),
	}
	for in, want := range cases {
		if got := sanitizeTitle(in); got != want {
			t.Errorf("sanitizeTitle(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}

func TestValidCaptureURL(t *testing.T) {
	ok := []string{"http://a.b/c.m3u8", "https://cdn.example/video.mp4?sig=1"}
	bad := []string{"", "file:///etc/passwd", "javascript:alert(1)", "ftp://a/b", "https://", strings.Repeat("h", maxURLLen+1)}
	for _, u := range ok {
		if !validCaptureURL(u) {
			t.Errorf("%q должен быть допустим", u)
		}
	}
	for _, u := range bad {
		if validCaptureURL(u) {
			t.Errorf("%q не должен быть допустим", u)
		}
	}
}

func TestCaptureArgsPassesHeaders(t *testing.T) {
	h := captureHeaders{
		Referer:   "https://site.example/watch",
		UserAgent: "Mozilla/5.0 (Test)",
		Cookie:    "sid=abc; auth=1",
		Origin:    "https://site.example",
	}
	args := captureArgs("https://cdn.example/master.m3u8", "hls", "720p", "Ролик", h, "downloads")
	joined := strings.Join(args, "\x00")

	for _, want := range []string{
		"--referer\x00https://site.example/watch",
		"--user-agent\x00Mozilla/5.0 (Test)",
		"--add-header\x00Cookie:sid=abc; auth=1",
		"--add-header\x00Origin:https://site.example",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("в аргументах нет %q\nаргументы: %v", strings.ReplaceAll(want, "\x00", " "), args)
		}
	}
	if !contains(args, "-f") || !strings.Contains(joined, "height<=720") {
		t.Errorf("качество не учтено: %v", args)
	}
	if args[len(args)-1] != "https://cdn.example/master.m3u8" {
		t.Errorf("ссылка должна быть последним аргументом: %v", args)
	}
	if !strings.Contains(joined, "downloads/Ролик.%(ext)s") {
		t.Errorf("имя файла не из заголовка страницы: %v", args)
	}
}

func TestCaptureArgsDirectFileWithoutFormatSelector(t *testing.T) {
	args := captureArgs("https://cdn.example/v.mp4", "file", "", "", captureHeaders{}, "downloads")
	if contains(args, "-f") {
		t.Errorf("для прямого файла селектор форматов не нужен: %v", args)
	}
	if !contains(args, "downloads/%(title)s.%(ext)s") {
		t.Errorf("без заголовка имя должно брать шаблон yt-dlp: %v", args)
	}
}

func TestUniqueCaptureName(t *testing.T) {
	dir := t.TempDir()
	// Пустой заголовок — пусть имя выбирает yt-dlp.
	if got := uniqueCaptureName(dir, "", "https://cdn.example/v.mp4"); got != "" {
		t.Fatalf("без заголовка имя не подбираем: %q", got)
	}
	if got := uniqueCaptureName(dir, "Ролик", "https://cdn.example/video.mp4"); got != "Ролик" {
		t.Fatalf("свободное имя должно браться как есть: %q", got)
	}

	// Тот же заголовок, другой поток с той же страницы — имя должно отличаться,
	// иначе yt-dlp скажет «уже скачано» и второй файл не появится.
	if err := os.WriteFile(filepath.Join(dir, "Ролик.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := uniqueCaptureName(dir, "Ролик", "https://cdn.example/master.m3u8")
	if got != "Ролик — master" {
		t.Fatalf("ожидалось различение по имени потока, получено %q", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "Ролик — master.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := uniqueCaptureName(dir, "Ролик", "https://cdn.example/master.m3u8"); got != "Ролик (2)" {
		t.Fatalf("ожидался числовой суффикс, получено %q", got)
	}
}

func TestParsePercent(t *testing.T) {
	cases := map[string]int{" 42.5%": 42, "100%": 100, "": 0, "N/A": 0, "-3%": 0, "1000%": 100}
	for in, want := range cases {
		if got := parsePercent(in); got != want {
			t.Errorf("parsePercent(%q) = %d, ожидалось %d", in, got, want)
		}
	}
}

func TestSecureEqual(t *testing.T) {
	if !secureEqual("abc", "abc") {
		t.Error("одинаковые строки должны совпасть")
	}
	if secureEqual("abc", "abd") || secureEqual("abc", "ab") || secureEqual("", "x") {
		t.Error("разные строки не должны совпадать")
	}
}

func TestLocalRequest(t *testing.T) {
	ok := []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080", "127.0.0.1"}
	bad := []string{"evil.example", "evil.example:8080", "192.168.1.5:8080"}
	for _, h := range ok {
		r := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
		r.Host = h
		if !localRequest(r) {
			t.Errorf("%q должен считаться локальным", h)
		}
	}
	for _, h := range bad {
		r := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
		r.Host = h
		if localRequest(r) {
			t.Errorf("%q не локальный (защита от DNS-rebinding)", h)
		}
	}
}

func TestPingRejectsSiteOrigin(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	r.Host = "127.0.0.1:8080"
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	handlePing(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("сайту нельзя отвечать: код %d", w.Code)
	}

	r = httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	r.Host = "127.0.0.1:8080"
	r.Header.Set("Origin", "chrome-extension://abcdef")
	w = httptest.NewRecorder()
	handlePing(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("расширению нужно отвечать: код %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "chrome-extension://abcdef" {
		t.Fatalf("нет CORS-заголовка для расширения: %v", w.Header())
	}
}

// captureTestSetup изолирует tools/ и делает yt-dlp заведомо ненаходимым,
// чтобы очередь не запускала настоящую загрузку в тестах.
func captureTestSetup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("VDOWN_TOOLS_DIR", dir)
	old := ytDlpPath
	ytDlpPath = filepath.Join(dir, "yt-dlp-not-here")
	t.Cleanup(func() { ytDlpPath = old })
	return dir
}

func postCapture(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/capture", bytes.NewReader(b))
	r.Host = "127.0.0.1:8080"
	r.Header.Set("Origin", "chrome-extension://abcdef")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleCapture(w, r)
	return w
}

func TestCaptureRequiresToken(t *testing.T) {
	captureTestSetup(t)

	w := postCapture(t, captureRequest{URL: "https://cdn.example/a.mp4", Kind: "file"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("без токена ожидался 403, получен %d: %s", w.Code, w.Body.String())
	}
	w = postCapture(t, captureRequest{Token: "не тот", URL: "https://cdn.example/a.mp4", Kind: "file"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("с чужим токеном ожидался 403, получен %d", w.Code)
	}
}

func TestCaptureAcceptsTokenAndRejectsDRM(t *testing.T) {
	captureTestSetup(t)
	token, err := extensionToken()
	if err != nil {
		t.Fatalf("токен не создан: %v", err)
	}

	w := postCapture(t, captureRequest{Token: token, URL: "https://cdn.example/a.mp4", Kind: "file",
		Title: "Тест", Headers: captureHeaders{Referer: "https://site.example/"}})
	if w.Code != http.StatusOK {
		t.Fatalf("с верным токеном ожидался 200, получен %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		OK     bool   `json:"ok"`
		Queued bool   `json:"queued"`
		ID     string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || !resp.OK || !resp.Queued || resp.ID == "" {
		t.Fatalf("непонятный ответ: %s (%v)", w.Body.String(), err)
	}

	// DRM — жёсткая граница: такие потоки в очередь не попадают.
	w = postCapture(t, captureRequest{Token: token, URL: "https://cdn.example/drm.mpd", Kind: "dash", DRM: true})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("DRM должен отклоняться, код %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "DRM") {
		t.Fatalf("ответ должен объяснять причину: %s", w.Body.String())
	}

	// Схема не http(s) — не наша забота.
	w = postCapture(t, captureRequest{Token: token, URL: "file:///etc/passwd", Kind: "file"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("file:// должен отклоняться, код %d", w.Code)
	}
}

func TestCaptureRejectsSiteOrigin(t *testing.T) {
	captureTestSetup(t)
	token, _ := extensionToken()

	b, _ := json.Marshal(captureRequest{Token: token, URL: "https://cdn.example/a.mp4", Kind: "file"})
	r := httptest.NewRequest(http.MethodPost, "/api/capture", bytes.NewReader(b))
	r.Host = "127.0.0.1:8080"
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	handleCapture(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("страница сайта не должна проходить даже с токеном: код %d", w.Code)
	}
}

func TestUnpackExtensionInjectsToken(t *testing.T) {
	dir := captureTestSetup(t)
	serverPort = 8137
	t.Cleanup(func() { serverPort = 0 })

	extDir, files, err := unpackExtension()
	if err != nil {
		t.Fatalf("распаковка не удалась: %v", err)
	}
	if files < 5 {
		t.Fatalf("распаковано подозрительно мало файлов: %d", files)
	}
	if extDir != filepath.Join(dir, "extension") {
		t.Fatalf("папка не рядом с зависимостями: %s", extDir)
	}

	manifest, err := os.ReadFile(filepath.Join(extDir, "manifest.json"))
	if err != nil {
		t.Fatalf("нет manifest.json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(manifest, &m); err != nil {
		t.Fatalf("manifest.json не разбирается: %v", err)
	}
	if m["manifest_version"] != float64(3) {
		t.Fatalf("ожидался Manifest V3: %v", m["manifest_version"])
	}

	cfg, err := os.ReadFile(filepath.Join(extDir, "config.js"))
	if err != nil {
		t.Fatalf("нет config.js: %v", err)
	}
	token, _ := extensionToken()
	text := string(cfg)
	if !strings.Contains(text, token) {
		t.Fatal("токен не впечатан в config.js")
	}
	if strings.Contains(text, "__VDOWN_TOKEN__") || strings.Contains(text, "__VDOWN_PORT__") ||
		strings.Contains(text, "__VDOWN_APP_VERSION__") {
		t.Fatalf("в config.js остались плейсхолдеры:\n%s", text)
	}
	if !strings.Contains(text, "preferredPort: 8137") {
		t.Fatal("порт не подставлен в config.js")
	}

	// Мусор от прошлой версии должен исчезать при повторной распаковке.
	stale := filepath.Join(extDir, "old-file.js")
	if err := os.WriteFile(stale, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := unpackExtension(); err != nil {
		t.Fatalf("повторная распаковка: %v", err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Fatal("файл от прошлой версии остался в папке расширения")
	}
}

func TestExtensionTokenIsStable(t *testing.T) {
	captureTestSetup(t)
	a, err := extensionToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := extensionToken()
	if a != b || len(a) < 32 {
		t.Fatalf("токен нестабилен или короткий: %q / %q", a, b)
	}
}

func TestDetectInstalledBrowsers(t *testing.T) {
	for _, b := range detectInstalledBrowsers() {
		if b.Name == "" || b.Path == "" {
			t.Errorf("браузер без имени или пути: %+v", b)
		}
		if b.Engine != "chromium" && b.Engine != "firefox" {
			t.Errorf("неизвестный движок у %s: %q", b.Name, b.Engine)
		}
		if b.ExtPage == "" {
			t.Errorf("нет адреса страницы расширений у %s", b.Name)
		}
	}
}

func TestParseRegDefaultValue(t *testing.T) {
	out := "\r\nHKEY_LOCAL_MACHINE\\SOFTWARE\\Clients\\StartMenuInternet\\Google Chrome\\shell\\open\\command\r\n" +
		"    (Default)    REG_SZ    \"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe\" --arg\r\n"
	if got := parseRegDefaultValue(out); got != `C:\Program Files\Google\Chrome\Application\chrome.exe` {
		t.Fatalf("путь разобран неверно: %q", got)
	}
	if got := parseRegDefaultValue("ничего похожего"); got != "" {
		t.Fatalf("ожидалась пустая строка, получено %q", got)
	}
}
