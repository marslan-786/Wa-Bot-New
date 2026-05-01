package main

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
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
	// This will safely delete the downloaded file and folder AFTER function finishes
	defer os.RemoveAll(tempDir)

	// Android User-Agent to prevent blocks
	userAgent := "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Mobile Safari/537.36"

	outputTemplate := filepath.Join(tempDir, "%(id)s.%(ext)s")

	// Smart Format: Strictly H.264 (avc1)
	formatString := "bestvideo[height<=360][vcodec^=avc1]+bestaudio[language*=hi][ext=m4a]/bestvideo[height<=360][vcodec^=avc1]+bestaudio[ext=m4a]/best[height<=360][vcodec^=avc1]"

	// Command execution
	cmd := exec.Command("yt-dlp",
		"--no-warnings",
		"--no-playlist",
		"--merge-output-format", "mp4",
		"-f", formatString,
		"--user-agent", userAgent,
		// 🔥 Force FFmpeg to make it WhatsApp friendly
		"--postprocessor-args", "ffmpeg:-c:v libx264 -pix_fmt yuv420p -c:a aac -movflags +faststart", 
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
	replyMessage(client, v, "📤 Uploading file to server...")

	// ---------------------------------------------------------
	// 📤 CORE UPLOAD FUNCTION (Custom Catbox API via HTTP POST)
	// ---------------------------------------------------------
	
	fileToUpload, err := os.Open(downloadedPath)
	if err != nil {
		replyMessage(client, v, "❌ *System Error:* Failed to read downloaded file.")
		return
	}
	defer fileToUpload.Close()

	// Prepare multipart form data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	
	// Catbox standard parameters
	_ = writer.WriteField("reqtype", "fileupload")
	
	part, err := writer.CreateFormFile("fileToUpload", filepath.Base(downloadedPath))
	if err == nil {
		_, _ = io.Copy(part, fileToUpload)
	}
	writer.Close()

	// HTTP POST request to your API
	req, err := http.NewRequest("POST", "https://catbox-production-6705.up.railway.app/upload", body)
	if err != nil {
		replyMessage(client, v, "❌ *Upload Error:* Request creation failed.")
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		replyMessage(client, v, fmt.Sprintf("❌ *API Error:* %v", err))
		return
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		replyMessage(client, v, "❌ *System Error:* Failed to read API response.")
		return
	}

	// Check if upload was successful
	if resp.StatusCode != http.StatusOK {
		replyMessage(client, v, fmt.Sprintf("❌ *Upload Failed:* Server returned status %d\n%s", resp.StatusCode, string(respBytes)))
		return
	}

	uploadedURL := strings.TrimSpace(string(respBytes))

	// Send the received link to WhatsApp
	replyMessage(client, v, fmt.Sprintf("✅ *Video Uploaded Successfully!*\n\n🔗 *Watch / Download:* %s", uploadedURL))
	react(client, v, "✅")
}