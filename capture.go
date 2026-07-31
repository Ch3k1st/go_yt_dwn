package main

// Приём медиа из расширения браузера и очередь их загрузки.
//
// Главное отличие от обычной загрузки по ссылке: адрес приходит вместе
// с заголовками запроса, которые сделал сам плеер (Referer, User-Agent, Cookie,
// Origin). Без них CDN отвечает 403 — это основная причина «ссылка есть,
// а не качается». Заголовки передаются в yt-dlp флагами --referer,
// --user-agent и --add-header.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Ограничения на входные данные: расширение шлёт то, что увидело в браузере,
// а значит длина строк ничем не ограничена. Обрезаем на входе.
const (
	maxCaptureBody   = 64 << 10
	maxCookieLen     = 8 << 10
	maxURLLen        = 4 << 10
	maxHeaderLen     = 2 << 10
	maxUserAgentLen  = 512
	maxTitleLen      = 120
	maxQueuedCapture = 64
	keepFinishedJobs = 50
)

// captureHeaders — заголовки исходного запроса плеера.
type captureHeaders struct {
	Referer   string `json:"referer"`
	UserAgent string `json:"userAgent"`
	Cookie    string `json:"cookie"`
	Origin    string `json:"origin"`
}

// captureRequest — тело POST /api/capture.
type captureRequest struct {
	Token    string         `json:"token"`
	URL      string         `json:"url"`
	PageURL  string         `json:"pageUrl"`
	Title    string         `json:"title"`
	Kind     string         `json:"kind"`
	Headers  captureHeaders `json:"headers"`
	Size     int64          `json:"size"`
	Quality  string         `json:"quality"`
	Duration int            `json:"duration"`
	DRM      bool           `json:"drm"`
	Protocol int            `json:"protocol"`
	ExtVer   string         `json:"extVersion"`
}

// captureJob — одна задача из расширения. Поля читаются HTTP-обработчиками
// из другой горутины, поэтому только под мьютексом менеджера.
type captureJob struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	PageURL string `json:"pageUrl"`
	Kind    string `json:"kind"`
	Source  string `json:"source"`
	State   string `json:"state"` // queued|downloading|done|error|canceled
	Percent int    `json:"percent"`
	Speed   string `json:"speed"`
	ETA     string `json:"eta"`
	File    string `json:"file"`
	Error   string `json:"error"`
	Created int64  `json:"created"`

	headers  captureHeaders
	quality  string
	cancel   context.CancelFunc
	finished time.Time
}

type captureManager struct {
	mu    sync.Mutex
	jobs  []*captureJob
	tasks chan *captureJob
	seq   int
	once  sync.Once
}

var captures = &captureManager{tasks: make(chan *captureJob, maxQueuedCapture)}

// start поднимает единственного воркера: параллельные yt-dlp только делят канал
// и чаще получают бан от CDN, поэтому очередь строго последовательная.
func (m *captureManager) start() {
	m.once.Do(func() {
		go func() {
			for job := range m.tasks {
				m.run(job)
			}
		}()
	})
}

func (m *captureManager) add(req captureRequest) (*captureJob, error) {
	m.start()

	m.mu.Lock()
	m.seq++
	job := &captureJob{
		ID:      fmt.Sprintf("cap%d-%d", time.Now().Unix(), m.seq),
		Title:   req.Title,
		URL:     req.URL,
		PageURL: req.PageURL,
		Kind:    req.Kind,
		Source:  "extension",
		State:   "queued",
		Created: time.Now().Unix(),
		headers: req.Headers,
		quality: req.Quality,
	}
	m.jobs = append(m.jobs, job)
	m.prune()
	m.mu.Unlock()

	select {
	case m.tasks <- job:
		return job, nil
	default:
		m.mu.Lock()
		job.State = "error"
		job.Error = "Очередь переполнена, дождитесь окончания текущих загрузок"
		m.mu.Unlock()
		return nil, fmt.Errorf("очередь переполнена")
	}
}

// prune держит список задач конечным. Вызывается из-под m.mu.
func (m *captureManager) prune() {
	if len(m.jobs) <= keepFinishedJobs {
		return
	}
	kept := m.jobs[len(m.jobs)-keepFinishedJobs:]
	m.jobs = append([]*captureJob(nil), kept...)
}

func (m *captureManager) snapshot() []captureJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]captureJob, 0, len(m.jobs))
	for i := len(m.jobs) - 1; i >= 0; i-- {
		out = append(out, *m.jobs[i]) // копия без внутренних полей — их в JSON нет
	}
	return out
}

func (m *captureManager) cancelJob(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.ID != id {
			continue
		}
		if j.State == "done" || j.State == "error" || j.State == "canceled" {
			return false
		}
		j.State = "canceled"
		if j.cancel != nil {
			j.cancel()
		}
		return true
	}
	return false
}

func (m *captureManager) update(job *captureJob, fn func(*captureJob)) {
	m.mu.Lock()
	fn(job)
	m.mu.Unlock()
}

// run выполняет одну задачу: собирает аргументы yt-dlp и читает его прогресс.
func (m *captureManager) run(job *captureJob) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var headers captureHeaders
	var quality, title, kind, rawURL string
	started := false
	// Задачу могли отменить, пока она стояла в очереди — тогда за неё не берёмся.
	m.update(job, func(j *captureJob) {
		if j.State != "queued" {
			return
		}
		j.cancel = cancel
		j.State = "downloading"
		headers, quality, title, kind, rawURL = j.headers, j.quality, j.Title, j.Kind, j.URL
		started = true
	})
	if !started {
		return
	}

	outDir := downloadsDir()
	if err := os.MkdirAll(outDir, os.ModePerm); err != nil {
		m.fail(job, "Не удалось создать папку загрузок: "+err.Error())
		return
	}

	args := captureArgs(rawURL, kind, quality, title, headers, outDir)
	cmd := exec.CommandContext(ctx, ytDlpPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.fail(job, "Не удалось запустить yt-dlp")
		return
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		m.fail(job, "Не удалось запустить yt-dlp: "+err.Error())
		return
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "__PROGRESS__"):
			p := strings.SplitN(strings.TrimPrefix(line, "__PROGRESS__"), "|", 3)
			m.update(job, func(j *captureJob) {
				if len(p) > 0 {
					j.Percent = parsePercent(p[0])
				}
				if len(p) > 1 {
					j.Speed = strings.TrimSpace(p[1])
				}
				if len(p) > 2 {
					j.ETA = strings.TrimSpace(p[2])
				}
			})
		case strings.HasPrefix(line, "__FILE__"):
			file := strings.TrimPrefix(line, "__FILE__")
			m.update(job, func(j *captureJob) { j.File = file })
		}
	}

	err = cmd.Wait()
	m.mu.Lock()
	wasCanceled := job.State == "canceled"
	m.mu.Unlock()
	if wasCanceled {
		m.update(job, func(j *captureJob) { j.finished = time.Now() })
		return
	}
	if err != nil {
		m.fail(job, "Ошибка скачивания: "+tail(stderr.String()))
		return
	}
	m.update(job, func(j *captureJob) {
		j.State = "done"
		j.Percent = 100
		j.ETA = ""
		j.finished = time.Now()
	})
}

func (m *captureManager) fail(job *captureJob, msg string) {
	m.update(job, func(j *captureJob) {
		j.State = "error"
		j.Error = msg
		j.finished = time.Now()
	})
}

// parsePercent превращает «  42.5%» в 42.
func parsePercent(s string) int {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		return 0
	}
	if f < 0 {
		return 0
	}
	if f > 100 {
		return 100
	}
	return int(f)
}

// captureArgs собирает командную строку yt-dlp для перехваченной ссылки.
//
// Заголовки — самая важная часть: ссылка из плеера почти всегда подписана под
// конкретный Referer и Cookie, и без них CDN отвечает 403.
func captureArgs(rawURL, kind, quality, title string, h captureHeaders, outDir string) []string {
	args := []string{
		"--no-playlist",
		"--no-warnings",
		"--newline",
		"--progress",
		"--progress-template", "__PROGRESS__%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s",
		"--print", "after_move:__FILE__%(filepath)s",
		"--ffmpeg-location", ffmpegPath,
		"--retries", "3",
	}

	// Прямой файл отдаём как есть: селектор форматов на нём только мешает.
	if kind == "hls" || kind == "dash" {
		sel := "bv*+ba/b/best"
		if quality != "" {
			if h := strings.TrimSuffix(quality, "p"); h != "" {
				sel = "bv*[height<=" + h + "]+ba/b[height<=" + h + "]/bv*+ba/b/best"
			}
		}
		args = append(args, "-f", sel, "--merge-output-format", "mp4")
	}

	if h.Referer != "" {
		args = append(args, "--referer", h.Referer)
	}
	if h.UserAgent != "" {
		args = append(args, "--user-agent", h.UserAgent)
	}
	if h.Origin != "" {
		args = append(args, "--add-header", "Origin:"+h.Origin)
	}
	if h.Cookie != "" {
		args = append(args, "--add-header", "Cookie:"+h.Cookie)
	}

	if name := uniqueCaptureName(outDir, sanitizeTitle(title), rawURL); name != "" {
		args = append(args, "-o", outDir+"/"+name+".%(ext)s")
	} else {
		args = append(args, "-o", outDir+"/%(title)s.%(ext)s")
	}
	return append(args, rawURL)
}

// uniqueCaptureName подбирает имя файла, которого ещё нет в папке загрузок.
//
// Имя берётся из заголовка страницы, а на одной странице потоков обычно
// несколько (mp4 и HLS, например). Без этой проверки второй файл получил бы
// то же имя, и yt-dlp просто написал бы «уже скачано», ничего не скачав.
func uniqueCaptureName(outDir, title, rawURL string) string {
	if title == "" {
		return ""
	}
	if !nameTaken(outDir, title) {
		return title
	}
	// Различаем по имени самого медиафайла: «Ролик — master», «Ролик — video».
	if base := sanitizeTitle(strings.TrimSuffix(urlBaseName(rawURL), filepath.Ext(urlBaseName(rawURL)))); base != "" {
		candidate := title + " — " + base
		if !nameTaken(outDir, candidate) {
			return candidate
		}
	}
	for i := 2; i <= 20; i++ {
		candidate := title + " (" + strconv.Itoa(i) + ")"
		if !nameTaken(outDir, candidate) {
			return candidate
		}
	}
	return title
}

// nameTaken — есть ли в папке файл с таким именем и любым расширением.
// Расширение заранее неизвестно: его выбирает yt-dlp после разбора потока.
func nameTaken(outDir, name string) bool {
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return false
	}
	prefix := name + "."
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			return true
		}
	}
	return false
}

func urlBaseName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return path.Base(u.Path)
}

// sanitizeTitle готовит заголовок страницы к подстановке в шаблон имени файла:
// убирает разделители путей, управляющие символы и «%», который yt-dlp принял бы
// за начало поля шаблона.
func sanitizeTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || r == 0x7f:
			return -1
		case strings.ContainsRune(`/\%:*?"<>|`, r):
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > maxTitleLen {
		s = strings.TrimSpace(string(runes[:maxTitleLen]))
	}
	// Точки в начале дают скрытые файлы в unix — и это не то, чего ждёт пользователь.
	return strings.TrimLeft(s, ". ")
}

// sanitizeHeader вырезает перевод строки (иначе значение «расщепит» заголовок)
// и ограничивает длину.
func sanitizeHeader(v string, max int) string {
	v = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == 0 {
			return -1
		}
		return r
	}, v)
	v = strings.TrimSpace(v)
	if len(v) > max {
		v = v[:max]
	}
	return v
}

// validCaptureURL пропускает только http(s) — file:// и прочие схемы в очередь не нужны.
func validCaptureURL(raw string) bool {
	if raw == "" || len(raw) > maxURLLen {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// --- HTTP-обработчики ---

// handleCapture принимает находку от расширения. Токен обязателен: без него
// любой сайт в браузере мог бы ставить задачи в очередь программы.
func handleCapture(w http.ResponseWriter, r *http.Request) {
	if !extensionOrigin(w, r) {
		writeErr(w, http.StatusForbidden, "Источник не разрешён")
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "Нужен POST")
		return
	}
	if !localRequest(r) {
		writeErr(w, http.StatusForbidden, "Доступ только с локального адреса")
		return
	}

	var req captureRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCaptureBody)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "Некорректный запрос")
		return
	}

	token, err := extensionToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Нет ключа доступа")
		return
	}
	if !secureEqual(req.Token, token) {
		writeErr(w, http.StatusForbidden,
			"Ключ доступа не подошёл. Переустановите расширение из программы (кнопка «Расширение»).")
		return
	}

	extLink.seen(req.ExtVer, req.Protocol)

	if req.DRM {
		// Жёсткая граница: защищённые потоки не качаем и не пытаемся.
		writeErr(w, http.StatusBadRequest, "Поток защищён DRM — скачивание не поддерживается")
		return
	}
	if !validCaptureURL(req.URL) {
		writeErr(w, http.StatusBadRequest, "Некорректная ссылка")
		return
	}
	switch req.Kind {
	case "hls", "dash", "file":
	default:
		req.Kind = "file"
	}

	req.Headers = captureHeaders{
		Referer:   sanitizeHeader(req.Headers.Referer, maxHeaderLen),
		UserAgent: sanitizeHeader(req.Headers.UserAgent, maxUserAgentLen),
		Cookie:    sanitizeHeader(req.Headers.Cookie, maxCookieLen),
		Origin:    sanitizeHeader(req.Headers.Origin, maxHeaderLen),
	}
	req.Title = sanitizeHeader(req.Title, maxHeaderLen)
	req.PageURL = sanitizeHeader(req.PageURL, maxHeaderLen)
	req.Quality = sanitizeHeader(req.Quality, 16)

	job, err := captures.add(req)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "queued": true, "id": job.ID})
}

// handleCaptureJobs отдаёт очередь перехваченных загрузок интерфейсу программы.
// Токена не требует, но и CORS-заголовков не отдаёт: со страницы стороннего
// сайта ответ прочитать нельзя.
func handleCaptureJobs(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	connected, ago, extVer, _ := extLink.snapshot()
	writeJSON(w, map[string]any{
		"jobs":       captures.snapshot(),
		"connected":  connected,
		"lastPing":   ago,
		"extVersion": extVer,
	})
}

func handleCaptureCancel(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "Нужен POST")
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "Некорректный запрос")
		return
	}
	if !captures.cancelJob(req.ID) {
		writeErr(w, http.StatusNotFound, "Задача не найдена или уже завершена")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
