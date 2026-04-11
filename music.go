package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// ==========================================
// 🎧 THE VIP MUSIC MIXER ENGINE
// ==========================================
func handleMusicMixer(client *whatsmeow.Client, v *events.Message, args string) {
	// 1. چیک کریں کہ کسی میسج کا رپلائی کیا ہے
	contextInfo := v.Message.GetExtendedTextMessage().GetContextInfo()
	if contextInfo == nil || contextInfo.GetQuotedMessage() == nil {
		replyMessage(client, v, "❌ *Error:* Please reply to a voice note with `.music` or `.music [sad/happy/etc]`")
		return
	}

	audioMsg := contextInfo.GetQuotedMessage().GetAudioMessage()
	if audioMsg == nil {
		replyMessage(client, v, "❌ *Error:* This command only works for voice notes.")
		return
	}

	// 2. سرچ کوئری سیٹ کریں (ڈیفالٹ یا یوزر کی دی ہوئی)
	searchQuery := "lofi instrumental no copyright"
	if args != "" {
		searchQuery = args + " instrumental no copyright"
	}

	// ڈاؤنلوڈ اینیمیشن شروع کریں
	react(client, v.Info.Chat, v.Info.ID, "⏳")

	// 3. عارضی فائل نیمز
	timestamp := time.Now().UnixNano()
	voiceFile := fmt.Sprintf("voice_%d.ogg", timestamp)
	musicFile := fmt.Sprintf("music_%d.mp3", timestamp)
	finalFile := fmt.Sprintf("final_%d.ogg", timestamp)

	// فائلیں خود بخود ڈیلیٹ کرنے کے لیے
	defer func() {
		os.Remove(voiceFile)
		os.Remove(musicFile)
		os.Remove(finalFile)
	}()

	// ==========================================
	// 🎙️ STEP A: یوزر کی وائس ڈاؤن لوڈ کریں
	// ==========================================
	audioData, err := client.Download(context.Background(), audioMsg)
	if err != nil {
		replyMessage(client, v, "❌ *Error:* Failed to download your voice note.")
		return
	}
	os.WriteFile(voiceFile, audioData, 0644)

	// ==========================================
	// 🎵 STEP B: یوٹیوب سے میوزک سرچ اور ڈاؤن لوڈ (آپ کی API کے ذریعے)
	// ==========================================
	// YT-DLP سے صرف ID نکالیں
	cmd := exec.Command("yt-dlp", "ytsearch1:"+searchQuery, "--flat-playlist", "--print", "id")
	out, err := cmd.CombinedOutput()
	if err != nil || len(out) == 0 {
		replyMessage(client, v, "❌ *Error:* Failed to find background music.")
		return
	}

	vidID := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	ytUrl := "https://www.youtube.com/watch?v=" + vidID

	// آپ کا کسٹم فنکشن کال کر کے ڈاؤنلوڈ لنک نکالیں
	_, dlLink, err := extractVidsSaveURL(ytUrl, "audio")
	if err != nil || dlLink == "" {
		replyMessage(client, v, "❌ *Error:* Failed to extract music via API.")
		return
	}

	// میوزک فائل ڈاؤن لوڈ کریں
	resp, err := http.Get(dlLink)
	if err != nil {
		replyMessage(client, v, "❌ *Error:* Failed to fetch music file.")
		return
	}
	defer resp.Body.Close()

	mFile, err := os.Create(musicFile)
	if err != nil {
		replyMessage(client, v, "❌ *System Error:* Could not create music file.")
		return
	}
	io.Copy(mFile, resp.Body)
	mFile.Close()

	react(client, v.Info.Chat, v.Info.ID, "🎛️") // مکسنگ کا اشارہ

	// ==========================================
	// 🎚️ STEP C: FFmpeg VIP مکسنگ (Vibrato + Echo)
	// ==========================================
	// filter: آواز میں ہلکی تھرتھراہٹ (vibrato)، گونج (aecho)، اور میوزک کی آواز کم (volume=0.2)
	filter := "[0:a]vibrato=f=4:d=0.2, aecho=0.8:0.88:40:0.3, volume=1.8[v]; [1:a]volume=0.15, lowpass=f=3000[bg]; [v][bg]amix=inputs=2:duration=first"

	mixCmd := exec.Command("ffmpeg", "-y",
		"-i", voiceFile,
		"-i", musicFile,
		"-filter_complex", filter,
		"-c:a", "libopus",
		"-b:a", "64k",
		"-vbr", "on",
		finalFile)

	err = mixCmd.Run()
	if err != nil {
		replyMessage(client, v, "❌ *Processing Error:* Failed to mix audio.")
		return
	}

	// ==========================================
	// 📤 STEP D: فائنل آڈیو کو واٹس ایپ پر بھیجیں
	// ==========================================
	finalData, err := os.ReadFile(finalFile)
	if err != nil {
		replyMessage(client, v, "❌ *Error:* Could not read final file.")
		return
	}

	uploaded, err := client.Upload(context.Background(), finalData, whatsmeow.MediaAudio)
	if err != nil {
		replyMessage(client, v, "❌ *Upload Error:* Failed to upload to WhatsApp.")
		return
	}

	// بطور PTT (وائس نوٹ) ریپلائی کریں
	ptt := true
	client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
		AudioMessage: &waProto.AudioMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String("audio/ogg; codecs=opus"),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(finalData))),
			Seconds:       audioMsg.Seconds,
			PTT:           &ptt,
			ContextInfo: &waProto.ContextInfo{
				StanzaID:      proto.String(v.Info.ID),
				Participant:   proto.String(v.Info.Sender.String()),
				QuotedMessage: v.Message,
			},
		},
	})

	react(client, v.Info.Chat, v.Info.ID, "✅")
}
