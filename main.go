package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	cReset  = "\033[0m"
	cBold   = "\033[1m"
	cDim    = "\033[2m"
	cRed    = "\033[31m"
	cGreen  = "\033[32m"
	cYellow = "\033[33m"
	cBlue   = "\033[34m"
	cPurple = "\033[35m"
	cCyan   = "\033[36m"
)

type browserInfo struct {
	id      string
	display string
	profile string
	file    string // if set, use --cookies <file> instead of --cookies-from-browser
}

var cookieSrc browserInfo

// version — версия сборки. Подставляется при релизе через -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cli := flag.Bool("cli", false, "запустить консольный интерфейс вместо веб-оболочки")
	addr := flag.String("addr", "127.0.0.1:8080", "адрес веб-сервера (host:port)")
	showVer := flag.Bool("version", false, "показать версию и выйти")
	flag.Parse()

	if *showVer {
		fmt.Printf("v-down %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return
	}
	if *cli {
		runCLI()
		return
	}
	runWeb(*addr)
}

func runCLI() {
	clearScreen()
	showLogo()
	ensureDependencies()
	askForUpdate()
	askForBrowser()

	reader := bufio.NewReader(os.Stdin)

	for {
		clearScreen()
		showLogo()
		cookieTag := "без cookies"
		if cookieSrc.id != "" {
			cookieTag = "cookies: " + cookieSrc.display
		}
		fmt.Printf("\n  %s┌─[ %sURL %s│ %s%s%s %s]%s\n",
			cCyan, cBold+cYellow, cReset+cCyan, cDim, cookieTag, cReset+cCyan, cCyan, cReset)
		fmt.Printf("  %s└─▶%s ", cCyan, cReset)

		url, _ := reader.ReadString('\n')
		url = strings.TrimSpace(url)

		if isExitCommand(url) {
			fmt.Printf("\n  %s%s◆ Завершение работы. Пока! ◆%s\n\n", cBold, cPurple, cReset)
			break
		}

		if url == "" {
			fmt.Printf("\n  %s⚠  Пустая ссылка, попробуйте ещё раз.%s\n", cYellow, cReset)
			pause()
			continue
		}

		downloadVideo(url)
	}
}

func isExitCommand(s string) bool {
	s = strings.ToLower(s)
	return s == "exit" || s == "quit" || s == "выход"
}

func askForUpdate() {
	fmt.Printf("\n  %s?%s Проверить обновление yt-dlp? %s[y/n]%s: ",
		cYellow, cReset, cDim, cReset)
	var answer string
	fmt.Scanln(&answer)
	answer = strings.ToLower(strings.TrimSpace(answer))

	if answer == "y" || answer == "д" {
		fmt.Printf("\n  %s⏳ Проверка обновлений...%s\n\n", cCyan, cReset)
		cmd := exec.Command(ytDlpPath, "-U")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err == nil {
			fmt.Printf("\n  %s✓ Готово.%s\n", cGreen, cReset)
		} else {
			fmt.Printf("\n  %s✗ Ошибка при обновлении: %v%s\n", cRed, err, cReset)
		}
	}
}

func detectBrowsers() []browserInfo {
	home, _ := os.UserHomeDir()
	local := os.Getenv("LOCALAPPDATA")
	appdata := os.Getenv("APPDATA")

	type entry struct {
		id, display string
		// install-check paths per OS — if any exists, browser is considered installed
		darwin, linux, windows string
		// optional profile-dir override (for browsers not supported by yt-dlp natively, like Yandex)
		darwinProfile, linuxProfile, windowsProfile string
	}
	candidates := []entry{
		{"chrome", "Google Chrome",
			"/Applications/Google Chrome.app",
			home + "/.config/google-chrome",
			local + `\Google\Chrome\User Data`,
			"", "", ""},
		{"firefox", "Firefox",
			"/Applications/Firefox.app",
			home + "/.mozilla/firefox",
			appdata + `\Mozilla\Firefox`,
			"", "", ""},
		{"safari", "Safari",
			"/Applications/Safari.app", "", "",
			"", "", ""},
		{"edge", "Microsoft Edge",
			"/Applications/Microsoft Edge.app",
			home + "/.config/microsoft-edge",
			local + `\Microsoft\Edge\User Data`,
			"", "", ""},
		{"brave", "Brave",
			"/Applications/Brave Browser.app",
			home + "/.config/BraveSoftware/Brave-Browser",
			local + `\BraveSoftware\Brave-Browser\User Data`,
			"", "", ""},
		{"opera", "Opera",
			"/Applications/Opera.app",
			home + "/.config/opera",
			appdata + `\Opera Software\Opera Stable`,
			"", "", ""},
		{"vivaldi", "Vivaldi",
			"/Applications/Vivaldi.app",
			home + "/.config/vivaldi",
			local + `\Vivaldi\User Data`,
			"", "", ""},
		{"chromium", "Chromium",
			"/Applications/Chromium.app",
			home + "/.config/chromium",
			local + `\Chromium\User Data`,
			"", "", ""},
		// Yandex Browser is Chromium-based; yt-dlp doesn't know it by name,
		// so we pass its profile path under the "chrome" reader.
		{"chrome", "Yandex Browser",
			home + "/Library/Application Support/Yandex/YandexBrowser",
			home + "/.config/yandex-browser",
			local + `\Yandex\YandexBrowser\User Data`,
			home + "/Library/Application Support/Yandex/YandexBrowser/Default",
			home + "/.config/yandex-browser/Default",
			local + `\Yandex\YandexBrowser\User Data\Default`},
	}

	pickInstall := func(c entry) string {
		switch runtime.GOOS {
		case "darwin":
			return c.darwin
		case "linux":
			return c.linux
		case "windows":
			return c.windows
		}
		return ""
	}
	pickProfile := func(c entry) string {
		switch runtime.GOOS {
		case "darwin":
			return c.darwinProfile
		case "linux":
			return c.linuxProfile
		case "windows":
			return c.windowsProfile
		}
		return ""
	}

	var found []browserInfo
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "cookies.txt")
		if _, err := os.Stat(candidate); err == nil {
			found = append(found, browserInfo{display: "cookies.txt (рядом с программой)", file: candidate})
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(cwd, "cookies.txt")
		if _, err := os.Stat(candidate); err == nil {
			already := len(found) > 0 && found[0].file == candidate
			if !already {
				found = append(found, browserInfo{display: "cookies.txt (текущая папка)", file: candidate})
			}
		}
	}
	for _, c := range candidates {
		p := pickInstall(c)
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			found = append(found, browserInfo{id: c.id, display: c.display, profile: pickProfile(c)})
		}
	}
	return found
}

func askForBrowser() {
	browsers := detectBrowsers()
	if len(browsers) == 0 {
		fmt.Printf("\n  %s▸ Браузеры для cookies не найдены — загрузка без cookies.%s\n", cDim, cReset)
		return
	}

	fmt.Printf("\n  %s▸ Cookies из браузера%s %s(обход ограничений YouTube)%s\n",
		cCyan, cReset, cDim, cReset)
	for i, b := range browsers {
		marker := ""
		if i == 0 {
			marker = " " + cDim + "(по умолчанию)" + cReset
		}
		fmt.Printf("    %s%d)%s %s%s%s%s\n", cBold, i+1, cReset, cBold, b.display, cReset, marker)
	}
	fmt.Printf("    %s0)%s без cookies\n", cBold, cReset)
	fmt.Printf("\n  %s?%s Выберите %s[1]%s: ", cYellow, cReset, cDim, cReset)

	var answer string
	fmt.Scanln(&answer)
	answer = strings.TrimSpace(answer)

	if answer == "0" {
		fmt.Printf("  %s✓ Без cookies.%s\n", cDim, cReset)
		return
	}

	idx := 0
	if answer != "" {
		var n int
		if _, err := fmt.Sscanf(answer, "%d", &n); err == nil && n >= 1 && n <= len(browsers) {
			idx = n - 1
		}
	}
	cookieSrc = browsers[idx]
	fmt.Printf("  %s✓ Cookies будут взяты из %s%s%s.%s\n",
		cGreen, cBold, cookieSrc.display, cReset, cReset)
}

func downloadVideo(url string) {
	outputDir := "downloads"
	_ = os.MkdirAll(outputDir, os.ModePerm)
	outputPath := outputDir + "/%(title)s.%(ext)s"

	printDivider("ЗАГРУЗКА")

	progressTpl := "download:" + cCyan + "▸" + cReset +
		" " + cBold + "%(progress._percent_str)s" + cReset +
		"  │  скорость: " + cYellow + "%(progress._speed_str)s" + cReset +
		"  │  ETA: " + cPurple + "%(progress._eta_str)s" + cReset

	args := []string{
		"-f", "bv*[vcodec^=avc]+ba[ext=m4a]/best[ext=mp4]/best",
		"--merge-output-format", "mp4",
		"--no-playlist",
		"--no-warnings",
		"--progress",
		"--newline",
		"--progress-template", progressTpl,
		"--downloader", "ffmpeg",
		"--external-downloader-args", "ffmpeg:-loglevel info",
		"--ffmpeg-location", ffmpegPath,
		// Обход "Sign in to confirm you're not a bot": используем TV/Safari-клиенты,
		// которым не нужен PO-токен. Если YouTube в очередной раз всё закроет —
		// останется fallback на cookies из меню.
		"--extractor-args", "youtube:player_client=tv,web_safari,default",
		"--retries", "3",
		"--sleep-requests", "1",
		"-o", outputPath,
	}
	args = append(args, cookieArgs(cookieSrc)...)
	args = append(args, url)

	cmd := exec.Command(ytDlpPath, args...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	printDivider("")

	if err != nil {
		fmt.Printf("\n  %s%s✗ Ошибка:%s %v\n", cBold, cRed, cReset, err)
	} else {
		fmt.Printf("\n  %s%s✓ Готово!%s Файл сохранён в %s./downloads/%s\n",
			cBold, cGreen, cReset, cCyan, cReset)
	}

	pause()
}

func printDivider(label string) {
	width := 60
	if label == "" {
		fmt.Printf("\n  %s%s%s\n", cPurple, strings.Repeat("─", width), cReset)
		return
	}
	pad := width - len([]rune(label)) - 4
	if pad < 2 {
		pad = 2
	}
	left := pad / 2
	right := pad - left
	fmt.Printf("\n  %s%s%s %s%s%s %s%s%s\n",
		cPurple, strings.Repeat("─", left), cReset,
		cBold+cYellow, label, cReset,
		cPurple, strings.Repeat("─", right), cReset)
}

func pause() {
	fmt.Printf("\n  %sНажмите Enter, чтобы продолжить...%s", cDim, cReset)
	bufio.NewScanner(os.Stdin).Scan()
}

func clearScreen() {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "cls")
	} else {
		cmd = exec.Command("clear")
	}
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func showLogo() {
	pixel := []string{
		"        ░░░░░░░░░░░░░░░░░░░░░░░░░░░░",
		"        ░░██████████████████████░░░░",
		"        ░░██░░░░░░░░░░░░░░░░░░██░░░░",
		"        ░░██░░░░██░░░░░░░░░░░░██░░░░",
		"        ░░██░░░░████░░░░░░░░░░██░░░░",
		"        ░░██░░░░██████░░░░░░░░██░░░░",
		"        ░░██░░░░████████░░░░░░██░░░░",
		"        ░░██░░░░██████░░░░░░░░██░░░░",
		"        ░░██░░░░████░░░░░░░░░░██░░░░",
		"        ░░██░░░░██░░░░░░░░░░░░██░░░░",
		"        ░░██░░░░░░░░░░░░░░░░░░██░░░░",
		"        ░░██████████████████████░░░░",
		"        ░░░░░░░░░░░░░░░░░░░░░░░░░░░░",
	}
	title := []string{
		"   ██╗   ██╗██╗██████╗ ███████╗ ██████╗ ",
		"   ██║   ██║██║██╔══██╗██╔════╝██╔═══██╗",
		"   ██║   ██║██║██║  ██║█████╗  ██║   ██║",
		"   ╚██╗ ██╔╝██║██║  ██║██╔══╝  ██║   ██║",
		"    ╚████╔╝ ██║██████╔╝███████╗╚██████╔╝",
		"     ╚═══╝  ╚═╝╚═════╝ ╚══════╝ ╚═════╝ ",
	}

	fmt.Println()
	for _, line := range pixel {
		fmt.Printf("%s%s%s%s\n", cBold, cRed, line, cReset)
	}
	fmt.Println()
	for _, line := range title {
		fmt.Printf("%s%s%s%s\n", cBold, cCyan, line, cReset)
	}
	fmt.Printf("\n         %s%s▶ D O W N L O A D E R ◀%s   %sby Ch3kL1st%s\n",
		cBold, cYellow, cReset, cDim, cReset)
}
