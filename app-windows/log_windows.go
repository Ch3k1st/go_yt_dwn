//go:build windows

// Лог оболочки и сервера: у GUI-процесса нет консоли, поэтому всё пишется в
// %LOCALAPPDATA%\video-downloader\app.log — именно этот путь показывают диалоги
// ошибок, чтобы владельцу было что прислать.

package main

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const logLimit = 4 << 20 // 4 МБ: дальше файл начинается заново

var (
	logMu   sync.Mutex
	logFile *os.File
)

// dataDir — %LOCALAPPDATA%\video-downloader: лог, геометрия окна, lock-файл и
// профиль WebView2. Рядом с exe ничего не пишем: архив могут распаковать в
// папку без прав на запись.
func dataDir() string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "video-downloader")
}

func logPath() string { return filepath.Join(dataDir(), "app.log") }

func openLog() func() {
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		return func() {}
	}
	if st, err := os.Stat(logPath()); err == nil && st.Size() > logLimit {
		os.Remove(logPath())
	}
	f, err := os.OpenFile(logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return func() {}
	}

	logMu.Lock()
	logFile = f
	logMu.Unlock()

	log.SetOutput(f)
	log.SetFlags(log.LstdFlags)
	log.Printf("--- VideoDownloader.exe запуск %s ---", time.Now().Format(time.RFC3339))

	return func() {
		logMu.Lock()
		logFile = nil
		logMu.Unlock()
		log.SetOutput(os.Stderr)
		f.Close()
	}
}

// writeLog кладёт сырой вывод сервера в тот же файл, что и наши сообщения.
func writeLog(p []byte) {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		logFile.Write(p)
	}
}
