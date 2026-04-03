package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// ==========================================
// 🛠️ HELPER: MEDIA DOWNLOADER
// ==========================================
func getQuotedMedia(client *whatsmeow.Client, v *events.Message) ([]byte, string, string) {
	extMsg := v.Message.GetExtendedTextMessage()
	if extMsg == nil || extMsg.ContextInfo == nil || extMsg.ContextInfo.QuotedMessage == nil {
		// چیک کریں کہ کیا میسج کے اندر ڈائریکٹ میڈیا ہے
		if img := v.Message.GetImageMessage(); img != nil {
			data, _ := client.Download(context.Background(), img)
			return data, "image", ".jpg"
		} else if vid := v.Message.GetVideoMessage(); vid != nil {
			data, _ := client.Download(context.Background(), vid)
			return data, "video", ".mp4"
		}
		return nil, "", ""
	}

	quoted := extMsg.ContextInfo.QuotedMessage
	if img := quoted.GetImageMessage(); img != nil {
		data, _ := client.Download(context.Background(), img)
		return data, "image", ".jpg"
	} else if vid := quoted.GetVideoMessage(); vid != nil {
		data, _ := client.Download(context.Background(), vid)
		return data, "video", ".mp4"
	} else if stk := quoted.GetStickerMessage(); stk != nil {
		data, _ := client.Download(context.Background(), stk)
		if stk.GetIsAnimated() { return data, "video", ".webp" }
		return data, "image", ".webp"
	}

	return nil, "", ""
}

// ==========================================
// 🎨 COMMAND: .sticker / .s
// ==========================================
func handleSticker(client *whatsmeow.Client, v *events.Message) {
	data, mediaType, ext := getQuotedMedia(client, v)
	if data == nil {
		replyMessage(client, v, "❌ Please reply to an Image or Video to make a sticker.")
		return
	}

	react(client, v.Info.Chat, v.Info.ID, "⏳")

	tempIn := fmt.Sprintf("./data/temp_in_%d%s", time.Now().UnixNano(), ext)
	tempOut := fmt.Sprintf("./data/temp_out_%d.webp", time.Now().UnixNano())
	os.WriteFile(tempIn, data, 0644)
	defer os.Remove(tempIn)
	defer os.Remove(tempOut)

	var cmd *exec.Cmd
	if mediaType == "image" {
		cmd = exec.Command("ffmpeg", "-i", tempIn, "-vcodec", "libwebp", "-vf", "scale='min(320,iw)':min'(320,ih)':force_original_aspect_ratio=decrease,fps=15, pad=320:320:-1:-1:color=white@0.0, format=rgba", "-lossless", "0", "-compression_level", "4", "-q:v", "50", "-loop", "0", "-preset", "default", "-an", "-vsync", "0", tempOut)
	} else {
		cmd = exec.Command("ffmpeg", "-i", tempIn, "-vcodec", "libwebp", "-vf", "scale='min(320,iw)':min'(320,ih)':force_original_aspect_ratio=decrease,fps=15, pad=320:320:-1:-1:color=white@0.0, format=rgba", "-lossless", "0", "-compression_level", "4", "-q:v", "50", "-loop", "0", "-preset", "default", "-an", "-vsync", "0", "-t", "00:00:10", tempOut)
	}

	if err := cmd.Run(); err != nil {
		replyMessage(client, v, "❌ Failed to create sticker.")
		return
	}

	stkData, _ := os.ReadFile(tempOut)
	up, _ := client.Upload(context.Background(), stkData, whatsmeow.MediaImage)

	client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
		StickerMessage: &waProto.StickerMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, Mimetype: proto.String("image/webp"),
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
			FileLength: proto.Uint64(uint64(len(stkData))),
		},
	})
	react(client, v.Info.Chat, v.Info.ID, "✅")
}

// ==========================================
// 🖼️ COMMAND: .toimg
// ==========================================
func handleToImg(client *whatsmeow.Client, v *events.Message) {
	data, _, ext := getQuotedMedia(client, v)
	if data == nil || ext != ".webp" {
		replyMessage(client, v, "❌ Please reply to a non-animated Sticker.")
		return
	}
	react(client, v.Info.Chat, v.Info.ID, "⏳")

	tempIn := fmt.Sprintf("./data/temp_in_%d.webp", time.Now().UnixNano())
	tempOut := fmt.Sprintf("./data/temp_out_%d.jpg", time.Now().UnixNano())
	os.WriteFile(tempIn, data, 0644)
	defer os.Remove(tempIn)
	defer os.Remove(tempOut)

	exec.Command("ffmpeg", "-i", tempIn, tempOut).Run()

	imgData, _ := os.ReadFile(tempOut)
	up, _ := client.Upload(context.Background(), imgData, whatsmeow.MediaImage)

	client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
		ImageMessage: &waProto.ImageMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, Mimetype: proto.String("image/jpeg"),
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
			FileLength: proto.Uint64(uint64(len(imgData))), Caption: proto.String("🎨 Converted by Silent Nexus"),
		},
	})
	react(client, v.Info.Chat, v.Info.ID, "✅")
}

// ==========================================
// 🎬 COMMAND: .tovideo / .togif
// ==========================================
func handleToVideo(client *whatsmeow.Client, v *events.Message, isGif bool) {
	data, _, ext := getQuotedMedia(client, v)
	if data == nil || ext != ".webp" {
		replyMessage(client, v, "❌ Please reply to an Animated Sticker.")
		return
	}
	react(client, v.Info.Chat, v.Info.ID, "⏳")

	tempIn := fmt.Sprintf("./data/temp_in_%d.webp", time.Now().UnixNano())
	tempOut := fmt.Sprintf("./data/temp_out_%d.mp4", time.Now().UnixNano())
	os.WriteFile(tempIn, data, 0644)
	defer os.Remove(tempIn)
	defer os.Remove(tempOut)

	exec.Command("ffmpeg", "-i", tempIn, "-pix_fmt", "yuv420p", tempOut).Run()

	vidData, _ := os.ReadFile(tempOut)
	up, _ := client.Upload(context.Background(), vidData, whatsmeow.MediaVideo)

	client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
		VideoMessage: &waProto.VideoMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, Mimetype: proto.String("video/mp4"),
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
			FileLength: proto.Uint64(uint64(len(vidData))), GifPlayback: proto.Bool(isGif),
			Caption: proto.String("🎨 Converted by Silent Nexus"),
		},
	})
	react(client, v.Info.Chat, v.Info.ID, "✅")
}

// ==========================================
// 🔗 COMMAND: .tourl (Catbox Uploader)
// ==========================================
func handleToUrl(client *whatsmeow.Client, v *events.Message) {
	data, _, ext := getQuotedMedia(client, v)
	if data == nil {
		replyMessage(client, v, "❌ Please reply to any Image, Video, or Sticker to upload.")
		return
	}
	react(client, v.Info.Chat, v.Info.ID, "⏳")

	tempFile := fmt.Sprintf("./data/upload_%d%s", time.Now().UnixNano(), ext)
	os.WriteFile(tempFile, data, 0644)
	defer os.Remove(tempFile)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("reqtype", "fileupload")
	
	part, _ := writer.CreateFormFile("fileToUpload", filepath.Base(tempFile))
	part.Write(data)
	writer.Close()

	req, _ := http.NewRequest("POST", "https://catbox.moe/user/api.php", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		replyMessage(client, v, "❌ Failed to upload media.")
		return
	}
	defer resp.Body.Close()

	linkData, _ := io.ReadAll(resp.Body)
	replyMessage(client, v, fmt.Sprintf("🌐 *Media Uploaded!*\n\n🔗 *URL:* %s", string(linkData)))
	react(client, v.Info.Chat, v.Info.ID, "✅")
}

// ==========================================
// 🎙️ COMMAND: .toptt (Google TTS Jugaad)
// ==========================================
func handleToPTT(client *whatsmeow.Client, v *events.Message, text string) {
	if text == "" {
		replyMessage(client, v, "❌ Please provide text.\nExample: `.toptt Hello Arslan bhai kaise ho`")
		return
	}
	react(client, v.Info.Chat, v.Info.ID, "🎙️")

	// 1. Google Translate API سے آڈیو ڈاونلوڈ کریں
	ttsURL := fmt.Sprintf("https://translate.google.com/translate_tts?ie=UTF-8&tl=ur&client=tw-ob&q=%s", url.QueryEscape(text))
	
	resp, err := http.Get(ttsURL)
	if err != nil || resp.StatusCode != 200 {
		replyMessage(client, v, "❌ Failed to generate audio. Text might be too long.")
		return
	}
	defer resp.Body.Close()

	mp3Data, _ := io.ReadAll(resp.Body)
	tempIn := fmt.Sprintf("./data/tts_in_%d.mp3", time.Now().UnixNano())
	tempOut := fmt.Sprintf("./data/tts_out_%d.ogg", time.Now().UnixNano())
	
	os.WriteFile(tempIn, mp3Data, 0644)
	defer os.Remove(tempIn)
	defer os.Remove(tempOut)

	// 2. FFmpeg کے ذریعے Opus/OGG (WhatsApp PTT فارمیٹ) میں کنورٹ کریں
	exec.Command("ffmpeg", "-i", tempIn, "-c:a", "libopus", "-b:a", "32k", "-vbr", "on", "-compression_level", "10", "-frame_duration", "20", "-application", "voip", tempOut).Run()

	oggData, _ := os.ReadFile(tempOut)
	up, _ := client.Upload(context.Background(), oggData, whatsmeow.MediaAudio)

	client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
		AudioMessage: &waProto.AudioMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, Mimetype: proto.String("audio/ogg; codecs=opus"),
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
			FileLength: proto.Uint64(uint64(len(oggData))), PTT: proto.Bool(true), // 👈 یہ اسے Voice Note بنا دے گا
		},
	})
}

// ==========================================
// 🔠 COMMAND: .fancy (Multi-Font Generator)
// ==========================================
func handleFancy(client *whatsmeow.Client, v *events.Message, args string) {
	if args == "" {
		replyMessage(client, v, "❌ Please provide text.\nExample: `.fancy Silent Hackers`")
		return
	}

	react(client, v.Info.Chat, v.Info.ID, "✨")

	// فونٹس کی میپنگ (یہاں ہم نے 12 سب سے زبردست ڈیزائن رکھے ہیں، جنہیں 50 تک بڑھایا جا سکتا ہے)
	fonts := []func(string) string{
		func(s string) string { return mapChars(s, "𝗮𝗯𝗰𝗱𝗲𝗳𝗴𝗵𝗶𝗷𝗸𝗹𝗺𝗻𝗼𝗽𝗾𝗿𝘀𝘁𝘂𝘃𝘄𝘅𝘆𝘇𝗔𝗕𝗖𝗗𝗘𝗙𝗚𝗛𝗜𝗝𝗞𝗟𝗠𝗡𝗢𝗣𝗤𝗥𝗦𝗧𝗨𝗩𝗪𝗫𝗬𝗭") },
		func(s string) string { return mapChars(s, "𝘢𝘣𝘤𝘥𝘦𝘧𝘨𝘩𝘪𝘫𝘬𝘭𝘮𝘯𝘰𝘱𝘲𝘳𝘴𝘵𝘶𝘷𝘸𝘹𝘺𝘻𝘈𝘉𝘊𝘋𝘌𝘍𝘎𝘏𝘐𝘑𝘒𝘓𝘔𝘕𝘖𝘗𝘘𝘙𝘚𝘛𝘜𝘝𝘞𝘟𝘠𝘡") },
		func(s string) string { return mapChars(s, "𝙖𝙗𝙘𝙙𝙚𝙛𝙜𝙝𝙞𝙟𝙠𝙡𝙢𝙣𝙤𝙥𝙦𝙧𝙨𝙩𝙪𝙫𝙬𝙭𝙮𝙯𝘼𝘽𝘾𝘿𝙀𝙁𝙂𝙃𝙄𝙅𝙆𝙇𝙈𝙉𝙊𝙋𝙌𝙍𝙎𝙏𝙐𝙑𝙒𝙓𝙔𝙕") },
		func(s string) string { return mapChars(s, "𝚊𝚋𝚌𝚍𝚎𝚏𝚐𝚑𝚒𝚓𝚔𝚕𝚖𝚗𝚘𝚙𝚚𝚛𝚜𝚝𝚞𝚟𝚠𝚡𝚢𝚣𝙰𝙱𝙲𝙳𝙴𝙵𝙶𝙷𝙸𝙹𝙺𝙻𝙼𝙽𝙾𝙿𝚀𝚁𝚂𝚃𝚄𝚅𝚆𝚇𝚈𝚉") },
		func(s string) string { return mapChars(s, "𝕒𝕓𝕔𝕕𝕖𝕗𝕘𝕙𝕚𝕛𝕜𝕝𝕞𝕟𝕠𝕡𝕢𝕣𝕤𝕥𝕦𝕧𝕨𝕩𝕪𝕫𝔸𝔹ℂ𝔻𝔼𝔽𝔾ℍ𝕀𝕁𝕂𝕃𝕄ℕ𝕆ℙℚℝ𝕊𝕋𝕌𝕍𝕎𝕏𝕐ℤ") },
		func(s string) string { return mapChars(s, "𝖆𝖇𝖈𝖉𝖊𝖋𝖌𝖍𝖎𝖏𝖐𝖑𝖒𝖓𝖔𝖕𝖖𝖗𝖘𝖙𝖚𝖛𝖜𝖝𝖞𝖟𝕬𝕭𝕮𝕯𝕰𝕱𝕲𝕳𝕴𝕵𝕶𝕷𝕸𝕹𝕺𝕻𝕼𝕽𝕾𝕿𝖀𝖁𝖂𝖃𝖄𝖅") },
		func(s string) string { return mapChars(s, "𝒶𝒷𝒸𝒹𝑒𝒻𝑔𝒽𝒾𝒿𝓀𝓁𝓂𝓃𝑜𝓅𝓆𝓇𝓈𝓉𝓊𝓋𝓌𝓍𝓎𝓏𝒜𝐵𝒞𝒟𝐸𝐹𝒢𝐻𝐼𝒥𝒦𝐿𝑀𝒩𝒪𝒫𝒬𝑅𝒮𝒯𝒰𝒱𝒲𝒳𝒴𝒵") },
		func(s string) string { return mapChars(s, "ⓐⓑⓒⓓⓔⓕⓖⓗⓘⓙⓚⓛⓜⓝⓞⓟⓠⓡⓢⓣⓤⓥⓦⓧⓨⓩⒶⒷⒸⒹⒺⒻⒼⒽⒾⒿⓀⓁⓂⓃⓄⓅⓆⓇⓈⓉⓊⓋⓌⓍⓎⓏ") },
		func(s string) string { return mapChars(s, "𝐚𝐛𝐜𝐝𝐞𝐟𝐠𝐡𝐢𝐣𝐤𝐥𝐦𝐧𝐨𝐩𝐪𝐫𝐬𝐭𝐮𝐯𝐰𝐱𝐲𝐳𝐀𝐁𝐂𝐃𝐄𝐅𝐆𝐇𝐈𝐉𝐊𝐋𝐌𝐍𝐎𝐏𝐐𝐑𝐒𝐓𝐔𝐕𝐖𝐗𝐘𝐙") },
		func(s string) string { return mapChars(s, "ₐbcdₑfgₕᵢⱼₖₗₘₙₒₚqᵣₛₜᵤᵥwₓyzₐBCDₑFGₕᵢⱼₖₗₘₙₒₚQᵣₛₜᵤᵥWₓYZ") },
	}

	result := "❖ ── ✦ 𝗙𝗔𝗡𝗖𝗬 𝗧𝗘𝗫𝗧 ✦ ── ❖\n\n"
	for i, fn := range fonts {
		result += fmt.Sprintf(" %d️⃣ %s\n\n", i+1, fn(args))
	}
	result += "↬ _Silent Nexus Engine_"

	replyMessage(client, v, result)
}

func mapChars(input string, charset string) string {
	normal := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	// چونکہ کچھ یونیکوڈ کریکٹرز 2 یا 4 بائٹس کے ہوتے ہیں، ہم runes کا استعمال کریں گے
	normalRunes := []rune(normal)
	charsetRunes := []rune(charset)
	
	output := ""
	for _, char := range input {
		found := false
		for i, nChar := range normalRunes {
			if char == nChar {
				output += string(charsetRunes[i])
				found = true
				break
			}
		}
		if !found { output += string(char) }
	}
	return output
}
