package main

// Кадр-превью для попапа расширения: один кадр из потока через ffmpeg.
//
// Почему это не прокси. Ручка принимает только POST с токеном, отвечает всегда
// перекодированным jpeg не шире 320px и никогда — байтами источника. Адрес
// приходит от service worker'а расширения, который сверяет его со своим списком
// находок вкладки (см. extension/background.js, действие preview): проверку
// «только из результатов» делает та сторона, которая этот список и ведёт —
// сервер находок до нажатия «Скачать» вообще не видит.
//
// Ограничения: ffmpeg берёт ровно один кадр (-ss + -frames:v 1, весь файл не
// тянется), общий таймаут 8 секунд, одновременно не больше двух процессов,
// результат кешируется на диске, петлевые и link-local адреса запрещены.

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxPreviewBody = 16 << 10
	// 320px хватает строке попапа с запасом на плотный экран.
	previewMaxPx    = 320
	previewTimeout  = 8 * time.Second
	previewMaxBytes = 512 << 10
	// Ссылки подписаны и живут недолго, но кадр по ним не меняется: сутки кеша
	// избавляют от повторных запусков ffmpeg при каждом открытии попапа.
	previewTTL = 24 * time.Hour
	// Неудачу тоже помним, иначе попап будет дёргать ffmpeg каждые две секунды.
	previewFailTTL  = 10 * time.Minute
	previewCacheMax = 200
	previewParallel = 2
)

// previewRequest — тело POST /api/preview.
type previewRequest struct {
	Token   string         `json:"token"`
	URL     string         `json:"url"`
	Kind    string         `json:"kind"`
	Headers captureHeaders `json:"headers"`
}

// previewSlots ограничивает число одновременных ffmpeg: попап открывают с
// десятком находок, и запускать десять процессов разом — верный способ
// подвесить машину.
var previewSlots = make(chan struct{}, previewParallel)

var (
	previewMu       sync.Mutex
	previewInflight = map[string]chan struct{}{}
)

func previewCacheDir() string {
	return filepath.Join(toolsDir(), "preview-cache")
}

// previewCacheName — имя файла кеша. Ключ — origin+path без query: подпись в
// query меняется при каждой перезагрузке страницы, а кадр за ней тот же.
func previewCacheName(raw string) string {
	key := raw
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		key = u.Scheme + "://" + u.Host + u.Path
	}
	sum := sha1.Sum([]byte(key))
	return hex.EncodeToString(sum[:])
}

// previewCached возвращает путь к готовому кадру. ok=false — кадра нет,
// fresh=false вдобавок означает «недавно пробовали и не вышло, не повторять».
func previewCached(name string) (path string, ok bool, knownBad bool) {
	dir := previewCacheDir()
	img := filepath.Join(dir, name+".jpg")
	if st, err := os.Stat(img); err == nil {
		if time.Since(st.ModTime()) < previewTTL {
			return img, true, false
		}
		_ = os.Remove(img)
	}
	fail := filepath.Join(dir, name+".fail")
	if st, err := os.Stat(fail); err == nil {
		if time.Since(st.ModTime()) < previewFailTTL {
			return "", false, true
		}
		_ = os.Remove(fail)
	}
	return "", false, false
}

// previewArgs собирает командную строку ffmpeg для одного кадра.
//
// -ss стоит до -i намеренно: это перемотка на входе, ffmpeg не читает файл
// с начала до нужной секунды, а прыгает по ключевым кадрам.
func previewArgs(rawURL string, seek int, h captureHeaders, out string) []string {
	args := []string{
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		// Без белого списка ffmpeg по манифесту готов пойти в любой протокол,
		// включая file:// — а адрес приходит снаружи.
		"-protocol_whitelist", "file,http,https,tcp,tls,crypto",
		// Молчащая сеть не должна держать процесс до общего таймаута.
		"-rw_timeout", "5000000",
	}
	if h.UserAgent != "" {
		args = append(args, "-user_agent", h.UserAgent)
	}
	var hdr strings.Builder
	for _, kv := range [][2]string{{"Referer", h.Referer}, {"Origin", h.Origin}, {"Cookie", h.Cookie}} {
		if kv[1] != "" {
			hdr.WriteString(kv[0] + ": " + kv[1] + "\r\n")
		}
	}
	if hdr.Len() > 0 {
		args = append(args, "-headers", hdr.String())
	}
	if seek > 0 {
		args = append(args, "-ss", fmt.Sprintf("%d", seek))
	}
	args = append(args, "-i", rawURL,
		// Только первая видеодорожка; «?» — не падать, если её нет вовсе.
		"-map", "0:v:0?", "-frames:v", "1", "-an", "-sn", "-dn",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", previewMaxPx),
		"-q:v", "4", "-f", "image2", out)
	return args
}

// previewFrame отдаёт путь к кадру, вытаскивая его при необходимости.
func previewFrame(ctx context.Context, rawURL string, h captureHeaders) (string, error) {
	name := previewCacheName(rawURL)
	if path, ok, bad := previewCached(name); ok {
		return path, nil
	} else if bad {
		return "", fmt.Errorf("кадр недавно не получился")
	}

	// Один и тот же адрес попап может попросить дважды (перерисовка списка) —
	// второй запрос ждёт первый, а не запускает ещё один ffmpeg.
	previewMu.Lock()
	if wait, running := previewInflight[name]; running {
		previewMu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		if path, ok, _ := previewCached(name); ok {
			return path, nil
		}
		return "", fmt.Errorf("кадр не получен")
	}
	done := make(chan struct{})
	previewInflight[name] = done
	previewMu.Unlock()
	defer func() {
		previewMu.Lock()
		delete(previewInflight, name)
		previewMu.Unlock()
		close(done)
	}()

	select {
	case previewSlots <- struct{}{}:
		defer func() { <-previewSlots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	dir := previewCacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(dir, name+".jpg")
	tmp := out + ".part"
	defer os.Remove(tmp)

	// Первая попытка — с первой секунды (нулевой кадр у многих потоков чёрный),
	// вторая — с самого начала, для роликов короче секунды.
	var lastErr error
	for _, seek := range []int{1, 0} {
		cmd := exec.CommandContext(ctx, ffmpegPath, previewArgs(rawURL, seek, h, tmp)...)
		if err := cmd.Run(); err != nil {
			lastErr = err
			if ctx.Err() != nil {
				break
			}
			continue
		}
		st, err := os.Stat(tmp)
		if err != nil || st.Size() == 0 || st.Size() > previewMaxBytes {
			lastErr = fmt.Errorf("кадр пустой или слишком большой")
			continue
		}
		if err := os.Rename(tmp, out); err != nil {
			return "", err
		}
		prunePreviewCache(dir)
		return out, nil
	}
	// Метка неудачи: без неё попап будет пытаться снова каждые две секунды.
	_ = os.WriteFile(filepath.Join(dir, name+".fail"), nil, 0o644)
	if lastErr == nil {
		lastErr = fmt.Errorf("кадр не получен")
	}
	return "", lastErr
}

// prunePreviewCache держит папку кеша конечной: лишние файлы — самые старые.
func prunePreviewCache(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= previewCacheMax {
		return
	}
	type aged struct {
		path string
		at   time.Time
	}
	list := make([]aged, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() {
			continue
		}
		list = append(list, aged{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].at.Before(list[j].at) })
	for _, f := range list[:max(0, len(list)-previewCacheMax)] {
		_ = os.Remove(f.path)
	}
}

// previewPublicIP запрещает петлевые, link-local и multicast адреса.
//
// Петля — потому что иначе ручкой можно щупать порты этой же машины, включая
// сам сервер программы; link-local — из-за 169.254.169.254 (метаданные
// облачных машин). Обычная домашняя сеть (192.168.x, 10.x) разрешена
// намеренно: домашний NAS с видео — сценарий рабочий, а не атака.
func previewPublicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast())
}

func previewHostAllowed(ctx context.Context, raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		return previewPublicIP(ip)
	}
	switch low := strings.ToLower(host); {
	case low == "localhost", strings.HasSuffix(low, ".localhost"),
		strings.HasSuffix(low, ".local"), strings.HasSuffix(low, ".internal"):
		return false
	}
	lookup, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(lookup, host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, a := range ips {
		if !previewPublicIP(a.IP) {
			return false
		}
	}
	return true
}

// --- HTTP-обработчик ---

// handlePreview отдаёт кадр из потока. Не смог — 204 без тела: попап тихо
// оставляет заглушку, ошибку человеку показывать не о чем.
func handlePreview(w http.ResponseWriter, r *http.Request) {
	if !extensionOrigin(w, r) {
		writeErr(w, http.StatusForbidden, "Источник не разрешён")
		return
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Только POST: GET с адресом в query можно было бы подставить в <img> на
	// любой странице, и ручка превратилась бы в то самое «превью чего угодно».
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "Нужен POST")
		return
	}
	if !localRequest(r) {
		writeErr(w, http.StatusForbidden, "Доступ только с локального адреса")
		return
	}

	var req previewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPreviewBody)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "Некорректный запрос")
		return
	}
	token, err := extensionToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Нет ключа доступа")
		return
	}
	if !secureEqual(req.Token, token) {
		writeErr(w, http.StatusForbidden, "Ключ доступа не подошёл")
		return
	}
	if !validCaptureURL(req.URL) {
		writeErr(w, http.StatusBadRequest, "Некорректная ссылка")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), previewTimeout)
	defer cancel()

	if !previewHostAllowed(ctx, req.URL) {
		writeErr(w, http.StatusForbidden, "Адрес не разрешён")
		return
	}
	if !toolAvailable(ffmpegPath, "ffmpeg") {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	req.Headers = captureHeaders{
		Referer:   sanitizeHeader(req.Headers.Referer, maxHeaderLen),
		UserAgent: sanitizeHeader(req.Headers.UserAgent, maxUserAgentLen),
		Cookie:    sanitizeHeader(req.Headers.Cookie, maxCookieLen),
		Origin:    sanitizeHeader(req.Headers.Origin, maxHeaderLen),
	}

	path, err := previewFrame(ctx, req.URL, req.Headers)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "max-age=3600")
	_, _ = w.Write(data)
}
