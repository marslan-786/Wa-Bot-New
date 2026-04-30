package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

var ytCookieHeader string

// ========== کوکیز لوڈر ==========
func loadYoutubeCookies() {
	type CookieJSON struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Domain string `json:"domain"`
		Path   string `json:"path"`
	}

	file, err := os.Open("youtube_cookies.json")
	if err != nil {
		fmt.Println("⚠️ [YT-DWN] youtube_cookies.json not found:", err)
		return
	}
	defer file.Close()

	var rawCookies []CookieJSON
	if err := json.NewDecoder(file).Decode(&rawCookies); err != nil {
		fmt.Println("⚠️ [YT-DWN] Error decoding cookies:", err)
		return
	}

	var parts []string
	hasConsent := false
	ytCount := 0

	for _, c := range rawCookies {
		if !strings.Contains(c.Domain, "youtube.com") {
			continue
		}
		cleanValue := strings.ReplaceAll(c.Value, "\n", "")
		cleanValue = strings.ReplaceAll(cleanValue, "\r", "")
		parts = append(parts, fmt.Sprintf("%s=%s", c.Name, cleanValue))
		ytCount++
		if strings.ToUpper(c.Name) == "CONSENT" {
			hasConsent = true
		}
	}

	if !hasConsent {
		parts = append(parts, "CONSENT=YES+cb.20210328-17-p0.en+FX+478")
		ytCount++
	}

	ytCookieHeader = strings.Join(parts, "; ")
	fmt.Printf("🍪 [YT-DWN] Loaded %d YouTube cookies successfully!\n", ytCount)
}

func init() {
	loadYoutubeCookies()
}

// ========== یوٹیوب پیج سے JSON ڈیٹا نکالیں ==========
func extractYTPlayerData(videoURL string) (map[string]interface{}, error) {
	videoID := ""
	if strings.Contains(videoURL, "v=") {
		parts := strings.Split(videoURL, "v=")
		videoID = strings.Split(parts[1], "&")[0]
	} else if strings.Contains(videoURL, "youtu.be/") {
		parts := strings.Split(videoURL, "youtu.be/")
		videoID = strings.Split(parts[1], "?")[0]
	} else {
		videoID = videoURL
	}

	req, _ := http.NewRequest("GET", "https://m.youtube.com/watch?v="+videoID, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Mobile Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if ytCookieHeader != "" {
		req.Header.Set("Cookie", ytCookieHeader)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch video page: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyString := string(bodyBytes)

	re := regexp.MustCompile(`(?s)ytInitialPlayerResponse\s*=\s*(\{.+?\});(?:var\s|</script>)`)
	match := re.FindStringSubmatch(bodyString)
	if len(match) < 2 {
		return nil, fmt.Errorf("player response not found")
	}

	var playerData map[string]interface{}
	err = json.Unmarshal([]byte(match[1]), &playerData)
	return playerData, err
}

// ========== مخصوص زبان کی بہترین آڈیو ==========
func findBestAudioTrack(adaptiveFormats []interface{}, langCode string) map[string]interface{} {
	var best map[string]interface{}
	var bestBitrate float64 = 0

	for _, f := range adaptiveFormats {
		fmtData, ok := f.(map[string]interface{})
		if !ok {
			continue
		}
		mime, _ := fmtData["mimeType"].(string)
		if !strings.HasPrefix(mime, "audio/") {
			continue
		}

		audioTrack, hasTrack := fmtData["audioTrack"].(map[string]interface{})
		if langCode == "orig" {
			if hasTrack {
				continue
			}
		} else {
			if !hasTrack {
				continue
			}
			code, _ := audioTrack["languageCode"].(string)
			if code != langCode {
				continue
			}
		}

		bitrate, _ := fmtData["bitrate"].(float64)
		if bitrate > bestBitrate {
			bestBitrate = bitrate
			best = fmtData
		}
	}
	return best
}

// ========== بہترین ویڈیو (بغیر آواز) ==========
func findBestVideoOnly(adaptiveFormats []interface{}) map[string]interface{} {
	var best map[string]interface{}
	bestHeight := 0
	for _, f := range adaptiveFormats {
		fmtData, ok := f.(map[string]interface{})
		if !ok {
			continue
		}
		mime, _ := fmtData["mimeType"].(string)
		if !strings.HasPrefix(mime, "video/") {
			continue
		}
		if _, hasAudio := fmtData["audioChannels"]; hasAudio {
			continue
		}
		height := 0
		if h, ok := fmtData["height"].(float64); ok {
			height = int(h)
		}
		if height > bestHeight {
			bestHeight = height
			best = fmtData
		}
	}
	return best
}

// ========== مین ہینڈلر .dwn ==========
func handleYTDownload(client *whatsmeow.Client, v *events.Message, videoURL string) {
	if videoURL == "" {
		replyMessage(client, v, "❌ لنک فراہم کریں!\nمثال: `.dwn https://youtu.be/xxxx`")
		return
	}

	replyMessage(client, v, "⏳ یوٹیوب سے ڈیٹا نکال رہا ہوں...")
	playerData, err := extractYTPlayerData(videoURL)
	if err != nil {
		replyMessage(client, v, fmt.Sprintf("❌ ایرر: %v", err))
		return
	}

	streamingData, ok := playerData["streamingData"].(map[string]interface{})
	if !ok {
		replyMessage(client, v, "❌ سٹریمنگ ڈیٹا موجود نہیں")
		return
	}

	adaptiveFormats, _ := streamingData["adaptiveFormats"].([]interface{})
	if adaptiveFormats == nil {
		replyMessage(client, v, "❌ اڈاپٹیو فارمیٹس نہیں ملے")
		return
	}

	// ---- زبان کا انتخاب (اردو > ہندی > اوریجنل) ----
	var targetLang string
	audioTrack := findBestAudioTrack(adaptiveFormats, "ur")
	if audioTrack != nil {
		targetLang = "ur"
	} else {
		audioTrack = findBestAudioTrack(adaptiveFormats, "hi")
		if audioTrack != nil {
			targetLang = "hi"
		} else {
			audioTrack = findBestAudioTrack(adaptiveFormats, "orig")
			if audioTrack != nil {
				targetLang = "orig"
			}
		}
	}

	if audioTrack == nil {
		replyMessage(client, v, "❌ کوئی آڈیو ٹریک نہیں ملا")
		return
	}

	videoTrack := findBestVideoOnly(adaptiveFormats)
	if videoTrack == nil {
		replyMessage(client, v, "❌ بغیر آواز ویڈیو ٹریک نہیں ملا")
		return
	}

	audioURL, _ := audioTrack["url"].(string)
	videoOnlyURL, _ := videoTrack["url"].(string)

	if audioURL == "" || videoOnlyURL == "" {
		replyMessage(client, v, "❌ ڈاؤن لوڈ لنک نہیں نکل سکا")
		return
	}

	replyMessage(client, v, fmt.Sprintf("⬇️ ڈاؤن لوڈ کر رہا ہوں... زبان: %s", targetLang))

	// ---- ڈاؤن لوڈ اور مرج ----
	tempVideo, err := os.CreateTemp("", "ytvideo_*.mp4")
	if err != nil {
		replyMessage(client, v, "❌ عارضی فائل بنانے میں مسئلہ")
		return
	}
	defer os.Remove(tempVideo.Name())

	tempAudio, err := os.CreateTemp("", "ytaudio_*.m4a")
	if err != nil {
		replyMessage(client, v, "❌ عارضی فائل بنانے میں مسئلہ")
		return
	}
	defer os.Remove(tempAudio.Name())

	outFile, err := os.CreateTemp("", "ytfinal_*.mp4")
	if err != nil {
		replyMessage(client, v, "❌ فائنل فائل بنانے میں مسئلہ")
		return
	}
	defer os.Remove(outFile.Name())

	// ویڈیو ڈاؤن لوڈ
	respV, err := http.Get(videoOnlyURL)
	if err != nil {
		replyMessage(client, v, "❌ ویڈیو ڈاؤن لوڈ میں مسئلہ")
		return
	}
	io.Copy(tempVideo, respV.Body)
	respV.Body.Close()

	// آڈیو ڈاؤن لوڈ
	respA, err := http.Get(audioURL)
	if err != nil {
		replyMessage(client, v, "❌ آڈیو ڈاؤن لوڈ میں مسئلہ")
		return
	}
	io.Copy(tempAudio, respA.Body)
	respA.Body.Close()

	// ffmpeg سے مرج
	cmd := exec.Command("ffmpeg", "-y",
		"-i", tempVideo.Name(),
		"-i", tempAudio.Name(),
		"-c", "copy",
		"-map", "0:v:0",
		"-map", "1:a:0",
		outFile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		replyMessage(client, v, fmt.Sprintf("❌ مرج کرتے وقت ایرر: %s\n%s", err.Error(), output))
		return
	}

	// فائنل فائل کی سائز چیک
	fileInfo, err := os.Stat(outFile.Name())
	if err != nil {
		replyMessage(client, v, "❌ فائل سائز چیک کرنے میں مسئلہ")
		return
	}
	fileSize := fileInfo.Size()
	const maxVideoSize int64 = 50 * 1024 * 1024 // 50 MB

	finalData, err := os.ReadFile(outFile.Name())
	if err != nil {
		replyMessage(client, v, "❌ فائنل فائل پڑھنے میں مسئلہ")
		return
	}

	if fileSize > maxVideoSize {
		// Document کی صورت میں بھیجیں
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
				FileName:      proto.String(fmt.Sprintf("Video_%s.mp4", targetLang)),
			},
		}
		client.SendMessage(context.Background(), v.Info.Chat, msg)
		replyMessage(client, v, fmt.Sprintf("✅ ویڈیو بطور ڈاکومنٹ بھیج دی گئی (زبان: %s, سائز: %.1f MB)", targetLang, float64(fileSize)/(1024*1024)))
	} else {
		// ویڈیو میسج کی صورت میں
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
		replyMessage(client, v, fmt.Sprintf("✅ ویڈیو %s زبان میں ڈاؤن لوڈ ہو گئی!", targetLang))
	}
}