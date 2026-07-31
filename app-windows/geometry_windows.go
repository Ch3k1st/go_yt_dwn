//go:build windows

// Геометрия окна: восстановление при старте и запоминание в
// %LOCALAPPDATA%\video-downloader\window.json.

package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

// geometry хранится в физических пикселях (процесс DPI-aware), поэтому окно
// возвращается того же видимого размера и на мониторе с другим масштабом.
type geometry struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	W         int  `json:"w"`
	H         int  `json:"h"`
	Maximized bool `json:"maximized"`
}

func geometryPath() string { return filepath.Join(dataDir(), "window.json") }

func loadGeometry() *geometry {
	b, err := os.ReadFile(geometryPath())
	if err != nil {
		return nil
	}
	var g geometry
	if err := json.Unmarshal(b, &g); err != nil || g.W <= 0 || g.H <= 0 {
		return nil
	}
	return &g
}

func saveGeometry(g geometry) error {
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(g)
	if err != nil {
		return err
	}
	return os.WriteFile(geometryPath(), b, 0o644)
}

// minSize — минимум окна в физических пикселях. Логический минимум 1100×700
// дополнительно урезается рабочей областью: на 1920×1080 при 150 % такое окно
// физически не влезает, и без урезания его нельзя было бы уменьшить.
func minSize(hwnd uintptr) (int, int) {
	dpi := dpiForWindow(hwnd)
	w, h := scaleDPI(minWidth, dpi), scaleDPI(minHeight, dpi)
	ww, wh := workArea()
	if ww > 0 && w > ww {
		w = ww
	}
	if wh > 0 && h > wh {
		h = wh
	}
	return w, h
}

func applyMinSize(w webview2.WebView, hwnd uintptr) {
	mw, mh := minSize(hwnd)
	w.SetSize(mw, mh, webview2.HintMin)
}

// applyGeometry ставит окно на сохранённое место либо, если сохранённого нет
// (или оно указывает на отключённый монитор), центрует размер по умолчанию.
func applyGeometry(hwnd uintptr, g *geometry) {
	minW, minH := minSize(hwnd)
	workW, workH := workArea()
	dpi := dpiForWindow(hwnd)

	w, h := scaleDPI(defWidth, dpi), scaleDPI(defHeight, dpi)
	if g != nil {
		w, h = g.W, g.H
	}
	if workW > 0 && w > workW {
		w = workW
	}
	if workH > 0 && h > workH {
		h = workH
	}
	if w < minW {
		w = minW
	}
	if h < minH {
		h = minH
	}

	if g != nil {
		r := rect{Left: int32(g.X), Top: int32(g.Y), Right: int32(g.X + w), Bottom: int32(g.Y + h)}
		if onScreen(r) {
			setWindowPlacement(hwnd, r, g.Maximized)
			return
		}
		log.Print("сохранённая позиция окна вне экранов — центруем")
	}
	centerResize(hwnd, w, h)
}

// centerResize меняет размер, сохраняя центр окна: библиотека уже поставила
// его в центр основного монитора.
func centerResize(hwnd uintptr, w, h int) {
	r, ok := getWindowRect(hwnd)
	if !ok {
		return
	}
	x := int(r.Left+r.Right)/2 - w/2
	y := int(r.Top+r.Bottom)/2 - h/2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	setWindowPos(hwnd, x, y, w, h)
}

func currentGeometry(hwnd uintptr) (geometry, bool) {
	pl, ok := getWindowPlacement(hwnd)
	if !ok {
		return geometry{}, false
	}
	r := pl.RcNormalPosition
	return geometry{
		X:         int(r.Left),
		Y:         int(r.Top),
		W:         int(r.Right - r.Left),
		H:         int(r.Bottom - r.Top),
		Maximized: pl.ShowCmd == swShowMaximized,
	}, true
}

// watchWindow раз в полсекунды снимает геометрию окна. Опрос вместо перехвата
// WM_SIZE/WM_CLOSE выбран сознательно: подменять оконную процедуру библиотеки
// рискованнее, чем на полсекунды отстать от реальности.
//
// Заодно ловится переезд на монитор с другим масштабом: минимальный размер
// окна задан в физических пикселях и должен пересчитываться.
func watchWindow(w webview2.WebView, hwnd uintptr, stop <-chan struct{}) {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()

	var last, saved geometry
	dpi := dpiForWindow(hwnd)

	flush := func() {
		if last.W <= 0 || last == saved {
			return
		}
		if err := saveGeometry(last); err != nil {
			log.Printf("не удалось сохранить геометрию окна: %v", err)
			return
		}
		saved = last
	}
	defer flush()

	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			if !isWindow(hwnd) {
				return
			}
			if d := dpiForWindow(hwnd); d != dpi {
				dpi = d
				w.Dispatch(func() { applyMinSize(w, hwnd) })
			}
			g, ok := currentGeometry(hwnd)
			if !ok {
				continue
			}
			if g == last {
				flush() // размер устоялся — не пишем файл на каждый кадр перетаскивания
			} else {
				last = g
			}
		}
	}
}
