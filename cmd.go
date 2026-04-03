package main

import (
	"context"
	"fmt" // 🛠️ FIX: یہ مسنگ تھا جس کی وجہ سے ڈوکر کریش ہوا
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// ==========================================
// 🧠 MAIN HANDLER (Zero-Delay Interceptor)
// ==========================================
func EventHandler(client *whatsmeow.Client, evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		// 1. ٹائم آؤٹ چیک (پرانے میسجز اگنور کریں)
		if time.Since(v.Info.Timestamp) > 60*time.Second {
			return
		}

		// 🔥 اصل گیم چینجر: پوری پروسیسنگ کو Goroutine میں ڈال دیں!
		go processMessageAsync(client, v)
	}
}

// ==========================================
// 🚀 ASYNC COMMAND PROCESSOR
// ==========================================
func processMessageAsync(client *whatsmeow.Client, v *events.Message) {
	// 🛡️ BULLETPROOF RECOVERY: یہ بوٹ کو کبھی کریش نہیں ہونے دے گا
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("⚠️ [CRASH PREVENTED] Error in command by %s: %v\n", v.Info.Sender.User, r)
			react(client, v.Info.Chat, v.Info.ID, "❌")
		}
	}()

	body := ""
	if v.Message.Conversation != nil {
		body = *v.Message.Conversation
	} else if v.Message.ExtendedTextMessage != nil && v.Message.ExtendedTextMessage.Text != nil {
		body = *v.Message.ExtendedTextMessage.Text
	}

	bodyClean := strings.TrimSpace(body)
	if bodyClean == "" {
		return
	}

	// مینو ریپلائی چیک
	extMsg := v.Message.GetExtendedTextMessage()
	if extMsg != nil && extMsg.ContextInfo != nil && extMsg.ContextInfo.StanzaID != nil {
		qID := *extMsg.ContextInfo.StanzaID
		// HandleMenuReplies 'downloader.go' فائل میں موجود ہے
		if HandleMenuReplies(client, v, bodyClean, qID) {
			return 
		}
	}

	prefix := "."
	if !strings.HasPrefix(bodyClean, prefix) {
		return
	}

	msgWithoutPrefix := strings.TrimPrefix(bodyClean, prefix)
	words := strings.Fields(msgWithoutPrefix)
	if len(words) == 0 {
		return
	}

	cmd := strings.ToLower(words[0])
	fullArgs := strings.TrimSpace(strings.Join(words[1:], " "))

	// ==========================================
	// 🎯 COMMAND SWITCH (Clean & Separated)
	// ==========================================
	switch cmd {

	case "menu", "help":
		sendMainMenu(client, v)

	case "play", "song":
		go handlePlayMusic(client, v, fullArgs)

	case "yt", "youtube":
		go handleYTDirect(client, v, fullArgs)

	case "yts":
		go handleYTS(client, v, fullArgs)

	case "tt", "tiktok":
		go handleTikTok(client, v, fullArgs)

	case "tts":
		go handleTTSearch(client, v, fullArgs)

	case "video":
		go handleVideoSearch(client, v, fullArgs)
	}
}

// ==========================================
// ✨ VIP MENU DESIGN
// ==========================================
func sendMainMenu(client *whatsmeow.Client, v *events.Message) {
	menu := `❖ ── ✦ 𝗦𝗜𝗟𝗘𝗡𝗧 𝙃𝙖𝙘𝙠𝙚𝙧𝙨 ✦ ── ❖
 
 👤 𝗢𝘄𝗻𝗲𝗿: 𝗦𝗜𝗟𝗘𝗡𝗧 𝙃𝙖𝙘𝙠𝙚𝙧𝙨
 ⚙️ 𝗠𝗼𝗱𝗲: Public

 ╭── ✦ [ 𝗬𝗢𝗨𝗧𝗨𝗕𝗘 𝗠𝗘𝗡𝗨 ] ✦ ──╮
 │ 
 │ ➭ *.play / .song* [name]
 │    _Direct HQ Audio Download_
 │
 │ ➭ *.video* [name]
 │    _Direct HD Video Download_
 │
 │ ➭ *.yt* [youtube link]
 │    _Download YT Video/Audio_
 │
 │ ➭ *.yts* [search query]
 │    _Search YouTube Videos_
 │
 ╰──────────────────────╯

 ╭── ✦ [ 𝗧𝗜𝗞𝗧𝗢𝗞 𝗠𝗘𝗡𝗨 ] ✦ ──╮
 │ 
 │ ➭ *.tt* [tiktok link]
 │    _No-Watermark TT Video_
 │
 │ ➭ *.tt audio* [tiktok link]
 │    _Extract TikTok Sound_
 │
 │ ➭ *.tts* [search query]
 │    _Search TikTok Trends_
 │
 ╰──────────────────────╯

 ╭── ✦ [ 𝗢𝗪𝗡𝗘𝗥 𝗠𝗘𝗡𝗨 ] ✦ ──╮
 │ 
 │ ➭ *.pair* [number]
 │    _Connect New Bot Session_
 │
 │ ➭ *.anticall* [on/off]
 │    _Block & Delete Calls_
 │
 │ ➭ *.antidm* [on/off]
 │    _Block Unsaved Numbers_
 │
 ╰──────────────────────╯

 ⚡ _𝙎𝙞𝙡𝙚𝙣𝙩 𝙃𝙖𝙘𝙠𝙚𝙧𝙨_`

	replyMessage(client, v, menu)
}

// ==========================================
// 🛠️ UTILITIES
// ==========================================
func react(client *whatsmeow.Client, chat types.JID, msgID types.MessageID, emoji string) {
	go client.SendMessage(context.Background(), chat, &waProto.Message{
		ReactionMessage: &waProto.ReactionMessage{
			Key: &waProto.MessageKey{
				RemoteJID: proto.String(chat.String()),
				ID:        proto.String(string(msgID)),
				FromMe:    proto.Bool(false),
			},
			Text:              proto.String(emoji),
			SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
		},
	})
}

func replyMessage(client *whatsmeow.Client, v *events.Message, text string) string {
	resp, err := client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String(text),
			ContextInfo: &waProto.ContextInfo{
				StanzaID:      proto.String(v.Info.ID),
				Participant:   proto.String(v.Info.Sender.String()),
				QuotedMessage: v.Message,
			},
		},
	})
	if err == nil {
		return resp.ID
	}
	return ""
}
