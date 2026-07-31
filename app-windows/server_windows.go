//go:build windows

// Дочерний процесс сервера: старт без консольного окна, чтение вывода в лог,
// парсинг адреса, ожидание готовности, гарантия отсутствия сирот (job object).

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	serverExe = "v-down.exe"
	// Первый запуск качает yt-dlp и FFmpeg (~80 МБ), поэтому ждём адрес долго,
	// но только пока сервер что-то печатает (см. outputTap.idle).
	startTimeout = 20 * time.Second
	idleTimeout  = 45 * time.Second
)

// Формат строки сервера: «Откройте в браузере: http://127.0.0.1:54321»
// (web.go). Строка цветная (ANSI-коды), поэтому ищем сам адрес, а не подпись.
var addrRe = regexp.MustCompile(`http://[0-9A-Za-z.:\[\]]+`)

type serverProc struct {
	cmd      *exec.Cmd
	base     string
	exited   chan struct{}
	exitErr  error
	stopping atomic.Bool
}

// serverWorkDir — рабочая папка сервера. Внутрь неё он кладёт downloads/;
// рядом с exe не пишем, программу могут распаковать в папку без прав.
func serverWorkDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return os.TempDir()
	}
	dir := filepath.Join(home, "Downloads", "Video Downloader")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("не удалось создать %s: %v", dir, err)
		return home
	}
	return dir
}

func downloadsDir() string { return filepath.Join(serverWorkDir(), "downloads") }

// startServer поднимает соседний v-down.exe и возвращается только когда тот
// отвечает на /api/browsers. onExit зовётся позже и лишь если сервер умер сам.
func startServer(onExit func()) (*serverProc, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("не удалось определить путь к оболочке: %w", err)
	}
	dir := filepath.Dir(self)
	exe := filepath.Join(dir, serverExe)
	if _, err := os.Stat(exe); err != nil {
		return nil, fmt.Errorf("рядом с оболочкой нет %s — распакуйте из архива оба файла", serverExe)
	}

	// -no-open: интерфейс уже показан в этом окне, вкладка браузера лишняя.
	// -addr с нулевым портом: систему просим выдать любой свободный.
	// Движок кладётся в архив вместе с оболочкой, так что флаг он понимает.
	return launch(exe, dir, []string{"-no-open", "-addr", "127.0.0.1:0"}, onExit)
}

func launch(exe, dir string, args []string, onExit func()) (*serverProc, error) {
	addr := make(chan string, 1)
	out := &outputTap{addr: addr, last: time.Now()}

	cmd := exec.Command(exe, args...)
	cmd.Dir = serverWorkDir()
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("не удалось запустить %s: %w", serverExe, err)
	}
	adoptChild(cmd.Process.Pid)
	log.Printf("движок запущен, pid %d, папка %s", cmd.Process.Pid, cmd.Dir)

	s := &serverProc{cmd: cmd, exited: make(chan struct{})}
	go func() {
		s.exitErr = cmd.Wait()
		close(s.exited)
	}()

	if err := s.waitAddr(addr, out); err != nil {
		s.stop()
		return nil, err
	}
	if err := s.waitHealthy(); err != nil {
		s.stop()
		return nil, err
	}

	go func() {
		<-s.exited
		if !s.stopping.Load() {
			log.Printf("движок завершился сам: %v", s.exitErr)
			if onExit != nil {
				onExit()
			}
		}
	}()
	return s, nil
}

// waitAddr ждёт строку с адресом. Таймаут отсчитывается от последней строки
// вывода: пока сервер качает зависимости, он жив и разговаривает.
func (s *serverProc) waitAddr(addr <-chan string, out *outputTap) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case s.base = <-addr:
			return nil
		case <-s.exited:
			return fmt.Errorf("движок завершился при старте (%v)", s.exitErr)
		case <-tick.C:
			idle := time.Since(out.idle())
			if idle > idleTimeout || (out.silent() && idle > startTimeout) {
				return fmt.Errorf("движок не сообщил адрес (молчит %s)", idle.Round(time.Second))
			}
		}
	}
}

// waitHealthy ждёт первого 200 от /api/browsers: адрес в выводе появляется
// раньше, чем сервер реально начинает отвечать.
func (s *serverProc) waitHealthy() error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(startTimeout)
	for {
		resp, err := client.Get(s.base + "/api/browsers")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-s.exited:
			return fmt.Errorf("движок завершился при старте (%v)", s.exitErr)
		default:
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("движок не ответил за %s", startTimeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// stop убивает сервер: штатного «мягкого» сигнала на Windows нет, а состояние
// движок в памяти не держит — терять при выходе нечего. Дети (yt-dlp, ffmpeg)
// уходят вместе с ним через job object.
func (s *serverProc) stop() {
	if s == nil || s.cmd.Process == nil {
		return
	}
	s.stopping.Store(true)
	if err := s.cmd.Process.Kill(); err != nil {
		log.Printf("не удалось остановить движок: %v", err)
	}
	select {
	case <-s.exited:
	case <-time.After(3 * time.Second):
		log.Print("движок не завершился за 3 с — доверяем job object")
	}
}

// outputTap: весь вывод сервера уходит в лог, а до первой находки ещё и
// просеивается на адрес. Читать вывод обязательно — иначе сервер встанет,
// когда буфер пайпа заполнится.
type outputTap struct {
	mu    sync.Mutex
	buf   []byte
	found bool
	last  time.Time
	seen  bool
	addr  chan<- string
}

func (t *outputTap) Write(p []byte) (int, error) {
	writeLog(p)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.last = time.Now()
	t.seen = true
	if t.found {
		return len(p), nil
	}
	t.buf = append(t.buf, p...)
	if len(t.buf) > 64<<10 { // адрес печатается в первых строках
		t.buf = t.buf[len(t.buf)-64<<10:]
	}
	if m := addrRe.Find(t.buf); m != nil {
		t.found = true
		t.buf = nil
		t.addr <- string(m)
	}
	return len(p), nil
}

// idle — время последней строки вывода, silent — не было ни одной.
func (t *outputTap) idle() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.last
}

func (t *outputTap) silent() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.seen
}

// --- job object: страховка от сирот ---------------------------------------

var (
	jobHandle windows.Handle
	jobHasAll bool // в job лежим мы сами, значит дети попадают туда по наследству
)

// setupJob создаёт job с KILL_ON_JOB_CLOSE и кладёт в него текущий процесс.
// Дети (сервер) и внуки (yt-dlp/ffmpeg) наследуют членство при создании —
// гонки «стартовал, но ещё не в job» не возникает. Когда процесс оболочки
// исчезает по любой причине, последний хэндл job закрывается и ядро гасит
// всё, что осталось: в диспетчере задач сирот не будет.
func setupJob() {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		log.Printf("job object недоступен: %v", err)
		return
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		log.Printf("не удалось настроить job object: %v", err)
		windows.CloseHandle(h)
		return
	}
	// Хэндл не закрываем никогда: его закрытие при выходе процесса и есть
	// механизм уборки.
	jobHandle = h
	if err := windows.AssignProcessToJobObject(h, windows.CurrentProcess()); err != nil {
		log.Printf("себя в job положить не вышло (%v) — назначаем детей поштучно", err)
		return
	}
	jobHasAll = true
}

// adoptChild — фолбэк для систем, где self-assign не прошёл: тогда в job
// кладём сам дочерний процесс (внуки всё равно наследуют членство от него).
func adoptChild(pid int) {
	if jobHandle == 0 || jobHasAll {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		log.Printf("не удалось открыть дочерний процесс %d: %v", pid, err)
		return
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(jobHandle, h); err != nil {
		log.Printf("не удалось положить движок в job: %v", err)
	}
}
