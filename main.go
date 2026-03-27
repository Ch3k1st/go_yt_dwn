package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	checkDependencies()
	showLogo()
	askForUpdate()

	reader := bufio.NewReader(os.Stdin)

	for {
		clearScreen()
		fmt.Print("\nВведите ссылку на видео (или 'exit', 'выход' для выхода): ")
		url, _ := reader.ReadString('\n')
		url = strings.TrimSpace(url)

		if strings.ToLower(url) == "exit" || strings.ToLower(url) == "quit" || strings.ToLower(url) == "выход"{
			fmt.Println("Завершение работы...")
			break 
		}

		if url != "" {
			downloadVideo(url)
		} else {
			fmt.Println("Пустая ссылка, попробуйте еще раз.")
		}
	}
}

func checkDependencies() {
	dependencies := []string{"ffmpeg", "yt-dlp"}
	for _, dep := range dependencies {
		if _, err := exec.LookPath(dep); err != nil {
			fmt.Printf("ОШИБКА: Утилита '%s' не найдена в системе!\n", dep)
			fmt.Println("Нажмите Enter для выхода...")
			bufio.NewReader(os.Stdin).ReadString('\n')
			os.Exit(1)
		}
	}
}

func askForUpdate() {
	fmt.Print("Проверить обновление yt-dlp? (y/n): ")
	var answer string

	fmt.Scanln(&answer)
	answer = strings.ToLower(strings.TrimSpace(answer))

	if answer == "y" || answer == "д" {
		fmt.Println("⏳ Проверка обновлений...")
		cmd := exec.Command("yt-dlp", "-U")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err == nil {
			fmt.Println("Обновление успешно (если оно было).")

		} else {
			fmt.Printf("Ошибка при обновлении: %v\n", err)
		}
	}
}

func downloadVideo(url string) {
    outputDir := "downloads"
    _ = os.MkdirAll(outputDir, os.ModePerm)
    outputPath := outputDir + "/%(title)s.%(ext)s"

    fmt.Printf("\nЗапуск загрузки...\n")
	fmt.Println("\n_______________________________\n")

    cmd := exec.Command("yt-dlp",
        "-f", "bv*[vcodec^=avc]+ba[ext=m4a]/best[ext=mp4]/best",
        "--merge-output-format", "mp4",
        "--no-playlist",
        "--no-warnings",
        

        "--progress",
        "--newline", 

        "--progress-template", "download:[download] %(progress._percent_str)s | Скорость: %(progress._speed_str)s | ETA: %(progress._eta_str)s",

        "--downloader", "ffmpeg",
        "--external-downloader-args", "ffmpeg:-loglevel info", 
        "-o", outputPath,
        url,
    )

    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    if err := cmd.Run(); err != nil {
        fmt.Printf("\nОшибка: %v\n", err)
    } else {
		fmt.Println("\n_______________________________\n")
        fmt.Println("\nГотово! Файл в папке 'downloads'.")
    }

    fmt.Println("\nНажмите Enter, чтобы продолжить...")
    inputScanner := bufio.NewScanner(os.Stdin)
    inputScanner.Scan() 
}

func clearScreen() {
    var cmd *exec.Cmd
    if strings.Contains(strings.ToLower(os.Getenv("OS")), "windows") {
        cmd = exec.Command("cls")
    } else {
        cmd = exec.Command("clear")
    }
    cmd.Stdout = os.Stdout
    cmd.Run()
}

func showLogo() {
	fmt.Println("=======================")
	fmt.Println("  Видео-загрузчик на Go")
	fmt.Println("  By Ch3kL1st")
	fmt.Println("=======================")
}