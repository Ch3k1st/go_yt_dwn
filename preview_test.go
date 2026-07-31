package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func postPreview(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/preview", bytes.NewReader(b))
	r.Host = "127.0.0.1:8080"
	r.Header.Set("Origin", "chrome-extension://abcdef")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handlePreview(w, r)
	return w
}

func TestPreviewRequiresToken(t *testing.T) {
	captureTestSetup(t)

	w := postPreview(t, previewRequest{URL: "https://cdn.example/a.m3u8"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("без токена ожидался 403, получен %d: %s", w.Code, w.Body.String())
	}
	w = postPreview(t, previewRequest{Token: "не тот", URL: "https://cdn.example/a.m3u8"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("с чужим токеном ожидался 403, получен %d", w.Code)
	}
}

// GET с адресом в query подставили бы в <img> на любой странице — ручка обязана
// принимать только POST.
func TestPreviewRejectsGetAndSiteOrigin(t *testing.T) {
	captureTestSetup(t)
	token, _ := extensionToken()

	r := httptest.NewRequest(http.MethodGet, "/api/preview?url=https://cdn.example/a.m3u8&token="+token, nil)
	r.Host = "127.0.0.1:8080"
	w := httptest.NewRecorder()
	handlePreview(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET должен отклоняться, код %d", w.Code)
	}

	b, _ := json.Marshal(previewRequest{Token: token, URL: "https://cdn.example/a.m3u8"})
	r = httptest.NewRequest(http.MethodPost, "/api/preview", bytes.NewReader(b))
	r.Host = "127.0.0.1:8080"
	r.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	handlePreview(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("страница сайта не должна проходить даже с токеном: код %d", w.Code)
	}
}

// Ручка не должна щупать порты этой же машины и облачные метаданные.
func TestPreviewBlocksLoopbackAndLinkLocal(t *testing.T) {
	captureTestSetup(t)
	token, _ := extensionToken()

	blocked := []string{
		"http://127.0.0.1:8080/api/ping",
		"http://[::1]:8080/x.mp4",
		"http://localhost:9000/x.mp4",
		"http://169.254.169.254/latest/meta-data/",
		"http://nas.local/video.mp4",
	}
	for _, u := range blocked {
		w := postPreview(t, previewRequest{Token: token, URL: u})
		if w.Code != http.StatusForbidden {
			t.Errorf("%s должен отклоняться, код %d", u, w.Code)
		}
	}

	// Домашняя сеть разрешена намеренно: NAS с видео — рабочий сценарий.
	if !previewHostAllowed(context.Background(), "http://192.168.1.50/video.mp4") {
		t.Error("адрес домашней сети не должен блокироваться")
	}
	if !previewHostAllowed(context.Background(), "http://10.0.0.7:8000/a.m3u8") {
		t.Error("адрес домашней сети не должен блокироваться")
	}
	if validCaptureURL("file:///etc/passwd") {
		t.Error("file:// не должен считаться допустимым")
	}
}

func TestPreviewArgsSingleFrameAndHeaders(t *testing.T) {
	h := captureHeaders{
		Referer:   "https://site.example/watch",
		UserAgent: "Mozilla/5.0 Test",
		Cookie:    "sid=abc",
		Origin:    "https://site.example",
	}
	args := previewArgs("https://cdn.example/master.m3u8", 1, h, "/tmp/out.jpg")
	line := strings.Join(args, " ")

	for _, want := range []string{
		"-frames:v 1",         // ровно один кадр, а не весь файл
		"-ss 1",               // перемотка до -i: вход не читается с начала
		"-protocol_whitelist", // ffmpeg не должен уходить в file:// по манифесту
		"-rw_timeout",
		"scale='min(320,iw)':-2",
		"-user_agent Mozilla/5.0 Test",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("в аргументах нет %q: %s", want, line)
		}
	}
	// -ss обязан стоять перед -i, иначе это перемотка на выходе и файл тянется целиком.
	if idxOf(args, "-ss") > idxOf(args, "-i") {
		t.Errorf("-ss должен идти до -i: %s", line)
	}
	hdr := args[idxOf(args, "-headers")+1]
	for _, want := range []string{"Referer: https://site.example/watch", "Cookie: sid=abc", "Origin: https://site.example"} {
		if !strings.Contains(hdr, want) {
			t.Errorf("заголовок %q не передан: %q", want, hdr)
		}
	}

	// Без заголовков лишних флагов быть не должно.
	bare := strings.Join(previewArgs("https://cdn.example/a.mp4", 0, captureHeaders{}, "/tmp/o.jpg"), " ")
	if strings.Contains(bare, "-headers") || strings.Contains(bare, "-user_agent") || strings.Contains(bare, "-ss") {
		t.Errorf("лишние флаги без заголовков: %s", bare)
	}
}

// Ключ кеша — origin+path: подпись в query меняется на каждой перезагрузке
// страницы, а кадр за ней тот же.
func TestPreviewCacheNameIgnoresQuery(t *testing.T) {
	a := previewCacheName("https://cdn.example/v/master.m3u8?token=1&exp=2")
	b := previewCacheName("https://cdn.example/v/master.m3u8?token=9&exp=8")
	if a != b {
		t.Fatal("подпись в query не должна менять ключ кеша")
	}
	if a == previewCacheName("https://cdn.example/v/other.m3u8") {
		t.Fatal("разные пути должны давать разные ключи")
	}
}

func TestPreviewFailIsRemembered(t *testing.T) {
	dir := captureTestSetup(t)
	name := previewCacheName("https://cdn.example/nope.mp4")
	if err := os.MkdirAll(previewCacheDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(previewCacheDir(), name+".fail"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, bad := previewCached(name); ok || !bad {
		t.Fatal("свежая метка неудачи должна останавливать повторный запуск ffmpeg")
	}
	// Протухшая метка — пробуем снова.
	old := time.Now().Add(-2 * previewFailTTL)
	_ = os.Chtimes(filepath.Join(previewCacheDir(), name+".fail"), old, old)
	if _, ok, bad := previewCached(name); ok || bad {
		t.Fatal("протухшая метка неудачи должна забываться")
	}
	_ = dir
}

func TestPrunePreviewCache(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < previewCacheMax+20; i++ {
		p := filepath.Join(dir, string(rune('a'+i%26))+string(rune('a'+i/26))+".jpg")
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		at := time.Now().Add(time.Duration(i) * time.Second)
		_ = os.Chtimes(p, at, at)
	}
	prunePreviewCache(dir)
	entries, _ := os.ReadDir(dir)
	if len(entries) > previewCacheMax {
		t.Fatalf("кеш не подрезан: %d файлов", len(entries))
	}
}

// Живой прогон ffmpeg: генерим ролик, снимаем кадр, проверяем что это jpeg
// и что он не шире 320px.
func TestPreviewFrameRealFfmpeg(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		if local := filepath.Join("tools", "ffmpeg"); fileExists(local) {
			ff, _ = filepath.Abs(local)
		} else {
			t.Skip("ffmpeg не найден")
		}
	}
	dir := captureTestSetup(t)
	oldFf := ffmpegPath
	ffmpegPath = ff
	t.Cleanup(func() { ffmpegPath = oldFf })

	src := filepath.Join(dir, "src.mp4")
	gen := exec.Command(ff, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=1280x720:rate=10:duration=3",
		"-pix_fmt", "yuv420p", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("не удалось собрать тестовый ролик: %v %s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// previewFrame принимает http(s)-адреса, но ffmpeg'у всё равно — здесь важно
	// проверить сам кадр, а не сетевой путь.
	path, err := previewFrame(ctx, "file://"+src, captureHeaders{})
	if err != nil {
		t.Fatalf("кадр не получен: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) < 3 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Fatalf("на выходе не jpeg (%d байт, %v)", len(data), err)
	}
	if len(data) > previewMaxBytes {
		t.Fatalf("кадр слишком большой: %d байт", len(data))
	}

	// Кадр обязан быть уменьшен: 1280x720 на входе → не шире 320px на выходе.
	if w, h := jpegSize(data); w == 0 || w > previewMaxPx {
		t.Fatalf("кадр не уменьшен: %dx%d", w, h)
	}

	// Второй вызов обязан прийти из кеша, не запуская ffmpeg заново.
	st, _ := os.Stat(path)
	again, err := previewFrame(ctx, "file://"+src, captureHeaders{})
	if err != nil || again != path {
		t.Fatalf("повтор не взялся из кеша: %v", err)
	}
	st2, _ := os.Stat(path)
	if !st.ModTime().Equal(st2.ModTime()) {
		t.Fatal("кадр перегенерирован, хотя лежал в кеше")
	}
}

// jpegSize достаёт размер из маркера SOF — тянуть image/jpeg ради двух чисел
// в тесте незачем, но и «на глаз» проверять уменьшение нельзя.
func jpegSize(b []byte) (int, int) {
	for i := 2; i+9 < len(b); {
		if b[i] != 0xFF {
			i++
			continue
		}
		m := b[i+1]
		if m == 0xD8 || m == 0x01 || (m >= 0xD0 && m <= 0xD7) {
			i += 2
			continue
		}
		size := int(b[i+2])<<8 | int(b[i+3])
		if m >= 0xC0 && m <= 0xCF && m != 0xC4 && m != 0xC8 && m != 0xCC {
			return int(b[i+7])<<8 | int(b[i+8]), int(b[i+5])<<8 | int(b[i+6])
		}
		i += 2 + size
	}
	return 0, 0
}

func idxOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}
