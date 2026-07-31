package main

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Пути к используемым бинарникам. По умолчанию — имена в PATH; после
// ensureDependencies() заменяются на реальные пути (PATH или скачанные в tools/).
var (
	ytDlpPath  = "yt-dlp"
	ffmpegPath = "ffmpeg"
)

// toolsDir — папка для скачанных зависимостей: рядом с исполняемым файлом (портативно),
// с откатом в пользовательский кеш, если рядом с exe писать нельзя.
func toolsDir() string {
	// Явное указание папки (переносные установки и тесты) важнее всего остального.
	if custom := os.Getenv("VDOWN_TOOLS_DIR"); custom != "" {
		if os.MkdirAll(custom, 0o755) == nil {
			return custom
		}
	}
	if exe, err := os.Executable(); err == nil {
		d := filepath.Join(filepath.Dir(exe), "tools")
		if os.MkdirAll(d, 0o755) == nil {
			return d
		}
	}
	cache, _ := os.UserCacheDir()
	d := filepath.Join(cache, "v-down", "tools")
	_ = os.MkdirAll(d, 0o755)
	return d
}

// ensureDependencies гарантирует наличие yt-dlp и ffmpeg: ищет в tools/, затем в PATH,
// иначе скачивает. При неудаче печатает инструкцию и завершает программу.
func ensureDependencies() {
	fmt.Printf("\n  %s▸ Проверка зависимостей%s\n", cCyan, cReset)
	dir := toolsDir()

	yt, err := ensureYtDlp(dir)
	if err != nil {
		fatalDep("yt-dlp", err)
	}
	ytDlpPath = yt

	ff, err := ensureFfmpeg(dir)
	if err != nil {
		fatalDep("ffmpeg", err)
	}
	ffmpegPath = ff

	// whisper — необязательная зависимость: без неё работает всё, кроме
	// транскрибации, поэтому не скачиваем её заранее и не падаем, если её нет.
	// Установка идёт по требованию из интерфейса (POST /api/whisper/install).
	if p := findWhisperBinary(dir); p != "" {
		setWhisperBinary(p)
		reportDep("whisper", whisperSource(p, dir))
	} else {
		fmt.Printf("    %s•%s %s%-8s%s  %s%s%s\n", cDim, cReset, cBold, "whisper", cReset,
			cDim, "не установлен — поставится по кнопке в интерфейсе", cReset)
	}
}

// whisperSource поясняет, откуда взят найденный whisper-cli.
func whisperSource(path, dir string) string {
	if strings.HasPrefix(path, dir) {
		return "из tools/"
	}
	return "из PATH"
}

func fatalDep(name string, err error) {
	fmt.Printf("    %s✗%s %s%-8s%s — не удалось получить: %v\n", cRed, cReset, cBold, name, cReset, err)
	fmt.Printf("\n  %sУстановите зависимость вручную и перезапустите программу.%s\n", cDim, cReset)
	switch runtime.GOOS {
	case "darwin":
		fmt.Printf("  %sНапример: brew install yt-dlp ffmpeg%s\n", cDim, cReset)
	case "windows":
		fmt.Printf("  %sНапример: winget install yt-dlp ffmpeg%s\n", cDim, cReset)
	default:
		fmt.Printf("  %sНапример: sudo apt install yt-dlp ffmpeg%s\n", cDim, cReset)
	}
	os.Exit(1)
}

func ensureYtDlp(dir string) (string, error) {
	name := "yt-dlp"
	if runtime.GOOS == "windows" {
		name = "yt-dlp.exe"
	}
	local := filepath.Join(dir, name)
	if fileExists(local) {
		reportDep("yt-dlp", "из tools/")
		return local, nil
	}
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		reportDep("yt-dlp", "из PATH")
		return p, nil
	}

	url := ytDlpURL()
	if url == "" {
		return "", fmt.Errorf("нет сборки для %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	fmt.Printf("    %s⏳%s %s%-8s%s — скачиваю...\n", cYellow, cReset, cBold, "yt-dlp", cReset)
	if err := downloadFile(url, local); err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(local, 0o755)
	}
	reportDep("yt-dlp", "скачано")
	return local, nil
}

func ytDlpURL() string {
	base := "https://github.com/yt-dlp/yt-dlp/releases/latest/download/"
	switch runtime.GOOS {
	case "windows":
		return base + "yt-dlp.exe"
	case "darwin":
		return base + "yt-dlp_macos"
	case "linux":
		return base + "yt-dlp_linux"
	}
	return ""
}

func ensureFfmpeg(dir string) (string, error) {
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name = "ffmpeg.exe"
	}
	local := filepath.Join(dir, name)
	if fileExists(local) {
		reportDep("ffmpeg", "из tools/")
		return local, nil
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		reportDep("ffmpeg", "из PATH")
		return p, nil
	}

	fmt.Printf("    %s⏳%s %s%-8s%s — скачиваю (~80 МБ, один раз)...\n", cYellow, cReset, cBold, "ffmpeg", cReset)
	if err := downloadFfmpeg(dir, local); err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(local, 0o755)
	}
	reportDep("ffmpeg", "скачано")
	return local, nil
}

// ffmpegDarwinArm64URL — нативная сборка ffmpeg для Apple Silicon (версия зафиксирована,
// чтобы обновление в источнике не ломало сборку у пользователей).
const ffmpegDarwinArm64URL = "https://github.com/eugeneware/ffmpeg-static/releases/download/b6.1.1/ffmpeg-darwin-arm64"

// downloadFfmpeg скачивает и распаковывает статичную сборку ffmpeg в local.
func downloadFfmpeg(dir, local string) error {
	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			// На Apple Silicon сборка evermeet (x86_64) без установленной Rosetta
			// просто не запускается: «bad CPU type in executable». Поэтому здесь
			// берём готовый нативный arm64-бинарник из релиза ffmpeg-static.
			return downloadFile(ffmpegDarwinArm64URL, local)
		}
		// evermeet.cx: zip с единственным файлом ffmpeg (только x86_64).
		tmp := filepath.Join(dir, "ffmpeg-dl.zip")
		if err := downloadFile("https://evermeet.cx/ffmpeg/getrelease/ffmpeg/zip", tmp); err != nil {
			return err
		}
		defer os.Remove(tmp)
		return extractFromZip(tmp, local, func(name string) bool {
			return filepath.Base(name) == "ffmpeg"
		})

	case "windows":
		tmp := filepath.Join(dir, "ffmpeg-dl.zip")
		url := "https://github.com/yt-dlp/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip"
		if err := downloadFile(url, tmp); err != nil {
			return err
		}
		defer os.Remove(tmp)
		return extractFromZip(tmp, local, func(name string) bool {
			return strings.HasSuffix(strings.ReplaceAll(name, "\\", "/"), "bin/ffmpeg.exe")
		})

	case "linux":
		arch := "linux64"
		if runtime.GOARCH == "arm64" {
			arch = "linuxarm64"
		}
		url := "https://github.com/yt-dlp/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-" + arch + "-gpl.tar.xz"
		tmp := filepath.Join(dir, "ffmpeg-dl.tar.xz")
		if err := downloadFile(url, tmp); err != nil {
			return err
		}
		defer os.Remove(tmp)
		return extractTarXz(tmp, dir, "bin/ffmpeg", local)
	}
	return fmt.Errorf("неподдерживаемая ОС: %s", runtime.GOOS)
}

// --- вспомогательные функции ---

func reportDep(name, src string) {
	fmt.Printf("    %s✓%s %s%-8s%s  %s%s%s\n", cGreen, cReset, cBold, name, cReset, cDim, src, cReset)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// downloadFile скачивает url в dest (через временный файл, затем атомарное переименование).
func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d при загрузке %s", resp.StatusCode, url)
	}

	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()
	return os.Rename(tmp, dest)
}

// extractFromZip достаёт из zip первый файл, удовлетворяющий match, и пишет его в dest.
func extractFromZip(zipPath, dest string, match func(name string) bool) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !match(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dest)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		return err
	}
	return fmt.Errorf("нужный файл не найден в архиве")
}

// extractTarXz распаковывает tar.xz через системный tar и переносит файл с суффиксом
// wantSuffix (например bin/ffmpeg) в dest.
func extractTarXz(archivePath, dir, wantSuffix, dest string) error {
	if _, err := exec.LookPath("tar"); err != nil {
		return fmt.Errorf("требуется утилита tar для распаковки")
	}
	cmd := exec.Command("tar", "-xf", archivePath, "-C", dir)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	var found string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, wantSuffix) {
			found = path
		}
		return nil
	})
	if found == "" {
		return fmt.Errorf("%s не найден в архиве", wantSuffix)
	}
	in, err := os.Open(found)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
