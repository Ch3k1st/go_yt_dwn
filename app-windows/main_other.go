//go:build !windows

// Оболочка с окном существует только под Windows. Заглушка нужна, чтобы на
// маке `go build ./...` и `go vet ./...` проходили по всему дереву, а не
// спотыкались о пакет без файлов для этой платформы.

package main

import "fmt"

func main() {
	fmt.Println("VideoDownloader — оболочка для Windows; собрать: make windows-app")
}
