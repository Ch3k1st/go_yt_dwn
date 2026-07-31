package main

// Состояние зависимостей и установка по кнопке из интерфейса.
//
// Логика скачивания живёт в deps.go; здесь только неблокирующая обёртка вокруг неё:
// ensureDependencies() при неудаче завершает программу — для консоли это правильно,
// а веб-оболочка должна остаться живой и показать баннер «зависимости не установлены»
// с кнопкой установки.

import (
	"fmt"
	"net/http"
	"os/exec"
	"sync"
)

// depsInstaller — состояние фоновой установки. Установка идёт по одной за раз.
type depsInstaller struct {
	mu      sync.Mutex
	running bool
	stage   string
	err     string
	done    bool
}

var depsJob depsInstaller

// missingDeps возвращает список зависимостей, которых сейчас нет.
func missingDeps() []string {
	var missing []string
	if !toolAvailable(ytDlpPath, "yt-dlp") {
		missing = append(missing, "yt-dlp")
	}
	if !toolAvailable(ffmpegPath, "ffmpeg") {
		missing = append(missing, "ffmpeg")
	}
	return missing
}

// toolAvailable: путь либо указывает на существующий файл, либо это имя,
// которое находится в PATH.
func toolAvailable(path, name string) bool {
	if path != "" && path != name && fileExists(path) {
		return true
	}
	_, err := exec.LookPath(path)
	return err == nil
}

// ensureDependenciesWeb — вариант ensureDependencies() для веб-режима: сообщает
// о проблеме и продолжает работу, вместо os.Exit(1).
func ensureDependenciesWeb() {
	fmt.Printf("\n  %s▸ Проверка зависимостей%s\n", cCyan, cReset)
	dir := toolsDir()

	if p, err := ensureYtDlp(dir); err == nil {
		ytDlpPath = p
	} else {
		fmt.Printf("    %s✗%s %s%-8s%s — %v\n", cRed, cReset, cBold, "yt-dlp", cReset, err)
	}
	if p, err := ensureFfmpeg(dir); err == nil {
		ffmpegPath = p
	} else {
		fmt.Printf("    %s✗%s %s%-8s%s — %v\n", cRed, cReset, cBold, "ffmpeg", cReset, err)
	}
	if p := findWhisperBinary(dir); p != "" {
		setWhisperBinary(p)
		reportDep("whisper", whisperSource(p, dir))
	} else {
		fmt.Printf("    %s•%s %s%-8s%s  %s%s%s\n", cDim, cReset, cBold, "whisper", cReset,
			cDim, "не установлен — поставится по кнопке в интерфейсе", cReset)
	}
	if m := missingDeps(); len(m) > 0 {
		fmt.Printf("\n  %s⚠ Нет зависимостей: %v — поставьте их кнопкой в интерфейсе.%s\n", cYellow, m, cReset)
	}
}

// install скачивает недостающее в фоне. Возвращает false, если установка уже идёт.
func (d *depsInstaller) install() bool {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return false
	}
	d.running, d.done, d.err, d.stage = true, false, "", "Подготовка"
	d.mu.Unlock()

	go func() {
		dir := toolsDir()
		var failure string
		if !toolAvailable(ytDlpPath, "yt-dlp") {
			d.setStage("Скачиваю yt-dlp")
			if p, err := ensureYtDlp(dir); err == nil {
				ytDlpPath = p
			} else {
				failure = "yt-dlp: " + err.Error()
			}
		}
		if failure == "" && !toolAvailable(ffmpegPath, "ffmpeg") {
			d.setStage("Скачиваю ffmpeg (~80 МБ, один раз)")
			if p, err := ensureFfmpeg(dir); err == nil {
				ffmpegPath = p
			} else {
				failure = "ffmpeg: " + err.Error()
			}
		}
		d.mu.Lock()
		d.running, d.done, d.err, d.stage = false, true, failure, "Готово"
		if failure != "" {
			d.stage = "Ошибка"
		}
		d.mu.Unlock()
	}()
	return true
}

func (d *depsInstaller) setStage(s string) {
	d.mu.Lock()
	d.stage = s
	d.mu.Unlock()
}

func (d *depsInstaller) snapshot() (running bool, stage, errMsg string, done bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.running, d.stage, d.err, d.done
}

// handleDeps — GET /api/deps: чего не хватает прямо сейчас.
func handleDeps(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	missing := missingDeps()
	running, stage, errMsg, _ := depsJob.snapshot()
	if missing == nil {
		missing = []string{}
	}
	writeJSON(w, map[string]any{
		"ok":         len(missing) == 0,
		"missing":    missing,
		"installing": running,
		"stage":      stage,
		"error":      errMsg,
	})
}

// handleDepsInstall — POST /api/deps/install: запускает докачивание недостающего.
func handleDepsInstall(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "Нужен POST")
		return
	}
	if !depsJob.install() {
		writeErr(w, http.StatusConflict, "Установка уже идёт")
		return
	}
	writeJSON(w, map[string]bool{"ok": true, "started": true})
}

// handleDepsProgress — GET /api/deps/progress: ход установки для баннера.
func handleDepsProgress(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	running, stage, errMsg, done := depsJob.snapshot()
	state := "idle"
	switch {
	case running:
		state = "installing"
	case errMsg != "":
		state = "error"
	case done:
		state = "done"
	}
	missing := missingDeps()
	if missing == nil {
		missing = []string{}
	}
	writeJSON(w, map[string]any{
		"state":   state,
		"stage":   stage,
		"error":   errMsg,
		"ok":      len(missing) == 0,
		"missing": missing,
	})
}
