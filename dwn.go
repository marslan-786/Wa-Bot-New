package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"bytes"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// ==========================================
// 🛡️ STATE CACHES
// ==========================================
type MediaSession struct {
	Results  []SearchResult
	SenderID string
}

type SearchResult struct {
	Title string
	Url   string
}

type YTDownloadState struct {
	Url      string
	SenderID string
}

var ytSearchCache = make(map[string]MediaSession)
var ttSearchCache = make(map[string]MediaSession)
var ytQualityCache = make(map[string]YTDownloadState)

// ==========================================
// 🌐 CONSTANTS & API STRUCTS
// ==========================================
type APIResponse struct {
	Success     bool   `json:"success"`
	Title       string `json:"title"`
	Resolution  string `json:"resolution"`
	DownloadURL string `json:"download_url"`
}

// واٹس ایپ کی سیف لمٹ: 1.8 GB (بائٹس میں)
const MaxWhatsAppSize int64 = 1932735283 // 1.8 GB in bytes
const SafeMarginMB = 1800.0

// ==========================================
// 🚀 1. API DOWNLOADER (For YT & TikTok)
// ==========================================
func downloadViaAPI(client *whatsmeow.Client, v *events.Message, targetUrl, resolution string, isAudio bool) {
	react(client, v.Info.Chat, v.Info.ID, "⬇️")

	httpClient := &http.Client{Timeout: 5 * time.Minute}

	apiUrl := fmt.Sprintf("https://silent-yt-dwn.up.railway.app/api/download?url=%s&resolution=%s", targetUrl, resolution)
	resp, err := httpClient.Get(apiUrl)
	if err != nil { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
	defer resp.Body.Close()

	var apiRes APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiRes); err != nil || !apiRes.Success || apiRes.DownloadURL == "" {
		react(client, v.Info.Chat, v.Info.ID, "❌"); return
	}

	fileResp, err := httpClient.Get(apiRes.DownloadURL)
	if err != nil { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
	defer fileResp.Body.Close()

	ext := ".mp4"
	if isAudio { ext = ".mp3" }
	tempFileName := fmt.Sprintf("./data/temp_%d%s", time.Now().UnixNano(), ext)
	
	outFile, err := os.Create(tempFileName)
	if err != nil { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
	
	_, err = io.Copy(outFile, fileResp.Body)
	outFile.Close()
	if err != nil { os.Remove(tempFileName); react(client, v.Info.Chat, v.Info.ID, "❌"); return }

	defer os.Remove(tempFileName)

	fileInfo, err := os.Stat(tempFileName)
	if err != nil { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
	
	fileSize := fileInfo.Size()
	var filesToSend []string

	if fileSize > MaxWhatsAppSize && !isAudio {
		react(client, v.Info.Chat, v.Info.ID, "✂️") 
		parts, err := splitVideoSmart(tempFileName, SafeMarginMB)
		if err != nil || len(parts) == 0 {
			filesToSend = append(filesToSend, tempFileName)
		} else {
			filesToSend = parts
		}
	} else {
		filesToSend = append(filesToSend, tempFileName)
	}

	react(client, v.Info.Chat, v.Info.ID, "📤")

	for i, filePath := range filesToSend {
		uploadAndSendFile(client, v, filePath, apiRes.Title, isAudio, i+1, len(filesToSend))
		if filePath != tempFileName {
			os.Remove(filePath)
		}
	}

	react(client, v.Info.Chat, v.Info.ID, "✅")
}

// ==========================================
// 🚀 2. UNIVERSAL YT-DLP DOWNLOADER (For FB, Insta, etc.)
// ==========================================
// ==========================================
// 🔗 HELPER: URL EXPANDER (Fixes 404 on Snapchat/FB)
// ==========================================
func getFinalURL(shortURL string) string {
	// اگر لنک میں یہ الفاظ ہوں تبھی ایکسپیینڈ کرو
	if !strings.Contains(shortURL, "snapchat.com/t/") && !strings.Contains(shortURL, "share/r/") && !strings.Contains(shortURL, "pin.it/") {
		return shortURL
	}

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", shortURL, nil)
	if err != nil { return shortURL }
	
	// براؤزر کا روپ تاکہ 404 نہ آئے
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	
	resp, err := client.Do(req)
	if err != nil { return shortURL }
	defer resp.Body.Close()
	
	finalUrl := resp.Request.URL.String()
	fmt.Printf("🔗 https://my.clevelandclinic.org/health/treatments/23502-palate-expander Short: %s -> Final: %s\n", shortURL, finalUrl)
	return finalUrl
}

// ==========================================
// 🚀 THE MASTER ENGINE (yt-dlp with Heavy Bypass)
// ==========================================
func downloadAndSend(client *whatsmeow.Client, v *events.Message, targetUrl, mode string, optionalFormat ...string) {
	react(client, v.Info.Chat, v.Info.ID, "⬇️")

	// 🔥 CRITICAL FIX: شارٹ لنک کو اصلی لنک میں تبدیل کریں
	targetUrl = getFinalURL(targetUrl)

	isYouTube := strings.Contains(strings.ToLower(targetUrl), "youtu")
	defaultUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	
	cmdTitle := exec.Command("yt-dlp", "--get-title", "--no-playlist", "--user-agent", defaultUA, targetUrl)
	titleOut, _ := cmdTitle.Output()

	cleanTitle := "Media_File"
	if len(titleOut) > 0 {
		cleanTitle = strings.TrimSpace(string(titleOut))
		cleanTitle = strings.Map(func(r rune) rune {
			if strings.ContainsRune(`/\?%*:|"<>`, r) { return '-' }
			return r
		}, cleanTitle)
	}

	tempFileName := fmt.Sprintf("temp_%d.mp4", time.Now().UnixNano())
	formatArg := "bestvideo+bestaudio/best"
	
	if len(optionalFormat) > 0 && optionalFormat[0] != "" {
		formatArg = optionalFormat[0]
	}

	isAudio := false
	if mode == "audio" {
		isAudio = true
		tempFileName = strings.Replace(tempFileName, ".mp4", ".mp3", 1)
	}

	var downloadErr error
	var rawErrorOutput string
	maxAttempts := 3

	for attempt := 0; attempt < maxAttempts; attempt++ {
		commonArgs := []string{
			"--no-playlist",
			"--force-ipv4",
			"--no-check-certificate",
			"--geo-bypass",
		}

		if attempt > 0 {
			commonArgs = append(commonArgs, "--rm-cache-dir")
		}

		if isYouTube {
			commonArgs = append(commonArgs, "--user-agent", defaultUA)
		} else {
			// یونیورسل بائی پاس ہیڈرز
			commonArgs = append(commonArgs, 
				"--user-agent", defaultUA,
				"--add-header", "Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
				"--add-header", "Accept-Language: en-US,en;q=0.5",
				"--add-header", "Sec-Fetch-Dest: document",
				"--add-header", "Sec-Fetch-Mode: navigate",
				"--add-header", "Sec-Fetch-Site: cross-site",
			)
			if !isAudio && (len(optionalFormat) == 0 || optionalFormat[0] == "") {
				formatArg = "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]/best"
			}
		}

		var args []string
		if isAudio {
			args = append(commonArgs, "-f", "bestaudio/best", "--extract-audio", "--audio-format", "mp3", "--audio-quality", "192K", "-o", tempFileName, targetUrl)
		} else {
			// 🔥 WHATSAPP PLAYBACK FIX 🔥
			// 1. -S "vcodec:h264" -> واٹس ایپ کے لیے لازمی H.264 کوڈیک فورس کرے گا
			// 2. --postprocessor-args -> پکسل فارمیٹ کو موبائل سکرینز کے حساب سے سیٹ کرے گا
			args = append(commonArgs, 
				"-S", "vcodec:h264,res,acodec:m4a", 
				"--postprocessor-args", "Video:-pix_fmt yuv420p", 
				"-f", formatArg, 
				"--merge-output-format", "mp4", 
				"-o", tempFileName, 
				targetUrl,
			)
		}

		cmd := exec.Command("yt-dlp", args...)
		var stderr bytes.Buffer
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
		
		downloadErr = cmd.Run()

		if downloadErr == nil {
			break
		}
		
		rawErrorOutput = strings.TrimSpace(stderr.String())
	}

	if downloadErr != nil {
		fmt.Printf("❌ Download Error permanently: %v\n", downloadErr)
		if len(rawErrorOutput) > 3000 {
			rawErrorOutput = rawErrorOutput[:3000] + "\n...[Truncated]"
		}
		replyMessage(client, v, fmt.Sprintf("❌ *Download Error:*\n```\n%s\n```", rawErrorOutput))
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return
	}

	finalExt := ".mp4"
	if isAudio { finalExt = ".mp3" }
	finalPath := cleanTitle + finalExt
	os.Rename(tempFileName, finalPath)

	defer os.Remove(finalPath)

	fileInfo, err := os.Stat(finalPath)
	if err != nil { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
	
	fileSize := fileInfo.Size()
	var filesToSend []string

	if fileSize > MaxWhatsAppSize && !isAudio {
		react(client, v.Info.Chat, v.Info.ID, "✂️") 
		parts, err := splitVideoSmart(finalPath, SafeMarginMB)
		if err != nil || len(parts) == 0 {
			filesToSend = append(filesToSend, finalPath)
		} else {
			filesToSend = parts
		}
	} else {
		filesToSend = append(filesToSend, finalPath)
	}

	react(client, v.Info.Chat, v.Info.ID, "📤")

	for i, filePath := range filesToSend {
		uploadAndSendFile(client, v, filePath, cleanTitle, isAudio, i+1, len(filesToSend))
		if filePath != finalPath {
			os.Remove(filePath)
		}
	}

	react(client, v.Info.Chat, v.Info.ID, "✅")
}


// ==========================================
// 📤 3. CORE UPLOAD & SEND FUNCTION
// ==========================================
func uploadAndSendFile(client *whatsmeow.Client, v *events.Message, filePath string, title string, isAudio bool, partNum int, totalParts int) {
	fileData, err := os.ReadFile(filePath)
	if err != nil { 
		fmt.Printf("❌ ReadFile failed: %v\n", err)
		return 
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var mType whatsmeow.MediaType
	var mime string
	if isAudio { 
		mType = whatsmeow.MediaAudio; mime = "audio/mpeg" 
	} else { 
		if len(fileData) > 90*1024*1024 {
			mType = whatsmeow.MediaDocument; mime = "video/mp4"
		} else {
			mType = whatsmeow.MediaVideo; mime = "video/mp4"
		}
	}

	up, err := client.Upload(ctx, fileData, mType)
	if err != nil { 
		fmt.Printf("❌ Upload failed for part %d: %v\n", partNum, err)
		return 
	}

	var msg waProto.Message
	finalTitle := title
	if totalParts > 1 {
		finalTitle = fmt.Sprintf("%s (Part %d/%d)", title, partNum, totalParts)
	}

	if isAudio {
		msg.AudioMessage = &waProto.AudioMessage{
			URL:           proto.String(up.URL), 
			DirectPath:    proto.String(up.DirectPath), 
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(mime), 
			FileLength:    proto.Uint64(uint64(len(fileData))), 
			PTT:           proto.Bool(false),
			FileSHA256:    up.FileSHA256,       
			FileEncSHA256: up.FileEncSHA256,    
			ContextInfo: &waProto.ContextInfo{
				StanzaID:      proto.String(v.Info.ID),
				Participant:   proto.String(v.Info.Sender.String()),
				QuotedMessage: v.Message,
			},
		}
	} else if mType == whatsmeow.MediaDocument {
		msg.DocumentMessage = &waProto.DocumentMessage{
			URL:           proto.String(up.URL), 
			DirectPath:    proto.String(up.DirectPath), 
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(mime), 
			Title:         proto.String(finalTitle), 
			FileName:      proto.String(finalTitle + ".mp4"),
			FileLength:    proto.Uint64(uint64(len(fileData))), 
			Caption:       proto.String("✅ " + finalTitle),
			FileSHA256:    up.FileSHA256,       
			FileEncSHA256: up.FileEncSHA256,    
			ContextInfo: &waProto.ContextInfo{
				StanzaID:      proto.String(v.Info.ID),
				Participant:   proto.String(v.Info.Sender.String()),
				QuotedMessage: v.Message,
			},
		}
	} else {
		msg.VideoMessage = &waProto.VideoMessage{
			URL:           proto.String(up.URL), 
			DirectPath:    proto.String(up.DirectPath), 
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(mime), 
			Caption:       proto.String("✅ " + finalTitle), 
			FileLength:    proto.Uint64(uint64(len(fileData))),
			FileSHA256:    up.FileSHA256,       
			FileEncSHA256: up.FileEncSHA256,    
			ContextInfo: &waProto.ContextInfo{
				StanzaID:      proto.String(v.Info.ID),
				Participant:   proto.String(v.Info.Sender.String()),
				QuotedMessage: v.Message,
			},
		}
	}

	_, err = client.SendMessage(ctx, v.Info.Chat, &msg)
	if err != nil {
		fmt.Printf("❌ SendMessage Error: %v\n", err)
	}
}

// ==========================================
// ✂️ 4. SMART SPLIT FUNCTION (FFMPEG)
// ==========================================
func splitVideoSmart(inputPath string, targetMB float64) ([]string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", inputPath)
	out, err := cmd.Output()
	if err != nil { return nil, err }
	
	durationSec, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	
	info, _ := os.Stat(inputPath)
	totalSizeMB := float64(info.Size()) / (1024 * 1024)
	
	chunkDuration := (targetMB / totalSizeMB) * durationSec
	chunkDuration = chunkDuration * 0.95 // 5% Safe margin

	fmt.Printf("✂️ Splitting video. Total: %.2f MB, Target: %.2f MB, Chunk Time: %.0f sec\n", totalSizeMB, targetMB, chunkDuration)

	outputPattern := strings.Replace(inputPath, ".mp4", "_part%03d.mp4", 1)
	
	splitCmd := exec.Command("ffmpeg", 
		"-i", inputPath, 
		"-c", "copy",          
		"-map", "0", 
		"-f", "segment", 
		"-segment_time", fmt.Sprintf("%.0f", chunkDuration), 
		"-reset_timestamps", "1", 
		outputPattern,
	)

	if err := splitCmd.Run(); err != nil {
		return nil, err
	}

	baseName := strings.TrimSuffix(outputPattern, "%03d.mp4")
	files, _ := filepath.Glob(baseName + "*")
	return files, nil
}

// ==========================================
// 🎯 5. COMMAND HANDLERS & MENUS
// ==========================================
// ==========================================
// 📺 YOUTUBE SEARCH MENU (.yts)
// ==========================================
func handleYTS(client *whatsmeow.Client, v *events.Message, query string) {
	if query == "" { return }
	react(client, v.Info.Chat, v.Info.ID, "🔍")

	// CombinedOutput یوز کر رہے ہیں تاکہ ایرر بھی پکڑ سکیں
	cmd := exec.Command("yt-dlp", "ytsearch5:"+query, "--flat-playlist", "--print", "%(title)s|||%(id)s")
	out, err := cmd.CombinedOutput()
	
	if err != nil { 
		// اگر سرچ فیل ہو تو ایرر منہ پر مارو
		errMsg := strings.TrimSpace(string(out))
		if len(errMsg) > 500 { errMsg = errMsg[:500] + "..." } // زیادہ لمبا ایرر ہو تو کاٹ دو
		
		fmt.Printf("❌ [YTS ERROR]: %v\nOutput: %s\n", err, errMsg)
		replyMessage(client, v, fmt.Sprintf("❌ *YouTube Search Error:*\n```\n%s\n```", errMsg))
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return 
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var results []SearchResult
	
	menuText := "❖ ── ✦ 𝗬𝗢𝗨𝗧𝗨𝗕𝗘 𝗦𝗘𝗔𝗥𝗖𝗛 ✦ ── ❖\n\n"
	icons := []string{"❶", "❷", "❸", "❹", "❺"}
	count := 0
	for _, line := range lines {
		parts := strings.Split(line, "|||")
		if len(parts) < 2 || count >= 5 { continue }
		
		title := strings.TrimSpace(parts[0])
		vidID := strings.TrimSpace(parts[1])
		results = append(results, SearchResult{Title: title, Url: "https://www.youtube.com/watch?v=" + vidID})
		
		menuText += fmt.Sprintf(" %s %s\n\n", icons[count], title)
		count++
	}

	if count == 0 { 
		replyMessage(client, v, "❌ *Error:* No videos found for this search.")
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return 
	}
	menuText += "↬ _Reply with a number (1-5)_"

	msgID := replyMessage(client, v, menuText)
	ytSearchCache[msgID] = MediaSession{Results: results, SenderID: v.Info.Sender.User}
}

// ==========================================
// 🎬 COMMAND: .video (Direct Video Search & Download)
// ==========================================
func handleVideoSearch(client *whatsmeow.Client, v *events.Message, query string) {
	if query == "" { return }
	react(client, v.Info.Chat, v.Info.ID, "🔍")

	// پرانے بوٹ والی پرفیکٹ لاجک (--get-id)
	cmd := exec.Command("yt-dlp", "ytsearch1:"+query, "--get-id")
	out, err := cmd.CombinedOutput()
	
	if err != nil {
		errMsg := strings.TrimSpace(string(out))
		if len(errMsg) > 500 { errMsg = errMsg[:500] + "..." }
		
		fmt.Printf("❌ [VIDEO SEARCH ERROR]: %v\nOutput: %s\n", err, errMsg)
		replyMessage(client, v, fmt.Sprintf("❌ *Search Error:*\n```\n%s\n```", errMsg))
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return
	}

	vidID := strings.TrimSpace(string(out))
	if vidID == "" {
		replyMessage(client, v, "❌ *Error:* No video found for this search.")
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return
	}

	ytUrl := "https://www.youtube.com/watch?v=" + vidID
	go downloadViaAPI(client, v, ytUrl, "360p", false)
}

// ==========================================
// 🎵 COMMAND: .play (Direct Audio Search & Download)
// ==========================================
func handlePlayMusic(client *whatsmeow.Client, v *events.Message, query string) {
	if query == "" { return }
	react(client, v.Info.Chat, v.Info.ID, "🔍")

	// پرانے بوٹ والی پرفیکٹ لاجک (--get-id)
	cmd := exec.Command("yt-dlp", "ytsearch1:"+query, "--get-id")
	out, err := cmd.CombinedOutput()
	
	if err != nil {
		errMsg := strings.TrimSpace(string(out))
		if len(errMsg) > 500 { errMsg = errMsg[:500] + "..." }
		
		fmt.Printf("❌ [PLAY SEARCH ERROR]: %v\nOutput: %s\n", err, errMsg)
		replyMessage(client, v, fmt.Sprintf("❌ *Search Error:*\n```\n%s\n```", errMsg))
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return
	}

	vidID := strings.TrimSpace(string(out))
	if vidID == "" {
		replyMessage(client, v, "❌ *Error:* No audio found for this search.")
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return
	}

	ytUrl := "https://www.youtube.com/watch?v=" + vidID
	go downloadViaAPI(client, v, ytUrl, "mp3", true)
}


func handleYTQualityMenu(client *whatsmeow.Client, v *events.Message, ytUrl string) {
	menu := `❖ ── ✦ 𝗤𝗨𝗔𝗟𝗜𝗧𝗬 ✦ ── ❖

 ❶  144p  (Low)
 ❷  360p  (Normal)
 ❸  720p  (HD)
 ❹  1080p (FHD)
 ❺  MP3   (Audio)

↬ _Reply with a number_`

	msgID := replyMessage(client, v, menu)
	ytQualityCache[msgID] = YTDownloadState{Url: ytUrl, SenderID: v.Info.Sender.User}
}

// ==========================================
// 🎵 TIKTOK SEARCH MENU (Fixed JSON Parsing + New UI)
// ==========================================
func handleTTSearch(client *whatsmeow.Client, v *events.Message, query string) {
	if query == "" { return }
	react(client, v.Info.Chat, v.Info.ID, "🔍")

	// Python Script چلائیں
	cmd := exec.Command("python3", "tiktok_search.py", query)
	
	// CombinedOutput یوز کریں تاکہ اگر کوئی ایرر آئے تو ہم لاگز دیکھ سکیں
	out, err := cmd.CombinedOutput()
	if err != nil { 
		fmt.Printf("❌ [GO] Execution Error: %v\n", err)
		react(client, v.Info.Chat, v.Info.ID, "❌") 
		return 
	}

	// 🔥 CRITICAL FIX: صرف آخری لائن نکالیں (جیسا پرانے بوٹ میں تھا)
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	lastLine := lines[len(lines)-1]

	var results []SearchResult
	
	// پہلے آخری لائن کو Parse کرنے کی کوشش کریں
	if jsonErr := json.Unmarshal([]byte(lastLine), &results); jsonErr != nil || len(results) == 0 {
		// اگر آخری لائن کام نہ کرے تو پورے آؤٹ پٹ کو ٹرائی کریں (Fallback)
		if err2 := json.Unmarshal(out, &results); err2 != nil || len(results) == 0 {
			fmt.Printf("❌ [GO] JSON Parse Error: %v\nRaw Output: %s\n", jsonErr, string(out))
			react(client, v.Info.Chat, v.Info.ID, "❌")
			return
		}
	}

	// NEW ELEGANT DESIGN
	menuText := "❖ ── ✦ 𝗧𝗜𝗞𝗧𝗢𝗞 𝗦𝗘𝗔𝗥𝗖𝗛 ✦ ── ❖\n\n"
	icons := []string{"❶", "❷", "❸", "❹", "❺", "❻", "❼", "❽", "❾", "❿"}
	
	limit := len(results)
	if limit > 5 { limit = 5 } // مینیو کو کلین رکھنے کے لیے 5 رزلٹس

	for i := 0; i < limit; i++ {
		menuText += fmt.Sprintf(" %s %s\n\n", icons[i], results[i].Title)
	}
	menuText += "↬ _Reply with a number_"

	// مینیو سینڈ کریں
	msgID := replyMessage(client, v, menuText)

	// Cache میں سیو کریں (MediaSession یوز کر رہے ہیں تاکہ HandleMenuReplies کام کرے)
	if msgID != "" {
		ttSearchCache[msgID] = MediaSession{Results: results[:limit], SenderID: v.Info.Sender.User}
	}
}


func HandleMenuReplies(client *whatsmeow.Client, v *events.Message, bodyClean string, qID string) bool {
    if HandleAIChatReply(client, v, bodyClean, qID) {
		return true
	}
	
	if session, ok := ytSearchCache[qID]; ok {
		if strings.Contains(v.Info.Sender.User, session.SenderID) {
			delete(ytSearchCache, qID)
			if idx, err := strconv.Atoi(bodyClean); err == nil && idx > 0 && idx <= len(session.Results) {
				handleYTQualityMenu(client, v, session.Results[idx-1].Url)
			}
			return true
		}
	}

	if state, ok := ytQualityCache[qID]; ok {
		if strings.Contains(v.Info.Sender.User, state.SenderID) {
			delete(ytQualityCache, qID)
			resMap := map[string]string{"1": "144p", "2": "360p", "3": "720p", "4": "1080p", "5": "mp3"}
			resConfig, exists := resMap[bodyClean]
			if !exists { resConfig = "360p" }
			go downloadViaAPI(client, v, state.Url, resConfig, resConfig == "mp3")
			return true
		}
	}

	if session, ok := ttSearchCache[qID]; ok {
		if strings.Contains(v.Info.Sender.User, session.SenderID) {
			delete(ttSearchCache, qID)
			if idx, err := strconv.Atoi(bodyClean); err == nil && idx > 0 && idx <= len(session.Results) {
				go downloadViaAPI(client, v, session.Results[idx-1].Url, "mp4", false)
			}
			return true
		}
	}
	return false
}

func handleYTDirect(client *whatsmeow.Client, v *events.Message, ytUrl string) {
	if ytUrl == "" { return }
	go downloadViaAPI(client, v, ytUrl, "360p", false)
}

func handleTikTok(client *whatsmeow.Client, v *events.Message, args string) {
	if args == "" { return }
	args = strings.TrimSpace(args)
	mode, isAudio, urlStr := "mp4", false, args
	
	parts := strings.Fields(args)
	if len(parts) > 1 && (strings.ToLower(parts[0]) == "a" || strings.ToLower(parts[0]) == "audio") {
		mode, isAudio, urlStr = "mp3", true, parts[1]
	}
	go downloadViaAPI(client, v, urlStr, mode, isAudio)
}

// 💎 پریمیم کارڈ میکر (ہیلپر)
// ==========================================
// 🌐 UNIVERSAL MEDIA DOWNLOADER (Silent Router)
// ==========================================
func handleUniversalDownload(client *whatsmeow.Client, v *events.Message, url string, cmd string) {
	if url == "" {
		replyMessage(client, v, "❌ *Error:* Please provide a valid link.")
		return
	}

	// 🛠️ FIX: اگر اسنیپ چیٹ کا شارٹ لنک ہے تو اسے Expand کر لیں
	if strings.Contains(url, "snapchat.com/t/") || strings.Contains(url, "pin.it/") {
		url = expandURL(url)
	}

	var emoji, mode string
	mode = "video" // ڈیفالٹ موڈ ویڈیو ہے
    
    // ... باقی سارا آپ کا پرانا کوڈ وہی رہے گا ...


	// کمانڈ کے حساب سے صرف ایموجی اور موڈ سیٹ کریں
	switch cmd {
	case "fb", "facebook":
		emoji = "💙"
	case "ig", "insta", "instagram":
		emoji = "📸"
	case "tw", "x", "twitter":
		emoji = "🐦"
	case "pin", "pinterest":
		emoji = "📌"
	case "snap", "snapchat":
		emoji = "👻"
	case "reddit":
		emoji = "👽"
	case "dm", "dailymotion":
		emoji = "📺"
	case "sc", "soundcloud", "spotify", "apple", "applemusic", "deezer", "tidal", "mixcloud", "napster", "bandcamp":
		// یہ سارے میوزک پلیٹ فارمز ہیں اس لیے ان کا موڈ 'audio' کر دیں
		emoji = "🎵"
		mode = "audio"
	default:
		emoji = "🚀"
	}

	// 1. صرف ری ایکشن دیں (کوئی پریمیم کارڈ یا میسج نہیں جائے گا)
	react(client, v.Info.Chat, v.Info.ID, emoji)

	// 2. ماسٹر ڈاؤنلوڈر کو فائل لانے کے لیے خاموشی سے بھیج دیں
	go downloadAndSend(client, v, url, mode)
}

// ==========================================
// 🔗 URL EXPANDER HELPER (For Snapchat/Pinterest)
// ==========================================
func expandURL(shortURL string) string {
	client := &http.Client{
		Timeout: 15 * time.Second,
	}
	req, err := http.NewRequest("GET", shortURL, nil)
	if err != nil {
		return shortURL
	}
	
	// براؤزر کا روپ دھاریں تاکہ 404 نہ آئے
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	
	resp, err := client.Do(req)
	if err != nil {
		return shortURL
	}
	defer resp.Body.Close()
	
	// یہ فائنل اور اصلی لنک واپس کر دے گا (e.g., snapchat.com/spotlight/...)
	return resp.Request.URL.String()
}
