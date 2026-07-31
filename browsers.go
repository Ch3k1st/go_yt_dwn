package main

// Поиск установленных браузеров для установки расширения.
//
// Это не то же самое, что detectBrowsers() в main.go: там ищутся источники cookies
// для yt-dlp (важен профиль), здесь — куда можно поставить расширение (важны
// исполняемый файл и адрес страницы расширений).

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// installedBrowser — браузер, найденный на машине.
type installedBrowser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Path — то, что передаётся системе для запуска: .app на macOS, .exe на Windows.
	Path string `json:"path"`
	// Engine — chromium или firefox: от него зависит способ установки.
	Engine string `json:"engine"`
	// ExtPage — внутренний адрес страницы расширений.
	ExtPage string `json:"extPage"`
	// Temporary — расширение живёт только до перезапуска браузера (Firefox).
	Temporary bool `json:"temporary"`
}

// browserSpec — описание известного браузера с путями по ОС.
type browserSpec struct {
	id, name, engine, extPage string
	temporary                 bool
	darwinApp                 string   // имя .app в /Applications и ~/Applications
	windowsRel                []string // пути относительно корней Program Files / LocalAppData
	linuxBins                 []string // имена исполняемых файлов в PATH
}

// knownBrowsers — таблица браузеров, которые умеют ставить распакованные расширения.
// Chromium-совместимые идут первыми: у них установка в один шаг.
var knownBrowsers = []browserSpec{
	{id: "chrome", name: "Google Chrome", engine: "chromium", extPage: "chrome://extensions",
		darwinApp:  "Google Chrome.app",
		windowsRel: []string{`Google\Chrome\Application\chrome.exe`},
		linuxBins:  []string{"google-chrome", "google-chrome-stable"}},
	// browser://tune — это «Дополнения» Яндекса: там нет ни режима разработчика,
	// ни кнопки «Загрузить распакованное расширение». Нужен Chromium-менеджер.
	{id: "yandex", name: "Яндекс.Браузер", engine: "chromium", extPage: "browser://extensions",
		darwinApp:  "Yandex.app",
		windowsRel: []string{`Yandex\YandexBrowser\Application\browser.exe`},
		linuxBins:  []string{"yandex-browser"}},
	{id: "edge", name: "Microsoft Edge", engine: "chromium", extPage: "edge://extensions",
		darwinApp:  "Microsoft Edge.app",
		windowsRel: []string{`Microsoft\Edge\Application\msedge.exe`},
		linuxBins:  []string{"microsoft-edge"}},
	{id: "brave", name: "Brave", engine: "chromium", extPage: "brave://extensions",
		darwinApp:  "Brave Browser.app",
		windowsRel: []string{`BraveSoftware\Brave-Browser\Application\brave.exe`},
		linuxBins:  []string{"brave-browser", "brave"}},
	{id: "opera", name: "Opera", engine: "chromium", extPage: "opera://extensions",
		darwinApp:  "Opera.app",
		windowsRel: []string{`Programs\Opera\opera.exe`, `Opera\opera.exe`},
		linuxBins:  []string{"opera"}},
	{id: "vivaldi", name: "Vivaldi", engine: "chromium", extPage: "vivaldi://extensions",
		darwinApp:  "Vivaldi.app",
		windowsRel: []string{`Vivaldi\Application\vivaldi.exe`},
		linuxBins:  []string{"vivaldi", "vivaldi-stable"}},
	{id: "arc", name: "Arc", engine: "chromium", extPage: "arc://extensions",
		darwinApp: "Arc.app"},
	{id: "chromium", name: "Chromium", engine: "chromium", extPage: "chrome://extensions",
		darwinApp:  "Chromium.app",
		windowsRel: []string{`Chromium\Application\chrome.exe`},
		linuxBins:  []string{"chromium", "chromium-browser"}},
	{id: "firefox", name: "Firefox", engine: "firefox", extPage: "about:debugging#/runtime/this-firefox",
		temporary:  true,
		darwinApp:  "Firefox.app",
		windowsRel: []string{`Mozilla Firefox\firefox.exe`},
		linuxBins:  []string{"firefox"}},
}

// detectInstalledBrowsers возвращает браузеры, найденные на машине.
func detectInstalledBrowsers() []installedBrowser {
	var found []installedBrowser
	seen := map[string]bool{}
	add := func(b installedBrowser) {
		key := strings.ToLower(b.Path)
		if b.Path == "" || seen[key] {
			return
		}
		seen[key] = true
		found = append(found, b)
	}

	switch runtime.GOOS {
	case "darwin":
		home, _ := os.UserHomeDir()
		roots := []string{"/Applications", filepath.Join(home, "Applications")}
		for _, spec := range knownBrowsers {
			if spec.darwinApp == "" {
				continue
			}
			for _, root := range roots {
				p := filepath.Join(root, spec.darwinApp)
				if _, err := os.Stat(p); err == nil {
					add(browserFrom(spec, p))
					break
				}
			}
		}
	case "windows":
		for _, spec := range knownBrowsers {
			if p := windowsBrowserPath(spec); p != "" {
				add(browserFrom(spec, p))
			}
		}
		// Реестр добирает то, чего нет в таблице (сборки Chromium, порталы вендоров).
		for _, b := range windowsRegistryBrowsers() {
			add(b)
		}
	default:
		for _, spec := range knownBrowsers {
			for _, bin := range spec.linuxBins {
				if p, err := exec.LookPath(bin); err == nil {
					add(browserFrom(spec, p))
					break
				}
			}
		}
	}

	// Chromium-браузеры выше: у них установка проще и постоянная.
	sort.SliceStable(found, func(i, j int) bool {
		return found[i].Engine == "chromium" && found[j].Engine != "chromium"
	})
	return found
}

func browserFrom(spec browserSpec, path string) installedBrowser {
	return installedBrowser{
		ID: spec.id, Name: spec.name, Path: path,
		Engine: spec.engine, ExtPage: spec.extPage, Temporary: spec.temporary,
	}
}

// windowsBrowserPath проверяет типовые места установки в Program Files и LocalAppData.
func windowsBrowserPath(spec browserSpec) string {
	roots := []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		os.Getenv("LOCALAPPDATA"),
	}
	for _, rel := range spec.windowsRel {
		for _, root := range roots {
			if root == "" {
				continue
			}
			p := filepath.Join(root, rel)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// windowsRegistryBrowsers читает список браузеров из StartMenuInternet.
// Работает через reg.exe: тянуть зависимость ради реестра не хочется, а на других
// ОС функция просто ничего не возвращает (кросс-сборка не ломается).
func windowsRegistryBrowsers() []installedBrowser {
	if runtime.GOOS != "windows" {
		return nil
	}
	var out []installedBrowser
	for _, hive := range []string{`HKLM\SOFTWARE\Clients\StartMenuInternet`, `HKCU\SOFTWARE\Clients\StartMenuInternet`} {
		listing, err := exec.Command("reg", "query", hive).Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(listing), "\n") {
			key := strings.TrimSpace(line)
			if !strings.HasPrefix(strings.ToUpper(key), "HK") {
				continue
			}
			cmdOut, err := exec.Command("reg", "query", key+`\shell\open\command`, "/ve").Output()
			if err != nil {
				continue
			}
			exe := parseRegDefaultValue(string(cmdOut))
			if exe == "" {
				continue
			}
			if _, err := os.Stat(exe); err != nil {
				continue
			}
			out = append(out, browserFromExe(key[strings.LastIndex(key, `\`)+1:], exe))
		}
	}
	return out
}

// parseRegDefaultValue достаёт путь из вывода reg query ... /ve.
func parseRegDefaultValue(s string) string {
	for _, line := range strings.Split(s, "\n") {
		idx := strings.Index(line, "REG_SZ")
		if idx < 0 {
			continue
		}
		v := strings.TrimSpace(line[idx+len("REG_SZ"):])
		v = strings.Trim(v, `"`)
		// В команде запуска бывают аргументы после пути.
		if i := strings.Index(strings.ToLower(v), ".exe"); i > 0 {
			v = v[:i+4]
		}
		return strings.TrimSpace(v)
	}
	return ""
}

// browserFromExe угадывает движок и страницу расширений по имени ключа реестра.
func browserFromExe(keyName, exe string) installedBrowser {
	low := strings.ToLower(keyName + " " + filepath.Base(exe))
	for _, spec := range knownBrowsers {
		if strings.Contains(low, spec.id) {
			return browserFrom(spec, exe)
		}
	}
	if strings.Contains(low, "firefox") || strings.Contains(low, "mozilla") {
		return installedBrowser{ID: "firefox", Name: keyName, Path: exe, Engine: "firefox",
			ExtPage: "about:debugging#/runtime/this-firefox", Temporary: true}
	}
	// Всё остальное со StartMenuInternet — почти всегда сборка Chromium.
	return installedBrowser{ID: "chromium", Name: keyName, Path: exe, Engine: "chromium",
		ExtPage: "chrome://extensions"}
}

// findBrowser ищет браузер по id среди найденных.
func findBrowser(id string) (installedBrowser, bool) {
	for _, b := range detectInstalledBrowsers() {
		if b.ID == id {
			return b, true
		}
	}
	return installedBrowser{}, false
}

// launchBrowser открывает браузер на заданном адресе.
// Возвращает ошибку запуска; «браузер проигнорировал адрес» отсюда не видно —
// поэтому вызывающая сторона всегда показывает адрес и текстом.
func launchBrowser(b installedBrowser, url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		if url == "" {
			cmd = exec.Command("open", "-a", b.Path)
		} else {
			cmd = exec.Command("open", "-a", b.Path, url)
		}
	case "windows":
		if url == "" {
			cmd = exec.Command(b.Path)
		} else {
			cmd = exec.Command(b.Path, url)
		}
	default:
		if url == "" {
			cmd = exec.Command(b.Path)
		} else {
			cmd = exec.Command(b.Path, url)
		}
	}
	return cmd.Start()
}

// openFolder показывает папку в файловом менеджере.
func openFolder(dir string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	return cmd.Start()
}
