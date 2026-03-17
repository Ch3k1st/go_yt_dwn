package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {

	_, errFF := exec.LookPath("ffmpeg")
	if errFF != nil {
		fmt.Println("ОШИБКА: FFmpeg не найден в системе!")
		return
	}

	_, errYT := exec.LookPath("yt-dlp")
	if errYT != nil {
		fmt.Println("ОШИБКА: yt-dlp не найден! Установите его перед запуском.")
		return
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Видео-загрузчик на Go")
	fmt.Println("By Ch3kL1st ")
	fmt.Println("-----------------------")

	fmt.Print("Введите ссылку на видео: ")
	url, _ := reader.ReadString('\n')
	url = strings.TrimSpace(url)

	if url == "" {
		return
	}

	outputDir := "downloads"
	_ = os.MkdirAll(outputDir, os.ModePerm)

	outputPath := outputDir + "/%(title)s.%(ext)s"
	
	cmd := exec.Command("yt-dlp", 

        "-f", "bv*[vcodec^=avc]+ba[ext=m4a]/best[ext=mp4]/best",
        "--merge-output-format", "mp4",
        "--no-playlist", 
        "-o", outputPath,
        url,
    )

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("\nПроцесс запущен...\n")

	err := cmd.Run()
	if err != nil {
		fmt.Printf("\n Ошибка: %v\n", err)
		return
	}

	fmt.Println("\nГотово! Файл скачан :).")
}