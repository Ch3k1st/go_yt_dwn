package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// cookieArgs строит флаги yt-dlp для передачи cookies из выбранного источника.
// Используется и консольным режимом (downloadVideo), и веб-обработчиками.
func cookieArgs(c browserInfo) []string {
	if c.file != "" {
		return []string{"--cookies", c.file}
	}
	if c.id != "" {
		spec := c.id
		if c.profile != "" {
			spec = spec + ":" + c.profile
		}
		return []string{"--cookies-from-browser", spec}
	}
	return nil
}

// resolveCookie преобразует индекс из выпадающего списка UI в источник cookies.
// idx < 0 или вне диапазона => без cookies.
func resolveCookie(browsers []browserInfo, idx int) browserInfo {
	if idx >= 0 && idx < len(browsers) {
		return browsers[idx]
	}
	return browserInfo{}
}

func runWeb(addr string) {
	ensureDependencies()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Порт занят — берём любой свободный.
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fmt.Printf("  %s✗ Не удалось запустить сервер: %v%s\n", cRed, err, cReset)
			os.Exit(1)
		}
	}

	url := "http://" + ln.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/browsers", handleBrowsers)
	mux.HandleFunc("/api/info", handleInfo)
	mux.HandleFunc("/api/download", handleDownload)

	fmt.Printf("\n  %s%s▶ Video Downloader%s %s%s%s — веб-оболочка запущена\n", cBold, cCyan, cReset, cDim, version, cReset)
	fmt.Printf("  %sОткройте в браузере:%s %s%s%s\n", cDim, cReset, cBold+cGreen, url, cReset)
	fmt.Printf("  %sДля остановки нажмите Ctrl+C%s\n\n", cDim, cReset)

	openBrowser(url)

	if err := http.Serve(ln, mux); err != nil {
		fmt.Printf("  %s✗ Сервер остановлен: %v%s\n", cRed, err, cReset)
	}
}

// openBrowser пытается открыть системный браузер на заданном URL.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

// handleBrowsers отдаёт список найденных источников cookies для выпадающего меню.
func handleBrowsers(w http.ResponseWriter, r *http.Request) {
	browsers := detectBrowsers()
	type item struct {
		Idx     int    `json:"idx"`
		Display string `json:"display"`
	}
	out := make([]item, 0, len(browsers))
	for i, b := range browsers {
		out = append(out, item{Idx: i, Display: b.display})
	}
	writeJSON(w, out)
}

// ytFormat — один формат из массива formats в выводе yt-dlp.
type ytFormat struct {
	Height int    `json:"height"`
	Vcodec string `json:"vcodec"`
}

// ytDump — поля из yt-dlp --dump-single-json, которые нам нужны для разбора.
type ytDump struct {
	Title        string     `json:"title"`
	Thumbnail    string     `json:"thumbnail"`
	Duration     float64    `json:"duration"`
	Uploader     string     `json:"uploader"`
	Channel      string     `json:"channel"`
	ViewCount    int64      `json:"view_count"`
	ExtractorKey string     `json:"extractor_key"`
	WebpageURL   string     `json:"webpage_url"`
	Formats      []ytFormat `json:"formats"`
}

// qualityOption — вариант качества для выпадающего списка в UI.
type qualityOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// infoResponse — то, что уходит в браузер по /api/info.
type infoResponse struct {
	Title          string          `json:"title"`
	Thumbnail      string          `json:"thumbnail"`
	DurationString string          `json:"duration_string"`
	Uploader       string          `json:"uploader"`
	Channel        string          `json:"channel"`
	ViewCount      int64           `json:"view_count"`
	ExtractorKey   string          `json:"extractor_key"`
	WebpageURL     string          `json:"webpage_url"`
	Qualities      []qualityOption `json:"qualities"`
}

func handleInfo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL    string `json:"url"`
		Cookie int    `json:"cookie"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "Некорректный запрос")
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		writeErr(w, http.StatusBadRequest, "Пустая ссылка")
		return
	}

	cookie := resolveCookie(detectBrowsers(), req.Cookie)

	args := []string{
		"--dump-single-json",
		"--no-playlist",
		"--no-warnings",
		"--extractor-args", "youtube:player_client=tv,web_safari,default",
	}
	args = append(args, cookieArgs(cookie)...)
	args = append(args, req.URL)

	cmd := exec.CommandContext(r.Context(), ytDlpPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Не удалось получить инфо: "+tail(stderr.String()))
		return
	}

	var dump ytDump
	if err := json.Unmarshal(out, &dump); err != nil {
		writeErr(w, http.StatusInternalServerError, "Не удалось разобрать ответ yt-dlp")
		return
	}

	resp := infoResponse{
		Title:        dump.Title,
		Thumbnail:    dump.Thumbnail,
		Uploader:     dump.Uploader,
		Channel:      dump.Channel,
		ViewCount:    dump.ViewCount,
		ExtractorKey: dump.ExtractorKey,
		WebpageURL:   dump.WebpageURL,
		Qualities:    buildQualities(dump.Formats),
	}
	if dump.Duration > 0 {
		// Единый формат m:ss / h:mm:ss вместо «сырых» секунд от yt-dlp.
		resp.DurationString = formatDuration(int(dump.Duration))
	}
	writeJSON(w, resp)
}

// buildQualities собирает список доступных вариантов качества из форматов yt-dlp:
// «Лучшее», каждое уникальное разрешение по убыванию, плюс аудио-варианты.
func buildQualities(formats []ytFormat) []qualityOption {
	seen := map[int]bool{}
	var heights []int
	for _, f := range formats {
		if f.Height > 0 && f.Vcodec != "" && f.Vcodec != "none" && !seen[f.Height] {
			seen[f.Height] = true
			heights = append(heights, f.Height)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(heights)))

	opts := []qualityOption{{Label: "Лучшее качество", Value: "best"}}
	for _, h := range heights {
		opts = append(opts, qualityOption{Label: strconv.Itoa(h) + "p", Value: strconv.Itoa(h)})
	}
	opts = append(opts,
		qualityOption{Label: "Только аудио (m4a)", Value: "audio-m4a"},
		qualityOption{Label: "Только аудио (mp3)", Value: "audio-mp3"},
	)
	return opts
}

// buildFormatArgs преобразует выбор качества в аргументы yt-dlp (-f и сопутствующие).
func buildFormatArgs(quality string) []string {
	switch quality {
	case "audio-m4a":
		return []string{"-f", "ba[ext=m4a]/ba/bestaudio"}
	case "audio-mp3":
		return []string{"-f", "ba/bestaudio", "-x", "--audio-format", "mp3"}
	case "", "best":
		return []string{"-f", "bv*+ba/best", "--merge-output-format", "mp4"}
	default:
		// Числовая высота, например "720" → не выше 720p.
		if _, err := strconv.Atoi(quality); err == nil {
			sel := "bv*[height<=" + quality + "]+ba/b[height<=" + quality + "]/best"
			return []string{"-f", sel, "--merge-output-format", "mp4"}
		}
		return []string{"-f", "bv*+ba/best", "--merge-output-format", "mp4"}
	}
}

// handleDownload запускает yt-dlp и стримит прогресс через Server-Sent Events.
func handleDownload(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming не поддерживается", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	rawURL := strings.TrimSpace(r.URL.Query().Get("url"))
	if rawURL == "" {
		sse(w, flusher, "error", "Пустая ссылка")
		return
	}
	cookieIdx, _ := strconv.Atoi(r.URL.Query().Get("cookie"))
	cookie := resolveCookie(detectBrowsers(), cookieIdx)

	outputDir := "downloads"
	_ = os.MkdirAll(outputDir, os.ModePerm)

	progressTpl := "__PROGRESS__%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s"

	args := buildFormatArgs(r.URL.Query().Get("quality"))
	args = append(args,
		"--no-playlist",
		"--no-warnings",
		"--newline",
		"--progress",
		"--progress-template", progressTpl,
		"--print", "after_move:__FILE__%(filepath)s",
		"--ffmpeg-location", ffmpegPath,
		"--extractor-args", "youtube:player_client=tv,web_safari,default",
		"--retries", "3",
		"--sleep-requests", "1",
		"-o", outputDir+"/%(title)s.%(ext)s",
	)
	args = append(args, cookieArgs(cookie)...)
	args = append(args, rawURL)

	cmd := exec.CommandContext(r.Context(), ytDlpPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sse(w, flusher, "error", "Не удалось запустить yt-dlp")
		return
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		sse(w, flusher, "error", "Не удалось запустить yt-dlp: "+err.Error())
		return
	}

	var savedFile string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "__PROGRESS__"):
			parts := strings.SplitN(strings.TrimPrefix(line, "__PROGRESS__"), "|", 3)
			payload := map[string]string{}
			if len(parts) > 0 {
				payload["percent"] = strings.TrimSpace(parts[0])
			}
			if len(parts) > 1 {
				payload["speed"] = strings.TrimSpace(parts[1])
			}
			if len(parts) > 2 {
				payload["eta"] = strings.TrimSpace(parts[2])
			}
			b, _ := json.Marshal(payload)
			sse(w, flusher, "progress", string(b))
		case strings.HasPrefix(line, "__FILE__"):
			savedFile = strings.TrimPrefix(line, "__FILE__")
		default:
			if s := strings.TrimSpace(line); s != "" {
				sse(w, flusher, "log", s)
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		sse(w, flusher, "error", "Ошибка скачивания: "+tail(stderr.String()))
		return
	}

	done, _ := json.Marshal(map[string]string{"file": savedFile})
	sse(w, flusher, "done", string(done))
}

// --- вспомогательные функции ---

func sse(w http.ResponseWriter, f http.Flusher, event, data string) {
	// data приходит одной строкой (JSON или очищенный лог) — это требование формата SSE.
	data = strings.ReplaceAll(data, "\n", " ")
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	f.Flush()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// tail возвращает последние строки текста (для компактного показа ошибок).
func tail(s string) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.TrimSpace(strings.Join(lines, " | "))
}

func formatDuration(sec int) string {
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
