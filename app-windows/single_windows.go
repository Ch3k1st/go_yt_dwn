//go:build windows

// Один экземпляр на пользователя: именованный мьютекс + lock-файл с HWND
// живого окна, чтобы второй запуск поднимал уже открытое окно.

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// Local\ — пространство имён сеанса: два пользователя на одной машине
// (или сеанс RDP рядом с локальным) друг другу не мешают.
const mutexName = `Local\video-downloader-app`

func lockPath() string { return filepath.Join(dataDir(), "app.lock") }

// acquireSingleInstance держит мьютекс всё время жизни процесса. Если
// экземпляр уже есть — поднимает его окно и возвращает ok=false: выходим тихо.
func acquireSingleInstance() (release func(), ok bool) {
	h, err := windows.CreateMutex(nil, false, utf16Ptr(mutexName))
	if err == windows.ERROR_ALREADY_EXISTS {
		if h != 0 {
			windows.CloseHandle(h)
		}
		activateExisting()
		return nil, false
	}
	if h == 0 {
		// Мьютекс не дался (экзотика вроде исчерпанных хэндлов) — работаем
		// без защиты, лучше запуститься вторым окном, чем не запуститься.
		log.Printf("мьютекс единственного экземпляра недоступен: %v", err)
		return func() {}, true
	}
	return func() {
		os.Remove(lockPath())
		windows.CloseHandle(h)
	}, true
}

func publishedWindow() (hwnd uintptr, pid uint32) {
	b, err := os.ReadFile(lockPath())
	if err != nil {
		return 0, 0
	}
	fmt.Sscanf(string(b), "%d %d", &pid, &hwnd)
	return hwnd, pid
}

// publishWindow сообщает будущим запускам, какое окно поднимать.
func publishWindow(hwnd uintptr) {
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		return
	}
	line := fmt.Sprintf("%d %d\n", os.Getpid(), hwnd)
	if err := os.WriteFile(lockPath(), []byte(line), 0o644); err != nil {
		log.Printf("не удалось записать %s: %v", lockPath(), err)
	}
}

// activateExisting поднимает окно первого экземпляра. HWND проверяется по pid:
// номер окна система переиспользует, а файл мог остаться от прошлого запуска.
//
// Ждём появления окна до 3 секунд: два быстрых двойных клика — обычное дело,
// а первый экземпляр к этому моменту мог ещё не создать окно.
func activateExisting() {
	deadline := time.Now().Add(3 * time.Second)
	for {
		hwnd, pid := publishedWindow()
		if hwnd != 0 && pid != 0 && isWindow(hwnd) && windowPID(hwnd) == pid {
			if isIconic(hwnd) {
				showWindow(hwnd, swRestore)
			}
			setForegroundWindow(hwnd)
			return
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	log.Print("второй запуск: окно первого экземпляра не найдено")
	messageBox(0, "Video Downloader уже запущен, но его окно не нашлось.\n\n"+
		"Если окна действительно нет, снимите процесс VideoDownloader.exe в диспетчере задач "+
		"и запустите программу снова.", appTitle, mbOK|mbIconWarning)
}
