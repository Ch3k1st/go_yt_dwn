package main

// Локальная транскрибация видео и аудио через whisper.cpp.
//
// Пайплайн: исходный файл → FFmpeg (16 кГц, моно, PCM 16 бит) → whisper-cli →
// текст рядом с исходным файлом (txt / srt / vtt).
//
// Всё считается локально, интернет нужен только один раз — чтобы скачать
// бинарник whisper и модель (см. whisper_install.go).

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- состояния задачи (значения из контракта API) ---

const (
	stateQueued       = "queued"
	stateExtracting   = "extracting"
	stateTranscribing = "transcribing"
	stateDone         = "done"
	stateError        = "error"
)

// extractShare — доля общего прогресса, отданная извлечению звука.
// Остальное (10..100) занимает распознавание: оно на порядок дольше.
const extractShare = 10

// previewLimit — сколько символов распознанного текста отдаём в превью.
const previewLimit = 800

// jobStatus — ответ /api/transcribe/progress. Поля state..preview заданы контрактом,
// stage — необязательное человекочитаемое пояснение (например «Сборка whisper.cpp»).
type jobStatus struct {
	State   string `json:"state"`
	Percent int    `json:"percent"`
	ETA     int    `json:"eta"`
	Error   string `json:"error"`
	OutPath string `json:"outPath"`
	Preview string `json:"preview"`
	Stage   string `json:"stage,omitempty"`
}

// whisperJob — одна задача: транскрибация файла или установка модели.
// Все поля читаются HTTP-обработчиками из другой горутины, поэтому под мьютексом.
type whisperJob struct {
	id string

	mu       sync.Mutex
	st       jobStatus
	canceled bool
	done     bool
	finished time.Time
	phaseAt  time.Time // начало текущей фазы — от неё считается ETA

	cancel context.CancelFunc
}

func (j *whisperJob) setStage(state, stage string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.done {
		return
	}
	if j.st.State != state {
		j.phaseAt = time.Now()
	}
	j.st.State = state
	j.st.Stage = stage
}

// setProgress выставляет общий процент; eta считается по времени текущей фазы.
// phaseFrom/phaseTo — участок общей шкалы, который занимает текущая фаза.
func (j *whisperJob) setProgress(phaseFrom, phaseTo, phasePercent float64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.done {
		return
	}
	if phasePercent < 0 {
		phasePercent = 0
	}
	if phasePercent > 100 {
		phasePercent = 100
	}
	overall := phaseFrom + (phaseTo-phaseFrom)*phasePercent/100
	if p := int(overall); p > j.st.Percent {
		j.st.Percent = p
	}
	if phasePercent > 1 && !j.phaseAt.IsZero() {
		elapsed := time.Since(j.phaseAt).Seconds()
		remaining := elapsed * (100 - phasePercent) / phasePercent
		j.st.ETA = int(remaining + 0.5)
	}
}

// resetPhaseClock перезапускает отсчёт времени фазы — от него считается ETA.
func (j *whisperJob) resetPhaseClock() {
	j.mu.Lock()
	j.phaseAt = time.Now()
	j.mu.Unlock()
}

func (j *whisperJob) setPreview(text string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.st.Preview = text
}

func (j *whisperJob) fail(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.done {
		return
	}
	j.done = true
	j.finished = time.Now()
	j.st.State = stateError
	j.st.ETA = 0
	j.st.Error = err.Error()
}

func (j *whisperJob) finish(outPath, preview string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.done {
		return
	}
	j.done = true
	j.finished = time.Now()
	j.st.State = stateDone
	j.st.Percent = 100
	j.st.ETA = 0
	j.st.Stage = ""
	j.st.OutPath = outPath
	if preview != "" {
		j.st.Preview = preview
	}
}

func (j *whisperJob) snapshot() jobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.st
}

func (j *whisperJob) isCanceled() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.canceled
}

// stop помечает задачу отменённой и убивает её процессы (через контекст).
func (j *whisperJob) stop() {
	j.mu.Lock()
	if j.done {
		j.mu.Unlock()
		return
	}
	j.canceled = true
	cancel := j.cancel
	j.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	j.mu.Lock()
	if !j.done {
		j.done = true
		j.finished = time.Now()
		j.st.State = stateError
		j.st.ETA = 0
		j.st.Error = "Отменено"
	}
	j.mu.Unlock()
}

// --- менеджер задач ---

// transcribeRequest — тело POST /api/transcribe.
type transcribeRequest struct {
	Path   string `json:"path"`
	Lang   string `json:"lang"`
	Model  string `json:"model"`
	Format string `json:"format"`
}

type transcribeTask struct {
	job *whisperJob
	req transcribeRequest
	ctx context.Context
}

// whisperManager хранит задачи и держит очередь: whisper запускается строго
// по одному процессу — модель занимает всю память и GPU, параллельный запуск
// только замедлил бы обе задачи.
type whisperManager struct {
	mu      sync.Mutex
	jobs    map[string]*whisperJob
	tasks   chan *transcribeTask
	seq     int
	running int // сколько задач реально выполняется (для busy)

	once sync.Once
}

var transcriber = &whisperManager{
	jobs:  make(map[string]*whisperJob),
	tasks: make(chan *transcribeTask, 32),
}

// start поднимает единственного воркера очереди и чистит мусор от прошлых запусков.
func (m *whisperManager) start() {
	m.once.Do(func() {
		go cleanupStaleTemps()
		go func() {
			for task := range m.tasks {
				if task.job.isCanceled() {
					continue
				}
				m.setRunning(1)
				m.runTranscribe(task)
				m.setRunning(-1)
			}
		}()
	})
}

func (m *whisperManager) setRunning(delta int) {
	m.mu.Lock()
	m.running += delta
	m.mu.Unlock()
}

func (m *whisperManager) busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running > 0
}

func (m *whisperManager) job(id string) (*whisperJob, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}

// prune убирает давно завершённые задачи, чтобы карта не росла бесконечно.
// Вызывается из-под m.mu.
func (m *whisperManager) prune() {
	const keep = 30 * time.Minute
	for id, j := range m.jobs {
		j.mu.Lock()
		old := j.done && time.Since(j.finished) > keep
		j.mu.Unlock()
		if old {
			delete(m.jobs, id)
		}
	}
}

// submitTranscribe ставит файл в очередь. Валидация запроса уже пройдена.
func (m *whisperManager) submitTranscribe(req transcribeRequest) (*whisperJob, error) {
	m.start()

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.prune()
	m.seq++
	id := fmt.Sprintf("t%d-%d", time.Now().Unix(), m.seq)
	job := &whisperJob{
		id:      id,
		st:      jobStatus{State: stateQueued, Stage: "В очереди"},
		cancel:  cancel,
		phaseAt: time.Now(),
	}
	m.jobs[id] = job
	m.mu.Unlock()

	select {
	case m.tasks <- &transcribeTask{job: job, req: req, ctx: ctx}:
		return job, nil
	default:
		cancel()
		m.mu.Lock()
		delete(m.jobs, id)
		m.mu.Unlock()
		return nil, errors.New("очередь транскрибации переполнена, попробуйте позже")
	}
}

// --- сам пайплайн ---

func (m *whisperManager) runTranscribe(task *transcribeTask) {
	job, req := task.job, task.req
	ctx := task.ctx

	job.setStage(stateExtracting, "Анализ файла")

	// 1. Пробуем файл ffmpeg-ом: заодно узнаём длительность и есть ли звук.
	dur, hasAudio, err := probeMedia(ctx, req.Path)
	if err != nil {
		job.fail(err)
		return
	}
	if !hasAudio {
		job.fail(errors.New("в файле нет звуковой дорожки — распознавать нечего"))
		return
	}

	// 2. Временная папка в системной temp: длинные записи не держим в памяти,
	// wav пишется на диск и удаляется в любом случае.
	tmpDir, err := os.MkdirTemp("", tempPrefix)
	if err != nil {
		job.fail(fmt.Errorf("не удалось создать временную папку: %w", err))
		return
	}
	defer os.RemoveAll(tmpDir)

	// 3. Место под wav: 16 кГц моно 16 бит = 32 КБ на секунду звука.
	if need := int64(dur*wavBytesPerSecond*1.1) + 16<<20; dur > 0 {
		if free, err := freeDiskSpace(tmpDir); err == nil && free < need {
			job.fail(fmt.Errorf("недостаточно места во временной папке: нужно %s, свободно %s",
				humanBytes(need), humanBytes(free)))
			return
		}
	}

	wav := filepath.Join(tmpDir, "audio.wav")
	job.setStage(stateExtracting, "Извлечение звука")
	if err := extractAudio(ctx, job, req.Path, wav, dur); err != nil {
		job.fail(canceledOr(ctx, err))
		return
	}

	// 4. Распознавание (состояние и стадии выставляет runWhisperCLI).
	outPath, preview, err := runWhisperCLI(ctx, job, wav, req, dur)
	if err != nil {
		job.fail(canceledOr(ctx, err))
		return
	}
	job.finish(outPath, preview)
}

// canceledOr подменяет техническую ошибку убитого процесса на понятное «Отменено».
func canceledOr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errors.New("Отменено")
	}
	return err
}

const (
	wavBytesPerSecond = 32000 // 16000 Гц × 2 байта × 1 канал
	tempPrefix        = "vdown-whisper-"
)

// probeMedia спрашивает у ffmpeg длительность и наличие звуковой дорожки.
// ffmpeg без выходного файла всегда завершается с ошибкой — нас интересует только
// его отчёт о потоках, поэтому код возврата игнорируем и разбираем текст.
func probeMedia(ctx context.Context, path string) (durSec float64, hasAudio bool, err error) {
	cmd := exec.CommandContext(ctx, ffmpegPath, "-hide_banner", "-nostdin", "-i", path)
	out, _ := cmd.CombinedOutput()
	text := string(out)

	if ctx.Err() != nil {
		return 0, false, errors.New("Отменено")
	}
	if !strings.Contains(text, "Duration:") && !strings.Contains(text, "Stream #") {
		return 0, false, fmt.Errorf("ffmpeg не смог прочитать файл: %s", tail(text))
	}
	dur, audio := parseProbeOutput(text)
	return dur, audio, nil
}

var (
	reProbeDuration = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)
	reProbeAudio    = regexp.MustCompile(`Stream #\d+:\d+.*: Audio:`)
	reFfmpegOutTime = regexp.MustCompile(`^out_time_(us|ms)=(\d+)`)
	reWhisperPct    = regexp.MustCompile(`progress\s*=\s*(\d+)%`)
	reWhisperSeg    = regexp.MustCompile(`^\[\d+:\d+:\d+\.\d+ --> (\d+):(\d+):(\d+\.\d+)\]\s*(.*)$`)
)

// parseProbeOutput достаёт длительность (сек) и признак звуковой дорожки из вывода ffmpeg -i.
func parseProbeOutput(text string) (durSec float64, hasAudio bool) {
	if mm := reProbeDuration.FindStringSubmatch(text); mm != nil {
		h, _ := strconv.ParseFloat(mm[1], 64)
		m, _ := strconv.ParseFloat(mm[2], 64)
		s, _ := strconv.ParseFloat(mm[3], 64)
		durSec = h*3600 + m*60 + s
	}
	hasAudio = reProbeAudio.MatchString(text)
	return durSec, hasAudio
}

// parseFfmpegProgress разбирает строку из `ffmpeg -progress pipe:1`.
// Ключ out_time_us — микросекунды; out_time_ms исторически тоже микросекунды.
func parseFfmpegProgress(line string) (sec float64, ok bool) {
	mm := reFfmpegOutTime.FindStringSubmatch(strings.TrimSpace(line))
	if mm == nil {
		return 0, false
	}
	us, err := strconv.ParseInt(mm[2], 10, 64)
	if err != nil {
		return 0, false
	}
	return float64(us) / 1e6, true
}

// parseWhisperProgress разбирает строку `whisper_print_progress_callback: progress =  42%`.
func parseWhisperProgress(line string) (percent int, ok bool) {
	if !strings.Contains(line, "progress") {
		return 0, false
	}
	mm := reWhisperPct.FindStringSubmatch(line)
	if mm == nil {
		return 0, false
	}
	p, err := strconv.Atoi(mm[1])
	if err != nil {
		return 0, false
	}
	return p, true
}

// parseWhisperSegment разбирает строку сегмента:
// `[00:00:03.000 --> 00:00:07.480]   текст` → конец сегмента в секундах и сам текст.
func parseWhisperSegment(line string) (endSec float64, text string, ok bool) {
	mm := reWhisperSeg.FindStringSubmatch(strings.TrimSpace(line))
	if mm == nil {
		return 0, "", false
	}
	h, _ := strconv.ParseFloat(mm[1], 64)
	m, _ := strconv.ParseFloat(mm[2], 64)
	s, _ := strconv.ParseFloat(mm[3], 64)
	return h*3600 + m*60 + s, strings.TrimSpace(mm[4]), true
}

// buildExtractArgs собирает аргументы ffmpeg для получения wav 16 кГц моно.
// -vn выкидывает видео, -map 0:a:0 берёт первую звуковую дорожку (в фильмах их бывает
// несколько), -progress pipe:1 даёт машинный прогресс на stdout.
func buildExtractArgs(input, output string) []string {
	return []string{
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-i", input,
		"-vn", "-map", "0:a:0",
		"-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le",
		"-progress", "pipe:1", "-nostats",
		output,
	}
}

// extractAudio запускает ffmpeg и двигает прогресс задачи по мере обработки.
func extractAudio(ctx context.Context, job *whisperJob, input, output string, dur float64) error {
	cmd := exec.CommandContext(ctx, ffmpegPath, buildExtractArgs(input, output)...)
	last, err := runLines(cmd, func(line string) {
		if sec, ok := parseFfmpegProgress(line); ok && dur > 0 {
			job.setProgress(0, extractShare, sec/dur*100)
		}
	})
	if err != nil {
		return fmt.Errorf("не удалось извлечь звук: %s", firstMeaningful(last, err))
	}
	if st, statErr := os.Stat(output); statErr != nil || st.Size() < 1024 {
		return errors.New("звуковая дорожка пуста — распознавать нечего")
	}
	job.setProgress(0, extractShare, 100)
	return nil
}

// whisperArgs — параметры запуска whisper-cli.
type whisperArgs struct {
	Model      string // путь к ggml-*.bin
	Audio      string // путь к wav 16 кГц
	OutPrefix  string // префикс выходного файла (без расширения)
	Lang       string // ru | en | auto
	Format     string // txt | srt | vtt
	Threads    int
	MaxContext int
}

// buildWhisperArgs собирает командную строку whisper-cli.
func buildWhisperArgs(a whisperArgs) []string {
	args := []string{
		"-m", a.Model,
		"-f", a.Audio,
		"-of", a.OutPrefix,
		"-l", a.Lang,
		"-t", strconv.Itoa(a.Threads),
		"-pp", // печатать прогресс — по нему двигаем полосу
	}
	switch a.Format {
	case "srt":
		args = append(args, "-osrt")
	case "vtt":
		args = append(args, "-ovtt")
	default:
		args = append(args, "-otxt")
	}
	return args
}

// runWhisperCLI распознаёт wav и кладёт результат рядом с исходным файлом.
// whisper пишет в свою временную папку, а мы переносим готовый файл — так проще
// поймать ошибку «папка только для чтения» и не оставить мусор при отмене.
func runWhisperCLI(ctx context.Context, job *whisperJob, wav string, req transcribeRequest, dur float64) (outPath, preview string, err error) {
	modelPath, ok := modelFilePath(req.Model)
	if !ok {
		return "", "", fmt.Errorf("модель %q не установлена", req.Model)
	}
	bin := whisperBinary()
	if bin == "" {
		return "", "", errors.New("whisper.cpp не установлен — нажмите «Установить»")
	}

	tmpPrefix := filepath.Join(filepath.Dir(wav), "result")
	args := buildWhisperArgs(whisperArgs{
		Model:     modelPath,
		Audio:     wav,
		OutPrefix: tmpPrefix,
		Lang:      req.Lang,
		Format:    req.Format,
		Threads:   whisperThreads(),
	})

	// Загрузка модели в память (у medium это полтора гигабайта) занимает секунды,
	// и прогресса на этом этапе нет — честно говорим об этом в stage.
	job.setStage(stateTranscribing, "Загрузка модели «"+req.Model+"»")
	started := false
	markStarted := func() {
		if !started {
			started = true
			job.setStage(stateTranscribing, "Распознавание речи")
			// Время загрузки модели не должно попадать в расчёт ETA распознавания.
			job.resetPhaseClock()
		}
	}

	var sb strings.Builder
	cmd := exec.CommandContext(ctx, bin, args...)
	last, runErr := runLines(cmd, func(line string) {
		if p, ok := parseWhisperProgress(line); ok {
			markStarted()
			job.setProgress(extractShare, 100, float64(p))
			return
		}
		end, text, ok := parseWhisperSegment(line)
		if !ok {
			return
		}
		markStarted()
		if dur > 0 {
			job.setProgress(extractShare, 100, end/dur*100)
		}
		if sb.Len() < previewLimit && text != "" {
			if sb.Len() > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(text)
			job.setPreview(sb.String())
		}
	})
	if runErr != nil {
		return "", "", fmt.Errorf("whisper завершился с ошибкой: %s", firstMeaningful(last, runErr))
	}

	produced := tmpPrefix + "." + req.Format
	if !fileExists(produced) {
		return "", "", errors.New("whisper не создал файл с результатом")
	}
	outPath = uniqueOutPath(req.Path, req.Format)
	if err := moveFile(produced, outPath); err != nil {
		return "", "", fmt.Errorf("не удалось сохранить результат рядом с видео: %w", err)
	}
	return outPath, sb.String(), nil
}

// whisperThreads — сколько потоков отдать декодеру. Больше 8 смысла не имеет:
// на Apple Silicon энкодер и так считается на GPU (Metal).
func whisperThreads() int {
	n := runtime.NumCPU()
	if n > 8 {
		n = 8
	}
	if n < 1 {
		n = 1
	}
	return n
}

// uniqueOutPath подбирает имя результата рядом с исходным файлом,
// не затирая уже существующий: «видео.txt», «видео (2).txt» и так далее.
func uniqueOutPath(srcPath, format string) string {
	dir := filepath.Dir(srcPath)
	base := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	candidate := filepath.Join(dir, base+"."+format)
	for i := 2; fileExists(candidate) && i < 1000; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d).%s", base, i, format))
	}
	return candidate
}

// moveFile переносит файл, с запасным вариантом «скопировать и удалить»:
// временная папка и папка с видео могут быть на разных дисках.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Remove(src)
	return nil
}

// cleanupStaleTemps подчищает временные папки, оставшиеся от аварийно завершённых
// запусков (например, программу убили во время распознавания).
func cleanupStaleTemps() {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), tempPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < 24*time.Hour {
			continue
		}
		_ = os.RemoveAll(filepath.Join(os.TempDir(), e.Name()))
	}
}

// --- валидация запроса ---

// supportedMediaExt — расширения, которые имеет смысл отдавать ffmpeg.
var supportedMediaExt = map[string]bool{
	".mp4": true, ".m4v": true, ".mkv": true, ".webm": true, ".mov": true,
	".avi": true, ".flv": true, ".ts": true, ".mpg": true, ".mpeg": true,
	".wmv": true, ".3gp": true, ".ogv": true,
	".mp3": true, ".m4a": true, ".aac": true, ".wav": true, ".flac": true,
	".ogg": true, ".opus": true, ".aiff": true, ".aif": true, ".wma": true,
	".mka": true, ".amr": true,
}

var supportedFormats = map[string]bool{"txt": true, "srt": true, "vtt": true}

// normalizePath приводит то, что прислал интерфейс, к обычному пути:
// поддерживает file://-ссылки и «~» в начале.
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "file://") {
		if u, err := url.Parse(p); err == nil {
			if decoded, err := url.PathUnescape(u.Path); err == nil {
				p = decoded
			} else {
				p = u.Path
			}
		}
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return filepath.Clean(p)
}

var reLangCode = regexp.MustCompile(`^[a-z]{2}$`)

// validateTranscribeRequest проверяет запрос и подставляет значения по умолчанию.
// Второе возвращаемое значение — машинный код ошибки для интерфейса.
func validateTranscribeRequest(req *transcribeRequest) (code string, err error) {
	req.Path = normalizePath(req.Path)
	if req.Path == "" {
		return "bad_path", errors.New("не указан файл")
	}
	info, statErr := os.Stat(req.Path)
	if statErr != nil {
		return "bad_path", fmt.Errorf("файл не найден: %s", req.Path)
	}
	if info.IsDir() {
		return "bad_path", errors.New("указана папка, а нужен файл")
	}
	ext := strings.ToLower(filepath.Ext(req.Path))
	if !supportedMediaExt[ext] {
		return "bad_format", fmt.Errorf("формат %s не поддерживается", ext)
	}

	req.Format = strings.ToLower(strings.TrimSpace(req.Format))
	if req.Format == "" {
		req.Format = "txt"
	}
	if !supportedFormats[req.Format] {
		return "bad_format", fmt.Errorf("неизвестный формат вывода: %s", req.Format)
	}

	req.Lang = strings.ToLower(strings.TrimSpace(req.Lang))
	if req.Lang == "" {
		req.Lang = defaultWhisperLang
	}
	if req.Lang != "auto" && !reLangCode.MatchString(req.Lang) {
		return "bad_lang", fmt.Errorf("неизвестный язык: %s", req.Lang)
	}

	req.Model = strings.ToLower(strings.TrimSpace(req.Model))
	if req.Model == "" {
		req.Model = defaultWhisperModel
	}
	if !knownModel(req.Model) {
		return "bad_model", fmt.Errorf("неизвестная модель: %s", req.Model)
	}
	return "", nil
}

// --- HTTP-обработчики ---

func handleTranscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "Нужен POST")
		return
	}
	var req transcribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "Некорректный запрос")
		return
	}
	if code, err := validateTranscribeRequest(&req); err != nil {
		writeErrCode(w, http.StatusBadRequest, err.Error(), code)
		return
	}
	if whisperBinary() == "" {
		writeErrCode(w, http.StatusBadRequest,
			"whisper.cpp не установлен — установите его кнопкой «Установить»", "whisper_missing")
		return
	}
	if _, ok := modelFilePath(req.Model); !ok {
		writeErrCode(w, http.StatusBadRequest,
			fmt.Sprintf("модель «%s» не скачана — установите её кнопкой «Установить»", req.Model), "model_missing")
		return
	}

	job, err := transcriber.submitTranscribe(req)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, map[string]string{"jobId": job.id})
}

func handleTranscribeProgress(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeErr(w, http.StatusBadRequest, "Не указан id задачи")
		return
	}
	job, ok := transcriber.job(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "Задача не найдена")
		return
	}
	writeJSON(w, job.snapshot())
}

func handleTranscribeCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "Нужен POST")
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "Некорректный запрос")
		return
	}
	job, ok := transcriber.job(strings.TrimSpace(req.ID))
	if !ok {
		writeErr(w, http.StatusNotFound, "Задача не найдена")
		return
	}
	job.stop()
	writeJSON(w, map[string]bool{"ok": true})
}

// whisperStatusResponse — ответ GET /api/whisper/status.
type whisperStatusResponse struct {
	Installed bool               `json:"installed"`
	Models    []whisperModelInfo `json:"models"`
	Busy      bool               `json:"busy"`
	Default   string             `json:"default"`
	Note      string             `json:"note"`
}

func handleWhisperStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, whisperStatusResponse{
		Installed: whisperBinary() != "",
		Models:    modelCatalog(),
		Busy:      transcriber.busy(),
		Default:   defaultWhisperModel,
		Note:      "whisper не разделяет говорящих: диаризации нет, реплики разных людей идут одним потоком текста",
	})
}

func handleWhisperInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "Нужен POST")
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "Некорректный запрос")
		return
	}
	model := strings.ToLower(strings.TrimSpace(req.Model))
	if model != "" && !knownModel(model) {
		writeErrCode(w, http.StatusBadRequest, "неизвестная модель: "+model, "bad_model")
		return
	}
	job := transcriber.submitInstall(model)
	writeJSON(w, map[string]string{"jobId": job.id})
}

// handleReveal показывает файл в проводнике (Finder / Explorer).
// В контракте этого нет, но кнопка «открыть в Finder» из браузера иначе не работает.
func handleReveal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "Нужен POST")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "Некорректный запрос")
		return
	}
	path := normalizePath(req.Path)
	if path == "" {
		writeErr(w, http.StatusBadRequest, "Не указан путь")
		return
	}
	if _, err := os.Stat(path); err != nil {
		writeErr(w, http.StatusNotFound, "Файл не найден")
		return
	}
	revealInFileManager(path)
	writeJSON(w, map[string]bool{"ok": true})
}

func revealInFileManager(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	case "windows":
		cmd = exec.Command("explorer", "/select,"+filepath.FromSlash(path))
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}
	_ = cmd.Start()
}

// writeErrCode — как writeErr, но с машинным кодом ошибки для интерфейса.
func writeErrCode(w http.ResponseWriter, status int, msg, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg, "code": code})
}

// --- мелкие утилиты ---

// humanBytes печатает размер по-человечески: «1.5 ГБ».
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d Б", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 3; v /= unit {
		div *= unit
		exp++
	}
	names := []string{"КБ", "МБ", "ГБ", "ТБ"}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), names[exp])
}

// firstMeaningful выбирает, что показать пользователю: вывод процесса или ошибку запуска.
func firstMeaningful(output string, err error) string {
	if s := strings.TrimSpace(output); s != "" {
		return s
	}
	return err.Error()
}

// runLines запускает процесс, отдаёт каждую строку его вывода (stdout и stderr вместе)
// в onLine и возвращает хвост вывода — его показываем в сообщении об ошибке.
//
// stdout и stderr сведены в один pipe: whisper печатает сегменты в stdout, а прогресс
// в stderr, и разбирать их удобнее одним сканером.
func runLines(cmd *exec.Cmd, onLine func(string)) (tailOutput string, err error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return "", err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		return "", err
	}
	pw.Close() // родителю своя копия конца трубы больше не нужна

	var lastLines []string
	scanner := bufio.NewScanner(pr)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if s := strings.TrimSpace(line); s != "" {
			lastLines = append(lastLines, s)
			if len(lastLines) > 5 {
				lastLines = lastLines[1:]
			}
		}
		onLine(line)
	}
	pr.Close()

	waitErr := cmd.Wait()
	return strings.Join(lastLines, " | "), waitErr
}
