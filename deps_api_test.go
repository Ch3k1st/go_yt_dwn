package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingDepsReportsAbsentTools(t *testing.T) {
	oldYt, oldFf := ytDlpPath, ffmpegPath
	t.Cleanup(func() { ytDlpPath, ffmpegPath = oldYt, oldFf })

	dir := t.TempDir()
	ytDlpPath = filepath.Join(dir, "yt-dlp")
	ffmpegPath = filepath.Join(dir, "ffmpeg")
	if got := missingDeps(); len(got) != 2 {
		t.Fatalf("оба инструмента отсутствуют, а найдено: %v", got)
	}

	if err := os.WriteFile(ytDlpPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := missingDeps()
	if len(got) != 1 || got[0] != "ffmpeg" {
		t.Fatalf("ожидался только ffmpeg, получено: %v", got)
	}
}
