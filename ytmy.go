package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

// ========== MAIN HANDLER (.dwn) ==========
func handleYTDownload(client *whatsmeow.Client, v *events.Message, videoURL string) {
	if videoURL == "" {
		replyMessage(client, v, "❌ *Error:* Please provide a valid link!\n*Example:* `.dwn https://youtu.be/xxxx`")
		return
	}

	react(client, v, "⏳")
	replyMessage(client, v, "⏳ Downloading 360p video (with Hindi audio if available)...")

	// Create Temporary Directory
	tempDir, err := os.MkdirTemp("", "ytdlp_*")
	if err != nil {
		replyMessage(client, v, "❌ *System Error:* Failed to create temporary directory.")
		return
	}
	// This will safely delete the downloaded file and folder AFTER uploadAndSendFile finishes
	defer os.RemoveAll(tempDir)

	// Android User-Agent to prevent blocks
	userAgent := "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Mobile Safari/537.36"

	outputTemplate := filepath.Join(tempDir, "%(id)s.%(ext)s")

	// Smart Format: 360p Hindi -> 360p Default -> Best 360p
	formatString := "bestvideo[height<=360]+bestaudio[language*=hi]/bestvideo[height<=360]+bestaudio/best[height<=360]"

	// Command execution
	cmd := exec.Command("yt-dlp",
		"--no-warnings",
		"--no-playlist",
		"--merge-output-format", "mp4",
		"-f", formatString,
		"--user-agent", userAgent,
		"--postprocessor-args", "ffmpeg:-movflags +faststart", // 🔥 THE MAGIC FIX: Makes it playable in WhatsApp
		"--output", outputTemplate,
		videoURL,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	
	if err != nil {
		replyMessage(client, v, fmt.Sprintf("❌ *yt-dlp Error:*\n```\n%s\n```", stderr.String()))
		react(client, v, "❌")
		return
	}

	// Locate the downloaded .mp4 file
	files, _ := os.ReadDir(tempDir)
	var downloadedPath string
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(strings.ToLower(f.Name()), ".mp4") {
			downloadedPath = filepath.Join(tempDir, f.Name())
			break
		}
	}
	
	if downloadedPath == "" {
		replyMessage(client, v, "❌ *Error:* Downloaded file could not be found.")
		react(client, v, "❌")
		return
	}

	react(client, v, "📤")

	// 📤 CORE UPLOAD FUNCTION 
	// Sending false for isAudio, partNum 1, totalParts 1
	uploadAndSendFile(client, v, downloadedPath, "YouTube Video", false, 1, 1)
	
	react(client, v, "✅")
}
