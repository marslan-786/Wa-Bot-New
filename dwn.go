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

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)


// ==========================================
// 🛡️ STATE CACHES (Shared with commands.go)
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
// 🌐 MASTER API DOWNLOADER
// ==========================================
type APIResponse struct {
	Success     bool   `json:"success"`
	Title       string `json:"title"`
	Resolution  string `json:"resolution"`
	DownloadURL string `json:"download_url"`
}

// ==========================================
// 🌐 MASTER API DOWNLOADER (Crash-Proof Edition)
// ==========================================


// واٹس ایپ کی سیف لمٹ: 1.8 GB (بائٹس میں)
// واٹس ایپ کی سیف لمٹ: 1.8 GB (بائٹس میں)
const MaxWhatsAppSize int64 = 1932735283 // 1.8 GB in bytes
const SafeMarginMB = 1800.0


// ==========================================
// 🌐 MASTER API DOWNLOADER (With Disk Streaming & Splitting)
// ==========================================
func downloadViaAPI(client *whatsmeow.Client, v *events.Message, targetUrl, resolution string, isAudio bool) {
	react(client, v.Info.Chat, v.Info.ID, "⬇️")

	httpClient := &http.Client{Timeout: 5 * time.Minute}

	// 1. API Call
	apiUrl := fmt.Sprintf("https://silent-yt-dwn.up.railway.app/api/download?url=%s&resolution=%s", targetUrl, resolution)
	resp, err := httpClient.Get(apiUrl)
	if err != nil { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
	defer resp.Body.Close()

	var apiRes APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiRes); err != nil || !apiRes.Success || apiRes.DownloadURL == "" {
		react(client, v.Info.Chat, v.Info.ID, "❌"); return
	}

	// 2. Download File
	fileResp, err := httpClient.Get(apiRes.DownloadURL)
	if err != nil { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
	defer fileResp.Body.Close()

	// 💾 RAM بچانے کے لیے فائل کو سیدھا ڈسک پر سیو کریں
	ext := ".mp4"
	if isAudio { ext = ".mp3" }
	tempFileName := fmt.Sprintf("./data/temp_%d%s", time.Now().UnixNano(), ext)
	
	outFile, err := os.Create(tempFileName)
	if err != nil { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
	
	_, err = io.Copy(outFile, fileResp.Body)
	outFile.Close()
	if err != nil { os.Remove(tempFileName); react(client, v.Info.Chat, v.Info.ID, "❌"); return }

	// صفائی کا خاص خیال: فنکشن ختم ہونے پر اصل فائل ڈیلیٹ ہو جائے
	defer os.Remove(tempFileName)

	// 3. سائز چیک اور اسپلٹنگ (Size Check & Splitting)
	fileInfo, err := os.Stat(tempFileName)
	if err != nil { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
	
	fileSize := fileInfo.Size()
	var filesToSend []string

	if fileSize > MaxWhatsAppSize && !isAudio {
		react(client, v.Info.Chat, v.Info.ID, "✂️") // اسپلٹنگ کا ری ایکشن
		
		parts, err := splitVideoSmart(tempFileName, SafeMarginMB)
		if err != nil || len(parts) == 0 {
			// اگر کاٹنے میں کوئی مسئلہ آیا تو اوریجنل فائل ہی بھیجنے کی کوشش کریں گے
			filesToSend = append(filesToSend, tempFileName)
		} else {
			filesToSend = parts
		}
	} else {
		filesToSend = append(filesToSend, tempFileName)
	}

	react(client, v.Info.Chat, v.Info.ID, "📤")

	// 4. اپلوڈ اور سینڈ کریں
	for i, filePath := range filesToSend {
		uploadAndSendFile(client, v, filePath, apiRes.Title, isAudio, i+1, len(filesToSend))
		
		// اگر کاٹے گئے ٹکڑے تھے، تو بھیجنے کے بعد انہیں ڈیلیٹ کر دیں
		if filePath != tempFileName {
			os.Remove(filePath)
		}
	}

	react(client, v.Info.Chat, v.Info.ID, "✅")
}

// 📤 Helper Function for Uploading
func uploadAndSendFile(client *whatsmeow.Client, v *events.Message, filePath string, title string, isAudio bool, partNum int, totalParts int) {
	fileData, err := os.ReadFile(filePath)
	if err != nil { return }

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var mType whatsmeow.MediaType
	var mime string
	if isAudio { 
		mType = whatsmeow.MediaAudio; mime = "audio/mpeg" 
	} else { 
		// اگر فائل بہت بڑی ہو تو اسے Document کے طور پر بھیجیں تاکہ واٹس ایپ ریجیکٹ نہ کرے
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
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			Mimetype: proto.String(mime), FileLength: proto.Uint64(uint64(len(fileData))), PTT: proto.Bool(false),
		}
	} else if mType == whatsmeow.MediaDocument {
		msg.DocumentMessage = &waProto.DocumentMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			Mimetype: proto.String(mime), Title: proto.String(finalTitle), FileName: proto.String(finalTitle + ".mp4"),
			FileLength: proto.Uint64(uint64(len(fileData))), Caption: proto.String("✅ " + finalTitle),
		}
	} else {
		msg.VideoMessage = &waProto.VideoMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			Mimetype: proto.String(mime), Caption: proto.String("✅ " + finalTitle), FileLength: proto.Uint64(uint64(len(fileData))),
		}
	}

	client.SendMessage(ctx, v.Info.Chat, &msg)
}

// ==========================================
// ✂️ SMART SPLIT FUNCTION (FFMPEG)
// ==========================================
func splitVideoSmart(inputPath string, targetMB float64) ([]string, error) {
	// 1. ویڈیو کی کل Duration (Seconds) حاصل کریں
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", inputPath)
	out, err := cmd.Output()
	if err != nil { return nil, err }
	
	durationSec, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	
	// 2. فائل کا سائز ایم بی میں نکالیں
	info, _ := os.Stat(inputPath)
	totalSizeMB := float64(info.Size()) / (1024 * 1024)
	
	// 3. کیلکولیشن: (TargetMB / TotalMB) * TotalDuration
	chunkDuration := (targetMB / totalSizeMB) * durationSec
	
	// سیف مارجن کے لیے 5% کم رکھیں
	chunkDuration = chunkDuration * 0.95

	fmt.Printf("✂️ Splitting video. Total: %.2f MB, Target: %.2f MB, Chunk Time: %.0f sec\n", totalSizeMB, targetMB, chunkDuration)

	outputPattern := strings.Replace(inputPath, ".mp4", "_part%03d.mp4", 1)
	
	// 4. FFmpeg سے ویڈیو کو بغیر کوالٹی گرائے کاٹیں
	splitCmd := exec.Command("ffmpeg", 
		"-i", inputPath, 
		"-c", "copy",          // Re-encode نہیں کریں گے تاکہ فوراً کام ہو جائے
		"-map", "0", 
		"-f", "segment", 
		"-segment_time", fmt.Sprintf("%.0f", chunkDuration), 
		"-reset_timestamps", "1", 
		outputPattern,
	)

	if err := splitCmd.Run(); err != nil {
		return nil, err
	}

	// 5. تمام کاٹے گئے ٹکڑوں کی لسٹ بنا کر واپس بھیجیں
	baseName := strings.TrimSuffix(outputPattern, "%03d.mp4")
	files, _ := filepath.Glob(baseName + "*")
	return files, nil
}


// ==========================================
// 📺 YOUTUBE SEARCH MENU (NEW UI)
// ==========================================
func handleYTS(client *whatsmeow.Client, v *events.Message, query string) {
	if query == "" { return }
	react(client, v.Info.Chat, v.Info.ID, "🔍")

	cmd := exec.Command("yt-dlp", "ytsearch5:"+query, "--flat-playlist", "--print", "%(title)s|||%(id)s")
	out, err := cmd.Output()
	if err != nil { react(client, v.Info.Chat, v.Info.ID, "❌"); return }

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var results []SearchResult
	
	// NEW ELEGANT DESIGN
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

	if count == 0 { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
	menuText += "↬ _Reply with a number (1-5)_"

	msgID := replyMessage(client, v, menuText)
	ytSearchCache[msgID] = MediaSession{Results: results, SenderID: v.Info.Sender.User}
}

// ==========================================
// 🎯 YOUTUBE QUALITY MENU (NEW UI)
// ==========================================
func handleYTQualityMenu(client *whatsmeow.Client, v *events.Message, ytUrl string) {
	// NEW ELEGANT DESIGN
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
// 🎵 TIKTOK SEARCH MENU (NEW UI)
// ==========================================
func handleTTSearch(client *whatsmeow.Client, v *events.Message, query string) {
	if query == "" { return }
	react(client, v.Info.Chat, v.Info.ID, "🔍")

	cmd := exec.Command("python3", "tiktok_search.py", query)
	out, err := cmd.Output()
	if err != nil { react(client, v.Info.Chat, v.Info.ID, "❌"); return }

	var results []SearchResult
	if err := json.Unmarshal(out, &results); err != nil || len(results) == 0 {
		react(client, v.Info.Chat, v.Info.ID, "❌"); return
	}

	menuText := "❖ ── ✦ 𝗧𝗜𝗞𝗧𝗢𝗞 𝗦𝗘𝗔𝗥𝗖𝗛 ✦ ── ❖\n\n"
	icons := []string{"❶", "❷", "❸", "❹", "❺", "❻", "❼", "❽", "❾", "❿"}
	
	limit := len(results)
	if limit > 5 { limit = 5 } // Showing top 5 to keep it clean

	for i := 0; i < limit; i++ {
		menuText += fmt.Sprintf(" %s %s\n\n", icons[i], results[i].Title)
	}
	menuText += "↬ _Reply with a number_"

	msgID := replyMessage(client, v, menuText)
	ttSearchCache[msgID] = MediaSession{Results: results[:limit], SenderID: v.Info.Sender.User}
}

// ==========================================
// 🔄 MENU REPLY INTERCEPTOR
// ==========================================
func HandleMenuReplies(client *whatsmeow.Client, v *events.Message, bodyClean string, qID string) bool {
	
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

// ==========================================
// 🚀 DIRECT COMMAND LOGIC (.play, .yt, .tt)
// ==========================================
func handlePlayMusic(client *whatsmeow.Client, v *events.Message, query string) {
	if query == "" { return }
	react(client, v.Info.Chat, v.Info.ID, "🔍")
	cmd := exec.Command("yt-dlp", "ytsearch1:"+query, "--flat-playlist", "--print", "%(id)s")
	out, err := cmd.Output()
	if err != nil || string(out) == "" { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
	go downloadViaAPI(client, v, "https://www.youtube.com/watch?v="+strings.TrimSpace(string(out)), "mp3", true)
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

// ==========================================
// 🎬 COMMAND: .video (Direct Video Search & 360p Download)
// ==========================================
func handleVideoSearch(client *whatsmeow.Client, v *events.Message, query string) {
	if query == "" { 
		return 
	}
	
	// 🔍 Reaction: Searching
	react(client, v.Info.Chat, v.Info.ID, "🔍")

	// Fast search using yt-dlp to get ID only (Sirf 1 result layega)
	cmd := exec.Command("yt-dlp", "ytsearch1:"+query, "--flat-playlist", "--print", "%(id)s")
	out, err := cmd.Output()
	
	// اگر کوئی ایرر آئے یا رزلٹ خالی ہو
	if err != nil || len(out) == 0 {
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return
	}

	vidID := strings.TrimSpace(string(out))
	if vidID == "" {
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return
	}

	// یوٹیوب کا مکمل لنک بنائیں
	ytUrl := "https://www.youtube.com/watch?v=" + vidID
	
	// ڈائریکٹ API کو ہٹ کریں: resolution = "360p", isAudio = false
	go downloadViaAPI(client, v, ytUrl, "360p", false)
}

