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
		react(client, v.Info.Chat, v.Info.ID, "📂")
		sendMainMenu(client, v)

	case "play", "song":
		react(client, v.Info.Chat, v.Info.ID, "🎵")
		go handlePlayMusic(client, v, fullArgs)

	case "yt", "youtube":
		react(client, v.Info.Chat, v.Info.ID, "🎬")
		go handleYTDirect(client, v, fullArgs)

	case "yts":
		react(client, v.Info.Chat, v.Info.ID, "🔍")
		go handleYTS(client, v, fullArgs)

	case "tt", "tiktok":
		react(client, v.Info.Chat, v.Info.ID, "📱")
		go handleTikTok(client, v, fullArgs)

	case "tts":
		react(client, v.Info.Chat, v.Info.ID, "🔍")
		go handleTTSearch(client, v, fullArgs)

	case "video":
		react(client, v.Info.Chat, v.Info.ID, "📽️")
		go handleVideoSearch(client, v, fullArgs)
	case "fb", "facebook", "ig", "insta", "instagram", "tw", "x", "twitter", "pin", "pinterest", "threads", "snap", "snapchat", "reddit", "dm", "dailymotion", "vimeo", "rumble", "bilibili", "douyin", "kwai", "bitchute", "sc", "soundcloud", "spotify", "apple", "applemusic", "deezer", "tidal", "mixcloud", "napster", "bandcamp", "imgur", "giphy", "flickr", "9gag", "ifunny":
	    react(client, v.Info.Chat, v.Info.ID, "🪩")
		go handleUniversalDownload(client, v, fullArgs, cmd)
		
		// 🔥 THE AI MASTERMINDS
	case "ai", "gpt", "chatgpt", "gemini", "claude", "llama", "groq", "bot", "ask":
	    react(client, v.Info.Chat, v.Info.ID, "🧠")
		go handleAICommand(client, v, fullArgs, cmd)
		
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

 ╭── ✦ [ 𝗨𝗡𝗜𝗩𝗘𝗥𝗦𝗔𝗟 𝗠𝗘𝗗𝗜𝗔 ] ✦ ──╮
 │ 
 │ ➭ *.fb / .facebook* [link]
 │    _FB High-Quality Videos_
 │
 │ ➭ *.ig / .insta* [link]
 │    _Instagram Reels/IGTV_
 │
 │ ➭ *.tw / .x* [link]
 │    _X/Twitter Media Extract_
 │
 │ ➭ *.snap / .snapchat* [link]
 │    _Snapchat Spotlights_
 │
 │ ➭ *.threads* [link]
 │    _Threads Video Download_
 │
 │ ➭ *.pin / .pinterest* [link]
 │    _Pinterest Video/Images_
 │
 │ ➭ *.reddit* [link]
 │    _Reddit Videos & GIFs_
 │
 │ ➭ *.imgur / .giphy* [link]
 │    _Download Gifs & Assets_
 │
 ╰──────────────────────╯

 ╭── ✦ [ 𝗔𝗨𝗗𝗜𝗢 & 𝗦𝗧𝗥𝗘𝗔𝗠𝗦 ] ✦ ──╮
 │ 
 │ ➭ *.sc / .soundcloud* [link]
 │    _SoundCloud Audio Rips_
 │
 │ ➭ *.spotify* [link]
 │    _Spotify HQ Tracks_
 │
 │ ➭ *.apple* [link]
 │    _Apple Music Audio_
 │
 │ ➭ *.dm / .dailymotion* [link]
 │    _DailyMotion Videos_
 │
 │ ➭ *.vimeo / .rumble* [link]
 │    _Vimeo & Rumble Streams_
 │
 ╰──────────────────────╯

 ╭── ✦ [ 🧠 𝗔𝗜 𝗠𝗔𝗦𝗧𝗘𝗥𝗠𝗜𝗡𝗗𝗦 ] ──╮
 │ 
 │ ➭ *.ai / .ask* [text]
 │    _Faisalabadi Smart AI_
 │
 │ ➭ *.gpt / .chatgpt* [text]
 │    _ChatGPT 4o Persona_
 │
 │ ➭ *.gemini* [text]
 │    _Google Gemini Pro_
 │
 │ ➭ *.claude* [text]
 │    _Anthropic Claude 3_
 │
 │ ➭ *.llama / .groq* [text]
 │    _Meta Llama 3 Fast Engine_
 │
 │ ➭ *.bot* [text]
 │    _Quick & Funny Chatbot_
 │
 │ ➭ *[ Reply to AI's Message ]*
 │    _To continue the conversation_
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


func react(client *whatsmeow.Client, chat types.JID, msgID types.MessageID, emoji string) {
	// 🚀 'go' لگانے سے یہ ری ایکشن الگ تھریڈ میں چلا جائے گا
	go func() {
		// 🛡️ Panic Recovery: اگر ری ایکشن میں نیٹ ورک کا کوئی ایرر آئے تو بوٹ کریش نہ ہو
		defer func() {
			if r := recover(); r != nil {
				// ایرر کو خاموشی سے ہینڈل کر لے گا
			}
		}()

		_, err := client.SendMessage(context.Background(), chat, &waProto.Message{
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
		
		if err != nil {
			fmt.Printf("⚠️ React Error: %v\n", err)
		}
	}()
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
