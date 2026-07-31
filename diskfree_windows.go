//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// freeDiskSpace возвращает, сколько байт доступно на диске, где лежит path.
// Windows-версия: GetDiskFreeSpaceExW из kernel32, без сторонних зависимостей.
func freeDiskSpace(path string) (int64, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var freeForCaller, total, totalFree uint64
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDiskFreeSpaceExW")
	r, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeForCaller)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return 0, callErr
	}
	return int64(freeForCaller), nil
}
