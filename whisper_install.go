package main

// Установка whisper.cpp и моделей распознавания — тем же принципом, что yt-dlp и
// ffmpeg в deps.go: сначала ищем в tools/ и в PATH, и только потом качаем к себе,
// ничего не устанавливая в систему.
//
// Windows: берём готовый бинарник из релиза whisper.cpp.
// macOS и Linux: готовых бинарников в релизах нет, поэтому собираем из исходников.
// На Apple Silicon сборка идёт с Metal — на M-чипах это разница в разы.

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// Версии зафиксированы: сборка должна быть воспроизводимой, а не «что там сегодня в latest».
	whisperVersion = "v1.9.1"
	cmakeVersion   = "4.4.2"

	modelBaseURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/"

	// defaultWhisperModel — компромисс скорости и качества на русском (замеры в docs/whisper.md).
	defaultWhisperModel = "small"
	defaultWhisperLang  = "ru"
)

// modelSpec — модель из каталога. size нужен, чтобы заранее проверить место на диске
// и показать размер до начала скачивания.
type modelSpec struct {
	name string
	file string
	size int64
}

var whisperModelSpecs = []modelSpec{
	{"base", "ggml-base.bin", 147951465},
	{"small", "ggml-small.bin", 487601967},
	{"medium", "ggml-medium.bin", 1533763059},
	{"large-v3", "ggml-large-v3.bin", 3095033483},
}

// whisperModelInfo — элемент списка моделей в /api/whisper/status.
type whisperModelInfo struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Downloaded bool   `json:"downloaded"`
}

func findModelSpec(name string) (modelSpec, bool) {
	for _, m := range whisperModelSpecs {
		if m.name == name {
			return m, true
		}
	}
	return modelSpec{}, false
}

func knownModel(name string) bool {
	_, ok := findModelSpec(name)
	return ok
}

// modelsDir — папка с моделями рядом с остальными зависимостями.
func modelsDir() string {
	d := filepath.Join(toolsDir(), "models")
	_ = os.MkdirAll(d, 0o755)
	return d
}

// modelFilePath возвращает путь к скачанной модели; false — если её ещё нет.
func modelFilePath(name string) (string, bool) {
	spec, ok := findModelSpec(name)
	if !ok {
		return "", false
	}
	p := filepath.Join(modelsDir(), spec.file)
	if !fileExists(p) {
		return "", false
	}
	return p, true
}

func modelCatalog() []whisperModelInfo {
	dir := modelsDir()
	out := make([]whisperModelInfo, 0, len(whisperModelSpecs))
	for _, m := range whisperModelSpecs {
		out = append(out, whisperModelInfo{
			Name:       m.name,
			Size:       m.size,
			Downloaded: fileExists(filepath.Join(dir, m.file)),
		})
	}
	return out
}

// --- путь к бинарнику whisper ---

var (
	whisperMu   sync.RWMutex
	whisperPath string
)

func whisperBinary() string {
	whisperMu.RLock()
	defer whisperMu.RUnlock()
	return whisperPath
}

func setWhisperBinary(p string) {
	whisperMu.Lock()
	whisperPath = p
	whisperMu.Unlock()
}

// findWhisperBinary ищет уже имеющийся whisper-cli: сначала свой в tools/,
// затем системный (например, поставленный через brew install whisper-cpp).
func findWhisperBinary(dir string) string {
	if local := localWhisperPath(dir); fileExists(local) {
		return local
	}
	for _, name := range []string{"whisper-cli", "whisper-cpp"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// localWhisperPath — куда кладём свой бинарник. На Windows рядом с ним лежат dll
// из релиза, поэтому там отдельная подпапка.
func localWhisperPath(dir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "whisper", "whisper-cli.exe")
	}
	return filepath.Join(dir, "whisper-cli")
}

// --- задача установки ---

// submitInstall запускает установку whisper и/или модели. Пустое имя модели —
// поставить только сам whisper. Повторный вызов во время работы возвращает ту же задачу.
func (m *whisperManager) submitInstall(model string) *whisperJob {
	m.start()

	id := "model:" + model
	if model == "" {
		id = "model:whisper"
	}

	m.mu.Lock()
	if existing, ok := m.jobs[id]; ok {
		existing.mu.Lock()
		active := !existing.done
		existing.mu.Unlock()
		if active {
			m.mu.Unlock()
			return existing
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &whisperJob{
		id:      id,
		st:      jobStatus{State: stateQueued, Stage: "Подготовка"},
		cancel:  cancel,
		phaseAt: time.Now(),
	}
	m.prune()
	m.jobs[id] = job
	m.mu.Unlock()

	go func() {
		m.setRunning(1)
		defer m.setRunning(-1)
		runInstall(ctx, job, model)
	}()
	return job
}

// runInstall ставит бинарник (если его нет) и качает модель.
// Первые binaryShare процентов шкалы — установка whisper, остальное — модель.
func runInstall(ctx context.Context, job *whisperJob, model string) {
	binaryShare := 0.0
	if whisperBinary() == "" {
		binaryShare = 20
		if model == "" {
			binaryShare = 100
		}
		job.setStage(stateExtracting, "Установка whisper.cpp")
		err := ensureWhisperBinary(ctx, func(stage string, pct float64) {
			job.setStage(stateExtracting, stage)
			job.setProgress(0, binaryShare, pct)
		})
		if err != nil {
			job.fail(canceledOr(ctx, fmt.Errorf("не удалось установить whisper.cpp: %w", err)))
			return
		}
		job.setProgress(0, binaryShare, 100)
	}

	if model == "" {
		job.finish(whisperBinary(), "")
		return
	}

	spec, ok := findModelSpec(model)
	if !ok {
		job.fail(fmt.Errorf("неизвестная модель: %s", model))
		return
	}
	dest := filepath.Join(modelsDir(), spec.file)
	if fileExists(dest) {
		// Модель могли докачать другим способом — недокачанный хвост больше не нужен.
		_ = os.Remove(dest + ".part")
		job.finish(dest, "")
		return
	}

	if free, err := freeDiskSpace(modelsDir()); err == nil && free < spec.size+64<<20 {
		job.fail(fmt.Errorf("недостаточно места для модели «%s»: нужно %s, свободно %s",
			spec.name, humanBytes(spec.size), humanBytes(free)))
		return
	}

	job.setStage(stateExtracting, fmt.Sprintf("Скачивание модели «%s» (%s)", spec.name, humanBytes(spec.size)))
	err := downloadWithProgress(ctx, modelBaseURL+spec.file, dest, spec.size, func(done, total int64) {
		if total > 0 {
			job.setProgress(binaryShare, 100, float64(done)/float64(total)*100)
		}
	})
	if err != nil {
		job.fail(canceledOr(ctx, fmt.Errorf("не удалось скачать модель: %w", err)))
		return
	}
	job.finish(dest, "")
}

// --- установка бинарника ---

// ensureWhisperBinary гарантирует наличие whisper-cli, скачивая или собирая его.
func ensureWhisperBinary(ctx context.Context, progress func(stage string, pct float64)) error {
	dir := toolsDir()
	if p := findWhisperBinary(dir); p != "" {
		setWhisperBinary(p)
		return nil
	}
	var err error
	if runtime.GOOS == "windows" {
		err = installWhisperWindows(ctx, dir, progress)
	} else {
		err = buildWhisperFromSource(ctx, dir, progress)
	}
	if err != nil {
		return err
	}
	local := localWhisperPath(dir)
	if !fileExists(local) {
		return errors.New("бинарник whisper не появился после установки")
	}
	setWhisperBinary(local)
	return nil
}

// installWhisperWindows забирает готовую сборку из релиза whisper.cpp:
// в архиве лежит whisper-cli.exe и рядом нужные ему dll.
func installWhisperWindows(ctx context.Context, dir string, progress func(string, float64)) error {
	url := fmt.Sprintf("https://github.com/ggml-org/whisper.cpp/releases/download/%s/whisper-bin-x64.zip", whisperVersion)
	tmpZip := filepath.Join(dir, "whisper-bin.zip")
	defer os.Remove(tmpZip)

	progress("Скачивание whisper.cpp", 0)
	if err := downloadWithProgress(ctx, url, tmpZip, 0, func(done, total int64) {
		if total > 0 {
			progress("Скачивание whisper.cpp", float64(done)/float64(total)*90)
		}
	}); err != nil {
		return err
	}

	progress("Распаковка whisper.cpp", 95)
	target := filepath.Join(dir, "whisper")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	// Из архива нужны только консольный распознаватель и библиотеки рядом с ним.
	return extractAllFromZip(tmpZip, target, func(name string) bool {
		base := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
		return base == "whisper-cli.exe" || strings.HasSuffix(strings.ToLower(base), ".dll")
	})
}

// buildWhisperFromSource собирает whisper-cli из исходников. Нужен только cmake
// (скачиваем портативный, если системного нет) и компилятор из Command Line Tools.
func buildWhisperFromSource(ctx context.Context, dir string, progress func(string, float64)) error {
	if free, err := freeDiskSpace(dir); err == nil && free < 2<<30 {
		return fmt.Errorf("нужно около 2 ГБ свободного места для сборки, свободно %s", humanBytes(free))
	}

	progress("Подготовка cmake", 0)
	cmakeBin, downloaded, err := ensureCMake(ctx, dir, func(pct float64) {
		progress("Скачивание cmake", pct*0.35)
	})
	if err != nil {
		return err
	}

	work, err := os.MkdirTemp(dir, "whisper-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	progress("Скачивание исходников whisper.cpp", 40)
	src := filepath.Join(work, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		return err
	}
	srcArchive := filepath.Join(work, "whisper-src.tar.gz")
	srcURL := fmt.Sprintf("https://github.com/ggml-org/whisper.cpp/archive/refs/tags/%s.tar.gz", whisperVersion)
	// Размер архива заранее неизвестен, поэтому показываем скачанные мегабайты:
	// на медленной сети без этого кажется, что установка зависла.
	if err := downloadWithProgress(ctx, srcURL, srcArchive, 0, func(done, total int64) {
		progress("Скачивание исходников whisper.cpp: "+humanBytes(done), 40)
	}); err != nil {
		return err
	}
	if err := extractTarGz(srcArchive, src); err != nil {
		return err
	}

	build := filepath.Join(work, "build")
	args := []string{
		"-S", src, "-B", build,
		"-DCMAKE_BUILD_TYPE=Release",
		"-DBUILD_SHARED_LIBS=OFF", // один самодостаточный файл, без .dylib рядом
		"-DWHISPER_BUILD_TESTS=OFF",
		"-DWHISPER_BUILD_SERVER=OFF",
		"-DWHISPER_BUILD_EXAMPLES=ON",
		"-DWHISPER_SDL2=OFF",
	}
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		// Metal даёт ускорение на M-чипах; EMBED_LIBRARY зашивает шейдеры внутрь
		// бинарника, чтобы рядом не требовался ggml-metal.metal.
		args = append(args, "-DGGML_METAL=ON", "-DGGML_METAL_EMBED_LIBRARY=ON")
	}

	progress("Настройка сборки", 50)
	if out, err := runQuiet(ctx, cmakeBin, args...); err != nil {
		return fmt.Errorf("cmake configure: %s", firstMeaningful(out, err))
	}

	progress("Сборка whisper.cpp", 60)
	buildArgs := []string{"--build", build, "--config", "Release",
		"-j", strconv.Itoa(runtime.NumCPU()), "--target", "whisper-cli"}
	cmd := exec.CommandContext(ctx, cmakeBin, buildArgs...)
	out, err := runLines(cmd, func(line string) {
		if pct, ok := parseCMakeProgress(line); ok {
			progress("Сборка whisper.cpp", 60+float64(pct)*0.35)
		}
	})
	if err != nil {
		return fmt.Errorf("сборка: %s", firstMeaningful(out, err))
	}

	built := filepath.Join(build, "bin", "whisper-cli")
	if !fileExists(built) {
		return errors.New("после сборки не нашёлся bin/whisper-cli")
	}
	progress("Установка whisper.cpp", 97)
	target := localWhisperPath(dir)
	if err := moveFile(built, target); err != nil {
		return err
	}
	if err := os.Chmod(target, 0o755); err != nil {
		return err
	}
	if downloaded {
		// Портативный cmake нужен был только для сборки — не оставляем 200 МБ мусора.
		_ = os.RemoveAll(filepath.Join(dir, "cmake"))
	}
	progress("Готово", 100)
	return nil
}

var reCMakeProgress = regexp.MustCompile(`^\[\s*(\d+)%\]`)

// parseCMakeProgress вытаскивает процент из строки вида `[ 42%] Building CXX object ...`.
func parseCMakeProgress(line string) (percent int, ok bool) {
	mm := reCMakeProgress.FindStringSubmatch(strings.TrimSpace(line))
	if mm == nil {
		return 0, false
	}
	p, err := strconv.Atoi(mm[1])
	if err != nil {
		return 0, false
	}
	return p, true
}

// ensureCMake возвращает путь к cmake: системный, ранее скачанный или свежескачанный.
// Второе значение — скачивали ли мы его сами (такой потом можно удалить).
func ensureCMake(ctx context.Context, dir string, progress func(float64)) (path string, downloaded bool, err error) {
	local := localCMakePath(dir)
	if fileExists(local) {
		return local, true, nil
	}
	if p, err := exec.LookPath("cmake"); err == nil {
		return p, false, nil
	}

	url, err := cmakeURL()
	if err != nil {
		return "", false, err
	}
	archive := filepath.Join(dir, "cmake-dl.tar.gz")
	defer os.Remove(archive)
	if err := downloadWithProgress(ctx, url, archive, 0, func(done, total int64) {
		if total > 0 && progress != nil {
			progress(float64(done) / float64(total) * 100)
		}
	}); err != nil {
		return "", false, fmt.Errorf("скачивание cmake: %w", err)
	}
	target := filepath.Join(dir, "cmake")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", false, err
	}
	if err := extractTarGz(archive, target); err != nil {
		return "", false, fmt.Errorf("распаковка cmake: %w", err)
	}
	if !fileExists(local) {
		return "", false, errors.New("cmake не найден в распакованном архиве")
	}
	_ = os.Chmod(local, 0o755)
	return local, true, nil
}

func localCMakePath(dir string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(dir, "cmake", "CMake.app", "Contents", "bin", "cmake")
	}
	return filepath.Join(dir, "cmake", "bin", "cmake")
}

func cmakeURL() (string, error) {
	base := fmt.Sprintf("https://github.com/Kitware/CMake/releases/download/v%s/cmake-%s-", cmakeVersion, cmakeVersion)
	switch {
	case runtime.GOOS == "darwin":
		return base + "macos-universal.tar.gz", nil
	case runtime.GOOS == "linux" && runtime.GOARCH == "arm64":
		return base + "linux-aarch64.tar.gz", nil
	case runtime.GOOS == "linux":
		return base + "linux-x86_64.tar.gz", nil
	}
	return "", fmt.Errorf("нет сборки cmake для %s/%s", runtime.GOOS, runtime.GOARCH)
}

// runQuiet выполняет команду и возвращает её вывод (для сообщений об ошибках).
func runQuiet(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return tail(string(out)), err
}

// --- скачивание с докачкой и прогрессом ---

// stallTimeout — сколько ждём хоть одного байта, прежде чем считать загрузку зависшей.
// Молчащее соединение иначе висит вечно, и пользователь смотрит на замерший прогресс.
// Переменная, а не константа, чтобы тест не ждал полторы минуты.
var stallTimeout = 90 * time.Second

// downloadWithProgress качает url в dest, продолжая с места обрыва, если рядом
// остался .part-файл. Готовый файл появляется атомарно — переименованием.
func downloadWithProgress(ctx context.Context, url, dest string, expected int64, cb func(done, total int64)) error {
	part := dest + ".part"
	var have int64
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var lastByteAt atomic.Int64
	var stalled atomic.Bool
	lastByteAt.Store(time.Now().UnixNano())
	checkEvery := stallTimeout / 3
	if checkEvery > 10*time.Second {
		checkEvery = 10 * time.Second
	}
	go func() {
		ticker := time.NewTicker(checkEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				idle := time.Since(time.Unix(0, lastByteAt.Load()))
				if idle > stallTimeout {
					stalled.Store(true)
					cancel()
					return
				}
			}
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var f *os.File
	total := expected
	switch resp.StatusCode {
	case http.StatusOK:
		// Сервер докачку не поддержал — качаем заново с нуля.
		have = 0
		if f, err = os.Create(part); err != nil {
			return err
		}
		if resp.ContentLength > 0 {
			total = resp.ContentLength
		}
	case http.StatusPartialContent:
		if f, err = os.OpenFile(part, os.O_WRONLY|os.O_APPEND, 0o644); err != nil {
			return err
		}
		if resp.ContentLength > 0 {
			total = have + resp.ContentLength
		}
	case http.StatusRequestedRangeNotSatisfiable:
		// Недокачанный файл уже не соответствует источнику — начинаем сначала.
		_ = os.Remove(part)
		return downloadWithProgress(ctx, url, dest, expected, cb)
	default:
		return fmt.Errorf("HTTP %d при загрузке %s", resp.StatusCode, url)
	}

	done := have
	lastReport := time.Now()
	buf := make([]byte, 256*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			done += int64(n)
			lastByteAt.Store(time.Now().UnixNano())
			if cb != nil && time.Since(lastReport) > 200*time.Millisecond {
				lastReport = time.Now()
				cb(done, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			f.Close()
			// .part не удаляем: при следующей попытке докачаем с этого места.
			if stalled.Load() {
				return fmt.Errorf("загрузка встала: за %s не пришло ни байта, проверьте сеть (скачано %s — повтор продолжит с этого места)",
					stallTimeout, humanBytes(done))
			}
			return readErr
		}
		if ctx.Err() != nil {
			f.Close()
			return ctx.Err()
		}
	}
	if err := f.Close(); err != nil {
		return err
	}

	if total > 0 && done != total {
		return fmt.Errorf("файл скачан не полностью: %s из %s", humanBytes(done), humanBytes(total))
	}
	if cb != nil {
		cb(done, total)
	}
	return os.Rename(part, dest)
}

// --- распаковка ---

// extractTarGz распаковывает архив в dir, снимая один верхний каталог.
// Используем системный tar: он есть и в macOS, и в современных Windows, и в Linux.
func extractTarGz(archive, dir string) error {
	if _, err := exec.LookPath("tar"); err != nil {
		return errors.New("для распаковки нужна утилита tar")
	}
	cmd := exec.Command("tar", "-xzf", archive, "-C", dir, "--strip-components=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tar: %s", firstMeaningful(string(out), err))
	}
	return nil
}

// extractAllFromZip достаёт из архива все файлы, подходящие под match,
// и складывает их плоско в dir.
func extractAllFromZip(zipPath, dir string, match func(name string) bool) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	found := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !match(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		dest := filepath.Join(dir, filepath.Base(strings.ReplaceAll(f.Name, "\\", "/")))
		out, err := os.Create(dest)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
		_ = os.Chmod(dest, 0o755)
		found++
	}
	if found == 0 {
		return errors.New("в архиве не нашлось нужных файлов")
	}
	return nil
}
