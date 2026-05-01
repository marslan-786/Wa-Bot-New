package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
    
    "go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// کوئی کوکیز نہیں، صرف yt-dlp + اینڈرائیڈ ہیڈرز

// ========== مین ہینڈلر (.dwn) ==========
// ========== مین ہینڈلر (.dwn) ==========
func handleYTDownload(client *whatsmeow.Client, v *events.Message, videoURL string) {
	if videoURL == "" {
		replyMessage(client, v, "❌ لنک فراہم کریں!\nمثال: `.dwn https://youtu.be/xxxx`")
		return
	}

	replyMessage(client, v, "⏳ 360p میں ویڈیو ڈاؤن لوڈ ہو رہی ہے (ہندی آڈیو کے ساتھ اگر دستیاب ہوئی)...")

	// عارضی ڈائریکٹری
	tempDir, err := os.MkdirTemp("", "ytdlp_*")
	if err != nil {
		replyMessage(client, v, "❌ عارضی ڈائریکٹری بنانے میں مسئلہ")
		return
	}
	defer os.RemoveAll(tempDir)

	// اینڈرائیڈ یوزر ایجنٹ
	userAgent := "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Mobile Safari/537.36"

	outputTemplate := filepath.Join(tempDir, "%(id)s.%(ext)s")

	// ---- اصل جادو یہاں ہے ----
	// یہ لائن yt-dlp کو بتاتی ہے: 
	// 1. سب سے پہلے 360p یا اس سے کم کی ویڈیو اور ہندی آڈیو کی ID ڈھونڈو اور ملاؤ۔
	// 2. اگر ہندی آڈیو نہ ملے، تو 360p ویڈیو کے ساتھ ڈیفالٹ آڈیو ملاؤ۔
	// 3. اگر الگ الگ نہ ملیں، تو جو بھی بیسٹ 360p فارمیٹ ہو وہ ڈاؤن لوڈ کر لو۔
	formatString := "bestvideo[height<=360]+bestaudio[language*=hi]/bestvideo[height<=360]+bestaudio/best[height<=360]"

	cmd := exec.Command("yt-dlp",
		"--no-warnings",
		"--no-playlist",
		"--merge-output-format", "mp4",
		"-f", formatString,
		"--user-agent", userAgent,
		"--output", outputTemplate,
		videoURL,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		replyMessage(client, v, fmt.Sprintf("❌ yt-dlp ایرر:\n%s", stderr.String()))
		return
	}

	// ڈاؤن لوڈ شدہ فائل تلاش کریں
	files, _ := os.ReadDir(tempDir)
	var downloadedPath string
	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(strings.ToLower(f.Name()), ".mp4") {
			downloadedPath = filepath.Join(tempDir, f.Name())
			break
		}
	}
	
	if downloadedPath == "" {
		replyMessage(client, v, "❌ ڈاؤن لوڈ شدہ فائل نہیں ملی")
		return
	}

	// فائل کا سائز
	fileInfo, err := os.Stat(downloadedPath)
	if err != nil {
		replyMessage(client, v, "❌ فائل کی معلومات نہیں مل سکی")
		return
	}
	fileSize := fileInfo.Size()
	const maxVideoSize int64 = 50 * 1024 * 1024 // 50MB لیمٹ

	finalData, err := os.ReadFile(downloadedPath)
	if err != nil {
		replyMessage(client, v, "❌ فائل پڑھنے میں مسئلہ")
		return
	}

	if fileSize > maxVideoSize {
		// ڈاکومنٹ کے طور پر اپلوڈ
		uploadResp, err := client.Upload(context.Background(), finalData, whatsmeow.MediaDocument)
		if err != nil {
			replyMessage(client, v, fmt.Sprintf("❌ واٹس ایپ اپلوڈ ایرر (Document): %v", err))
			return
		}
		msg := &waProto.Message{
			DocumentMessage: &waProto.DocumentMessage{
				URL:           proto.String(uploadResp.URL),
				DirectPath:    proto.String(uploadResp.DirectPath),
				MediaKey:      uploadResp.MediaKey,
				Mimetype:      proto.String("video/mp4"),
				FileEncSHA256: uploadResp.FileEncSHA256,
				FileSHA256:    uploadResp.FileSHA256,
				FileLength:    proto.Uint64(uint64(fileSize)),
				FileName:      proto.String("Downloaded_Video.mp4"),
			},
		}
		client.SendMessage(context.Background(), v.Info.Chat, msg)
		replyMessage(client, v, fmt.Sprintf("✅ ویڈیو سائز بڑا تھا اس لیے بطور ڈاکومنٹ بھیج دی گئی (سائز: %.1f MB)", float64(fileSize)/(1024*1024)))
	} else {
		// نارمل ویڈیو کے طور پر اپلوڈ
		uploadResp, err := client.Upload(context.Background(), finalData, whatsmeow.MediaVideo)
		if err != nil {
			replyMessage(client, v, fmt.Sprintf("❌ واٹس ایپ اپلوڈ ایرر (Video): %v", err))
			return
		}
		msg := &waProto.Message{
			VideoMessage: &waProto.VideoMessage{
				URL:           proto.String(uploadResp.URL),
				DirectPath:    proto.String(uploadResp.DirectPath),
				MediaKey:      uploadResp.MediaKey,
				Mimetype:      proto.String("video/mp4"),
				FileEncSHA256: uploadResp.FileEncSHA256,
				FileSHA256:    uploadResp.FileSHA256,
				FileLength:    proto.Uint64(uint64(fileSize)),
			},
		}
		client.SendMessage(context.Background(), v.Info.Chat, msg)
		replyMessage(client, v, "✅ 360p میں ویڈیو کامیابی سے ڈاؤن لوڈ ہو گئی!")
	}
}
