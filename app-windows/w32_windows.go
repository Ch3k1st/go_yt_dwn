//go:build windows

// Тонкие обёртки над user32 и comdlg32 — то, чего нет ни в x/sys/windows, ни в
// go-webview2: геометрия окна, DPI, иконка, диалоги, активация чужого окна,
// выбор файла. Всё через *W-функции (UTF-16), чтобы пути и тексты с кириллицей
// не ломались. runtime.KeepAlive после вызовов: адрес, ушедший в uintptr, для
// сборщика мусора уже не ссылка, а буфер должен дожить до конца вызова.

package main

import (
	"runtime"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	comdlg32 = windows.NewLazySystemDLL("comdlg32.dll")

	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	procGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	procGetWindowPlacement            = user32.NewProc("GetWindowPlacement")
	procSetWindowPlacement            = user32.NewProc("SetWindowPlacement")
	procGetWindowRect                 = user32.NewProc("GetWindowRect")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procShowWindow                    = user32.NewProc("ShowWindow")
	procIsIconic                      = user32.NewProc("IsIconic")
	procIsWindow                      = user32.NewProc("IsWindow")
	procSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	procGetWindowThreadProcessId      = user32.NewProc("GetWindowThreadProcessId")
	procSendMessageW                  = user32.NewProc("SendMessageW")
	procCreateIconFromResourceEx      = user32.NewProc("CreateIconFromResourceEx")
	procMessageBoxW                   = user32.NewProc("MessageBoxW")
	procMonitorFromRect               = user32.NewProc("MonitorFromRect")
	procSystemParametersInfoW         = user32.NewProc("SystemParametersInfoW")

	procGetOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
)

const (
	swShowNormal    = 1
	swShowMaximized = 3
	swRestore       = 9

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	wmSetIcon = 0x0080
	iconSmall = 0
	iconBig   = 1

	mbOK          = 0x0000
	mbRetryCancel = 0x0005
	mbIconError   = 0x0010
	mbIconWarning = 0x0030
	mbTopMost     = 0x00040000
	idRetry       = 4

	monitorDefaultToNull = 0x0000

	spiGetWorkArea = 0x0030

	// CREATE_NO_WINDOW: консольный сервер стартует без окна консоли.
	createNoWindow = 0x08000000

	// Размеры окна в коде — логические пиксели этого масштаба.
	baseDPI = 96

	ofnFileMustExist = 0x00001000
	ofnPathMustExist = 0x00000800
	ofnNoChangeDir   = 0x00000008
	ofnExplorer      = 0x00080000
)

type rect struct{ Left, Top, Right, Bottom int32 }

type point struct{ X, Y int32 }

type windowPlacement struct {
	Length           uint32
	Flags            uint32
	ShowCmd          uint32
	PtMinPosition    point
	PtMaxPosition    point
	RcNormalPosition rect
}

// utf16 не даёт свалиться на строке с внутренним NUL: тексты диалогов
// склеиваются из сообщений сервера, гарантий по содержимому нет.
func utf16Ptr(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		p, _ = windows.UTF16PtrFromString("?")
	}
	return p
}

// enablePerMonitorDPI зовётся до создания окна: без этого на экране со 150 %
// WebView2 показывает растянутую растровую картинку вместо чёткого интерфейса.
func enablePerMonitorDPI() {
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 == (HANDLE)-4
	if procSetProcessDpiAwarenessContext.Find() == nil {
		if r, _, _ := procSetProcessDpiAwarenessContext.Call(^uintptr(3)); r != 0 {
			return
		}
	}
	if procSetProcessDPIAware.Find() == nil {
		_, _, _ = procSetProcessDPIAware.Call()
	}
}

func dpiForWindow(hwnd uintptr) int {
	if procGetDpiForWindow.Find() == nil {
		if r, _, _ := procGetDpiForWindow.Call(hwnd); r > 0 {
			return int(r)
		}
	}
	return baseDPI
}

func scaleDPI(logical, dpi int) int { return logical * dpi / baseDPI }

// workArea — рабочая область основного монитора (экран минус панель задач).
// Нули означают «узнать не удалось», вызывающий тогда не ограничивает размер.
func workArea() (int, int) {
	var r rect
	ok, _, _ := procSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0)
	runtime.KeepAlive(&r)
	if ok == 0 || r.Right <= r.Left || r.Bottom <= r.Top {
		return 0, 0
	}
	return int(r.Right - r.Left), int(r.Bottom - r.Top)
}

func getWindowRect(hwnd uintptr) (rect, bool) {
	var r rect
	ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	runtime.KeepAlive(&r)
	return r, ok != 0
}

func getWindowPlacement(hwnd uintptr) (windowPlacement, bool) {
	var pl windowPlacement
	pl.Length = uint32(unsafe.Sizeof(pl))
	ok, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&pl)))
	runtime.KeepAlive(&pl)
	return pl, ok != 0
}

func setWindowPlacement(hwnd uintptr, normal rect, maximized bool) {
	pl := windowPlacement{RcNormalPosition: normal, ShowCmd: swShowNormal}
	pl.Length = uint32(unsafe.Sizeof(pl))
	if maximized {
		pl.ShowCmd = swShowMaximized
	}
	_, _, _ = procSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&pl)))
	runtime.KeepAlive(&pl)
}

func setWindowPos(hwnd uintptr, x, y, w, h int) {
	_, _, _ = procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		swpNoZOrder|swpNoActivate)
}

// onScreen отвечает, пересекает ли прямоугольник хоть один подключённый монитор
// (сохранённая геометрия могла остаться от внешнего экрана, которого уже нет).
func onScreen(r rect) bool {
	h, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), monitorDefaultToNull)
	runtime.KeepAlive(&r)
	return h != 0
}

func isWindow(hwnd uintptr) bool {
	r, _, _ := procIsWindow.Call(hwnd)
	return r != 0
}

func isIconic(hwnd uintptr) bool {
	r, _, _ := procIsIconic.Call(hwnd)
	return r != 0
}

func showWindow(hwnd uintptr, cmd int) {
	_, _, _ = procShowWindow.Call(hwnd, uintptr(cmd))
}

func setForegroundWindow(hwnd uintptr) {
	_, _, _ = procSetForegroundWindow.Call(hwnd)
}

func windowPID(hwnd uintptr) uint32 {
	var pid uint32
	_, _, _ = procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	runtime.KeepAlive(&pid)
	return pid
}

func sendMessage(hwnd uintptr, msg, wparam, lparam uintptr) {
	_, _, _ = procSendMessageW.Call(hwnd, msg, wparam, lparam)
}

// createIcon делает HICON из кадра .ico (DIB или PNG — Windows понимает оба
// начиная с Vista). HICON живёт до конца процесса и не освобождается: их два.
func createIcon(frame []byte, size int) uintptr {
	if len(frame) == 0 {
		return 0
	}
	h, _, _ := procCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&frame[0])), uintptr(len(frame)),
		1,          // fIcon: иконка, а не курсор
		0x00030000, // версия формата ресурса
		uintptr(size), uintptr(size), 0)
	runtime.KeepAlive(frame)
	return h
}

// messageBox — единственный способ поговорить с пользователем, пока окна нет
// или пока оно пустое: у GUI-процесса нет консоли, писать некуда.
func messageBox(hwnd uintptr, text, caption string, flags uint32) int {
	t, c := utf16Ptr(text), utf16Ptr(caption)
	r, _, _ := procMessageBoxW.Call(hwnd,
		uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), uintptr(flags|mbTopMost))
	runtime.KeepAlive(t)
	runtime.KeepAlive(c)
	return int(r)
}

// --- выбор файла ----------------------------------------------------------

// openFileName повторяет OPENFILENAMEW (comdlg32). Порядок и типы полей менять
// нельзя: структура передаётся в системную функцию как есть.
type openFileName struct {
	StructSize      uint32
	Owner           uintptr
	Instance        uintptr
	Filter          *uint16
	CustomFilter    *uint16
	MaxCustomFilter uint32
	FilterIndex     uint32
	File            *uint16
	MaxFile         uint32
	FileTitle       *uint16
	MaxFileTitle    uint32
	InitialDir      *uint16
	Title           *uint16
	Flags           uint32
	FileOffset      uint16
	FileExtension   uint16
	DefExt          *uint16
	CustData        uintptr
	FnHook          uintptr
	TemplateName    *uint16
	PvReserved      uintptr
	DwReserved      uint32
	FlagsEx         uint32
}

// filterList склеивает список фильтров: пары «подпись, маска», разделённые
// нулями, и два нуля в конце. UTF16PtrFromString тут не годится — она считает
// внутренний NUL ошибкой.
func filterList(pairs ...string) *uint16 {
	var buf []uint16
	for _, p := range pairs {
		buf = append(buf, utf16.Encode([]rune(p))...)
		buf = append(buf, 0)
	}
	buf = append(buf, 0)
	return &buf[0]
}

// pickMediaFile показывает системный диалог выбора файла и возвращает путь.
// Зовётся не из потока цикла сообщений: диалог модальный.
func pickMediaFile(initialDir string) (string, bool) {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	ofn := openFileName{
		Filter: filterList(
			"Видео и аудио", "*.mp4;*.mkv;*.webm;*.mov;*.avi;*.m4a;*.mp3;*.wav;*.flac;*.ogg;*.opus",
			"Все файлы", "*.*"),
		FilterIndex: 1,
		File:        &buf[0],
		MaxFile:     uint32(len(buf)),
		Title:       utf16Ptr("Выберите видео или аудио"),
		Flags:       ofnFileMustExist | ofnPathMustExist | ofnNoChangeDir | ofnExplorer,
	}
	if initialDir != "" {
		ofn.InitialDir = utf16Ptr(initialDir)
	}
	ofn.StructSize = uint32(unsafe.Sizeof(ofn))

	ok, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	runtime.KeepAlive(&ofn)
	runtime.KeepAlive(buf)
	if ok == 0 {
		return "", false // пользователь нажал «Отмена»
	}
	return windows.UTF16ToString(buf), true
}
