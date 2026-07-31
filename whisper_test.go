package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- разбор прогресса ---

func TestParseWhisperProgress(t *testing.T) {
	cases := []struct {
		line string
		want int
		ok   bool
	}{
		{"whisper_print_progress_callback: progress =  39%", 39, true},
		{"whisper_print_progress_callback: progress = 100%", 100, true},
		{"whisper_print_progress_callback: progress =   0%", 0, true},
		{"[00:00:01.000 --> 00:00:02.000]   текст", 0, false},
		{"", 0, false},
		{"progress = абв%", 0, false},
	}
	for _, c := range cases {
		got, ok := parseWhisperProgress(c.line)
		if ok != c.ok || got != c.want {
			t.Errorf("parseWhisperProgress(%q) = %d, %v; хотели %d, %v", c.line, got, ok, c.want, c.ok)
		}
	}
}

func TestParseWhisperSegment(t *testing.T) {
	end, text, ok := parseWhisperSegment("[00:00:22.560 --> 00:01:04.760]   Привет, мир!")
	if !ok {
		t.Fatal("строка сегмента не распозналась")
	}
	if end != 64.76 {
		t.Errorf("конец сегмента = %v, хотели 64.76", end)
	}
	if text != "Привет, мир!" {
		t.Errorf("текст = %q", text)
	}

	if _, _, ok := parseWhisperSegment("whisper_init_state: Metal"); ok {
		t.Error("служебная строка ошибочно принята за сегмент")
	}
}

func TestParseFfmpegProgress(t *testing.T) {
	cases := []struct {
		line string
		want float64
		ok   bool
	}{
		{"out_time_us=12500000", 12.5, true},
		{"out_time_ms=1000000", 1, true}, // ffmpeg отдаёт тут микросекунды
		{"out_time=00:00:12.50", 0, false},
		{"progress=continue", 0, false},
	}
	for _, c := range cases {
		got, ok := parseFfmpegProgress(c.line)
		if ok != c.ok || got != c.want {
			t.Errorf("parseFfmpegProgress(%q) = %v, %v; хотели %v, %v", c.line, got, ok, c.want, c.ok)
		}
	}
}

func TestParseProbeOutput(t *testing.T) {
	withAudio := `  Duration: 01:02:03.45, start: 0.000000, bitrate: 1200 kb/s
  Stream #0:0[0x1](und): Video: h264 (avc1), yuv420p, 1920x1080
  Stream #0:1[0x2](rus): Audio: aac (LC), 48000 Hz, stereo, fltp, 128 kb/s`
	dur, hasAudio := parseProbeOutput(withAudio)
	if !hasAudio {
		t.Error("звуковая дорожка не найдена, хотя она есть")
	}
	if int(dur) != 3723 {
		t.Errorf("длительность = %v, хотели 3723", dur)
	}

	silent := `  Duration: 00:00:30.00, start: 0.000000, bitrate: 900 kb/s
  Stream #0:0: Video: h264 (avc1), yuv420p, 1280x720`
	dur, hasAudio = parseProbeOutput(silent)
	if hasAudio {
		t.Error("у файла без звука нашлась звуковая дорожка")
	}
	if dur != 30 {
		t.Errorf("длительность = %v, хотели 30", dur)
	}
}

func TestParseCMakeProgress(t *testing.T) {
	if p, ok := parseCMakeProgress("[ 42%] Building CXX object src/CMakeFiles"); !ok || p != 42 {
		t.Errorf("получили %d, %v", p, ok)
	}
	if _, ok := parseCMakeProgress("-- Configuring done"); ok {
		t.Error("обычная строка принята за прогресс")
	}
}

// --- сборка аргументов ---

func TestBuildWhisperArgs(t *testing.T) {
	args := buildWhisperArgs(whisperArgs{
		Model:     "/tools/models/ggml-small.bin",
		Audio:     "/tmp/аудио файл.wav",
		OutPrefix: "/tmp/result",
		Lang:      "ru",
		Format:    "srt",
		Threads:   8,
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{"-m /tools/models/ggml-small.bin", "-f /tmp/аудио файл.wav",
		"-of /tmp/result", "-l ru", "-t 8", "-pp", "-osrt"} {
		if !strings.Contains(joined, want) {
			t.Errorf("в аргументах нет %q: %v", want, args)
		}
	}
	if strings.Contains(joined, "-otxt") || strings.Contains(joined, "-ovtt") {
		t.Errorf("выбран лишний формат вывода: %v", args)
	}
}

func TestBuildWhisperArgsFormats(t *testing.T) {
	cases := map[string]string{"txt": "-otxt", "srt": "-osrt", "vtt": "-ovtt", "": "-otxt"}
	for format, flag := range cases {
		args := buildWhisperArgs(whisperArgs{Format: format, Threads: 4, Lang: "auto"})
		if !contains(args, flag) {
			t.Errorf("формат %q: нет флага %s (%v)", format, flag, args)
		}
	}
}

func TestBuildExtractArgs(t *testing.T) {
	args := buildExtractArgs("/видео/мой файл.mp4", "/tmp/out.wav")
	joined := strings.Join(args, " ")
	for _, want := range []string{"-i /видео/мой файл.mp4", "-ac 1", "-ar 16000",
		"-c:a pcm_s16le", "-vn", "-progress pipe:1", "/tmp/out.wav"} {
		if !strings.Contains(joined, want) {
			t.Errorf("в аргументах ffmpeg нет %q: %v", want, args)
		}
	}
	// Путь передаётся отдельным аргументом — кавычки и экранирование не нужны.
	if !contains(args, "/видео/мой файл.mp4") {
		t.Error("путь с пробелом и кириллицей должен идти одним аргументом")
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// --- пути и валидация ---

func TestUniqueOutPath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Моё видео 2026.mp4")

	got := uniqueOutPath(src, "txt")
	want := filepath.Join(dir, "Моё видео 2026.txt")
	if got != want {
		t.Fatalf("получили %q, хотели %q", got, want)
	}

	if err := os.WriteFile(want, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = uniqueOutPath(src, "txt")
	want = filepath.Join(dir, "Моё видео 2026 (2).txt")
	if got != want {
		t.Fatalf("при занятом имени получили %q, хотели %q", got, want)
	}
}

func TestNormalizePath(t *testing.T) {
	if got := normalizePath("file:///Users/rocket/Моё%20видео.mp4"); got != "/Users/rocket/Моё видео.mp4" {
		t.Errorf("file:// разобран как %q", got)
	}
	home, _ := os.UserHomeDir()
	if got := normalizePath("~/видео.mp4"); got != filepath.Join(home, "видео.mp4") {
		t.Errorf("тильда разобрана как %q", got)
	}
	if got := normalizePath("   /tmp/a.mp4  "); got != "/tmp/a.mp4" {
		t.Errorf("пробелы по краям не убрались: %q", got)
	}
}

func TestValidateTranscribeRequest(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "запись 01.mp4")
	if err := os.WriteFile(video, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Значения по умолчанию: русский язык, txt, модель по умолчанию.
	req := transcribeRequest{Path: video}
	if code, err := validateTranscribeRequest(&req); err != nil {
		t.Fatalf("валидный запрос отклонён: %v (%s)", err, code)
	}
	if req.Lang != "ru" || req.Format != "txt" || req.Model != defaultWhisperModel {
		t.Errorf("умолчания не подставились: %+v", req)
	}

	bad := []struct {
		name string
		req  transcribeRequest
		code string
	}{
		{"нет файла", transcribeRequest{Path: filepath.Join(dir, "нет.mp4")}, "bad_path"},
		{"пустой путь", transcribeRequest{Path: "  "}, "bad_path"},
		{"папка", transcribeRequest{Path: dir}, "bad_path"},
		{"чужое расширение", transcribeRequest{Path: mustFile(t, dir, "текст.txt")}, "bad_format"},
		{"формат вывода", transcribeRequest{Path: video, Format: "docx"}, "bad_format"},
		{"язык", transcribeRequest{Path: video, Lang: "русский"}, "bad_lang"},
		{"модель", transcribeRequest{Path: video, Model: "huge"}, "bad_model"},
	}
	for _, c := range bad {
		r := c.req
		code, err := validateTranscribeRequest(&r)
		if err == nil {
			t.Errorf("%s: ошибки нет, хотя ожидалась", c.name)
			continue
		}
		if code != c.code {
			t.Errorf("%s: код %q, хотели %q", c.name, code, c.code)
		}
	}
}

func mustFile(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// --- состояние задачи ---

func TestJobProgressAndCancel(t *testing.T) {
	job := &whisperJob{st: jobStatus{State: stateQueued}, phaseAt: time.Now()}

	job.setStage(stateExtracting, "Извлечение звука")
	job.setProgress(0, extractShare, 50)
	if p := job.snapshot().Percent; p != 5 {
		t.Errorf("процент = %d, хотели 5 (половина этапа извлечения)", p)
	}
	// Прогресс не должен ехать назад: whisper иногда повторяет старые таймкоды.
	job.setProgress(0, extractShare, 10)
	if p := job.snapshot().Percent; p != 5 {
		t.Errorf("процент откатился назад: %d", p)
	}

	job.stop()
	st := job.snapshot()
	if st.State != stateError || st.Error != "Отменено" {
		t.Errorf("после отмены состояние %+v", st)
	}
	// Отменённую задачу нельзя «завершить успешно» задним числом.
	job.finish("/tmp/out.txt", "текст")
	if job.snapshot().State != stateError {
		t.Error("завершение перезаписало отменённое состояние")
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		512:        "512 Б",
		1536:       "1.5 КБ",
		147951465:  "141.1 МБ",
		1533763059: "1.4 ГБ",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, хотели %q", in, got, want)
		}
	}
}

// --- докачка моделей ---

func TestDownloadWithProgressResume(t *testing.T) {
	payload := strings.Repeat("абв", 5000) // ~30 КБ юникода
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "model.bin", time.Unix(0, 0), strings.NewReader(payload))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "ggml-test.bin")
	// Изображаем прерванную загрузку: часть файла уже лежит в .part.
	half := len(payload) / 2
	if err := os.WriteFile(dest+".part", []byte(payload[:half]), 0o644); err != nil {
		t.Fatal(err)
	}

	var lastDone, lastTotal int64
	err := downloadWithProgress(context.Background(), srv.URL, dest, int64(len(payload)),
		func(done, total int64) { lastDone, lastTotal = done, total })
	if err != nil {
		t.Fatalf("докачка не удалась: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != payload {
		t.Fatalf("итоговый файл повреждён: %d байт вместо %d", len(got), len(payload))
	}
	if lastDone != int64(len(payload)) || lastTotal != int64(len(payload)) {
		t.Errorf("прогресс закончился на %d из %d", lastDone, lastTotal)
	}
	if fileExists(dest + ".part") {
		t.Error(".part-файл остался после успешной загрузки")
	}
}

func TestDownloadWithProgressCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10000000")
		for i := 0; i < 100; i++ {
			if _, err := w.Write(make([]byte, 1000)); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			time.Sleep(20 * time.Millisecond)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "ggml-test.bin")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	if err := downloadWithProgress(ctx, srv.URL, dest, 0, nil); err == nil {
		t.Fatal("отменённая загрузка завершилась успешно")
	}
	if fileExists(dest) {
		t.Error("после отмены появился готовый файл")
	}
	// Недокачанное сохраняем: следующая попытка продолжит с этого места.
	if !fileExists(dest + ".part") {
		t.Error("после отмены не осталось .part для докачки")
	}
}

// TestDownloadWithProgressStalled проверяет, что молчащее соединение не висит вечно.
func TestDownloadWithProgressStalled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		_, _ = w.Write(make([]byte, 1000))
		w.(http.Flusher).Flush()
		// Дальше сервер «замолкает» — ровно тот случай, когда прогресс замирает.
		<-r.Context().Done()
	}))
	defer srv.Close()

	old := stallTimeout
	stallTimeout = 300 * time.Millisecond
	defer func() { stallTimeout = old }()

	dest := filepath.Join(t.TempDir(), "ggml-test.bin")
	err := downloadWithProgress(context.Background(), srv.URL, dest, 0, nil)
	if err == nil {
		t.Fatal("зависшая загрузка завершилась без ошибки")
	}
	if !strings.Contains(err.Error(), "загрузка встала") {
		t.Errorf("непонятная ошибка: %v", err)
	}
	if !fileExists(dest + ".part") {
		t.Error("недокачанное не сохранено для докачки")
	}
}

// --- интеграционный прогон ---

// TestTranscribeIntegration прогоняет весь пайплайн на коротком сгенерированном
// аудио с моделью base. Пропускается, если whisper или модель ещё не установлены
// (в CI без них тест смысла не имеет).
func TestTranscribeIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("интеграционный тест: пропуск в режиме -short")
	}
	ff := lookupFfmpeg()
	if ff == "" {
		t.Skip("нет ffmpeg — пропускаем")
	}
	bin := findWhisperBinary(toolsDir())
	if bin == "" {
		t.Skip("нет whisper-cli — пропускаем (поставьте через /api/whisper/install)")
	}
	model, ok := modelFilePath("base")
	if !ok {
		t.Skipf("нет модели base в %s — пропускаем", modelsDir())
	}
	t.Logf("whisper: %s, модель: %s", bin, model)

	ffmpegPath = ff
	setWhisperBinary(bin)

	// Имя с пробелом и кириллицей — заодно проверяем пути.
	dir := t.TempDir()
	src := filepath.Join(dir, "тестовое аудио.wav")
	gen := exec.Command(ff, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("не удалось сгенерировать аудио: %v %s", err, out)
	}

	job, err := transcriber.submitTranscribe(transcribeRequest{
		Path: src, Lang: "ru", Model: "base", Format: "txt",
	})
	if err != nil {
		t.Fatalf("задача не поставилась: %v", err)
	}

	deadline := time.Now().Add(3 * time.Minute)
	var st jobStatus
	for time.Now().Before(deadline) {
		st = job.snapshot()
		if st.State == stateDone || st.State == stateError {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if st.State != stateDone {
		t.Fatalf("состояние %q, ошибка: %s", st.State, st.Error)
	}
	if st.Percent != 100 {
		t.Errorf("процент по завершении = %d", st.Percent)
	}
	if !fileExists(st.OutPath) {
		t.Fatalf("файл результата не создан: %s", st.OutPath)
	}
	if filepath.Dir(st.OutPath) != dir {
		t.Errorf("результат сохранён не рядом с исходником: %s", st.OutPath)
	}
	t.Logf("результат: %s", st.OutPath)
}

// TestTranscribeNoAudioTrack проверяет понятную ошибку для видео без звука.
func TestTranscribeNoAudioTrack(t *testing.T) {
	if testing.Short() {
		t.Skip("интеграционный тест: пропуск в режиме -short")
	}
	ff := lookupFfmpeg()
	if ff == "" {
		t.Skip("нет ffmpeg — пропускаем")
	}
	ffmpegPath = ff

	dir := t.TempDir()
	src := filepath.Join(dir, "без звука.mp4")
	gen := exec.Command(ff, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black:s=320x240:d=2", "-c:v", "libx264", "-pix_fmt", "yuv420p", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("не удалось сгенерировать видео без звука: %v %s", err, out)
	}

	_, hasAudio, err := probeMedia(context.Background(), src)
	if err != nil {
		t.Fatalf("probeMedia: %v", err)
	}
	if hasAudio {
		t.Error("в видео без звука нашлась звуковая дорожка")
	}
}

// lookupFfmpeg ищет ffmpeg так же, как программа: сначала свой, потом системный.
func lookupFfmpeg() string {
	name := "ffmpeg"
	local := filepath.Join(toolsDir(), name)
	if fileExists(local) {
		return local
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}
