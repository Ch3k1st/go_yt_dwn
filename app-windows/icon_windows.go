//go:build windows

// Иконка окна (заголовок, Alt+Tab, панель задач). Иконка самого файла в
// Проводнике берётся из ресурса rsrc_windows_amd64.syso — на окно ресурс не
// влияет, потому что класс окна регистрирует библиотека go-webview2.

package main

import (
	_ "embed"
	"encoding/binary"
)

// icon.ico рисуется скриптом app-windows/make-ico.swift (цель make windows-icon)
// и лежит в репозитории: сборка не должна зависеть от Swift под рукой.
//
//go:embed icon.ico
var iconICO []byte

func setWindowIcon(hwnd uintptr) {
	dpi := dpiForWindow(hwnd)
	if h := loadIcon(scaleDPI(32, dpi)); h != 0 {
		sendMessage(hwnd, wmSetIcon, iconBig, h)
	}
	if h := loadIcon(scaleDPI(16, dpi)); h != 0 {
		sendMessage(hwnd, wmSetIcon, iconSmall, h)
	}
}

func loadIcon(size int) uintptr {
	frame, ok := pickFrame(iconICO, size)
	if !ok {
		return 0
	}
	return createIcon(frame, size)
}

// pickFrame достаёт из .ico кадр, ближайший сверху к нужному размеру (лучше
// уменьшить крупный, чем растянуть мелкий); если таких нет — самый крупный.
func pickFrame(ico []byte, want int) ([]byte, bool) {
	const dirEntry = 16
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[2:4]) != 1 {
		return nil, false
	}
	count := int(binary.LittleEndian.Uint16(ico[4:6]))
	if count == 0 || len(ico) < 6+dirEntry*count {
		return nil, false
	}

	var best []byte
	bestSize := 0
	for i := 0; i < count; i++ {
		e := ico[6+dirEntry*i:]
		side := int(e[0])
		if side == 0 {
			side = 256
		}
		size := int(binary.LittleEndian.Uint32(e[8:12]))
		off := int(binary.LittleEndian.Uint32(e[12:16]))
		if size <= 0 || off < 0 || off+size > len(ico) {
			continue
		}
		better := best == nil ||
			(bestSize < want && side > bestSize) || // текущий выбор мелковат
			(side >= want && side < bestSize) // нашёлся подходящий поменьше
		if better {
			best, bestSize = ico[off:off+size], side
		}
	}
	return best, best != nil
}
