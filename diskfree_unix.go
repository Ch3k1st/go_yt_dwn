//go:build !windows

package main

import "syscall"

// freeDiskSpace возвращает, сколько байт доступно на диске, где лежит path.
// Нужно, чтобы заранее отказаться от скачивания модели или распаковки звука,
// а не упасть на середине с невнятной ошибкой записи.
func freeDiskSpace(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
