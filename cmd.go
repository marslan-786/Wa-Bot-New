package main

import (
	"context"
	"fmt"
	"os"
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
// cmd.go میں processMessageAsync کے اندر تبدیلیاں:
func processMessageAsync(client *whatsmeow.Client, v *events.Message) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("⚠️ [CRASH PREVENTED]: %v\n", r)
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
	if bodyClean == "" { return }

	// 🔥 1. اس سیشن کی سیٹنگز لائیں
	settings := getBotSettings(client.Store.ID.User)

	// 🔥 2. موڈ کے حساب سے فلٹر کریں
	isGroup := strings.Contains(v.Info.Chat.String(), "@g.us")
	isOwner := v.Info.IsFromMe // جو نمبر بوٹ چلا رہا ہے وہ خود اونر ہے

	if !isOwner { // اونر پر موڈ کی پابندی نہیں ہوتی
		if settings.Mode == "private" && isGroup {
			return // گروپس میں بلاک
		}
		if settings.Mode == "admin" && isGroup {
			groupInfo, err := client.GetGroupInfo(context.Background(), v.Info.Chat)
			if err != nil { return }
			isAdmin := false
			for _, participant := range groupInfo.Participants {
				if participant.JID.User == v.Info.Sender.User && (participant.IsAdmin || participant.IsSuperAdmin) {
					isAdmin = true
					break
				}
			}
			if !isAdmin { return } // اگر ایڈمن نہیں تو اگنور کرو
		}
	}

	// مینو ریپلائی چیک (آپ کا پرانا کوڈ)
	extMsg := v.Message.GetExtendedTextMessage()
	if extMsg != nil && extMsg.ContextInfo != nil && extMsg.ContextInfo.StanzaID != nil {
		qID := *extMsg.ContextInfo.StanzaID
		if HandleMenuReplies(client, v, bodyClean, qID) { return }
	}

	// 🔥 3. ڈائنامک پریفکس چیک کریں
	if !strings.HasPrefix(bodyClean, settings.Prefix) {
		return
	}

	msgWithoutPrefix := strings.TrimPrefix(bodyClean, settings.Prefix)
	words := strings.Fields(msgWithoutPrefix)
	if len(words) == 0 { return }

	cmd := strings.ToLower(words[0])
	fullArgs := strings.TrimSpace(strings.Join(words[1:], " "))

	switch cmd {
    
	case "setprefix":
		if !isOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleSetPrefix(client, v, fullArgs)

	case "mode":
		if !isOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleMode(client, v, fullArgs)

	case "menu", "help":
		react(client, v.Info.Chat, v.Info.ID, "📂")
		go sendMainMenu(client, v, settings)

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
// ✨ VIP MENU DESIGN (Image + Status Broadcast + Stylish Footer)
func sendMainMenu(client *whatsmeow.Client, v *events.Message, settings BotSettings) {
	// اپ ٹائم حاصل کریں
	uptimeStr := getUptimeString(settings.UptimeStart)

	menu := fmt.Sprintf(`❖ ── ✦ 𝗦𝗜𝗟𝗘𝗡𝗧 𝙃𝙖𝙘𝙠𝙚𝙧𝙨 ✦ ── ❖
 
 👤 𝗢𝘄𝗻𝗲𝗿: 𝗦𝗜𝗟𝗘𝗡𝗧 𝙃𝙖𝙘𝙠𝙚𝙧𝙨
 ⚙️ 𝗠𝗼𝗱𝗲: %s
 ⏱️ 𝗨𝗽𝘁𝗶𝗺𝗲: %s
 ⚡ 𝗣𝗿𝗲𝗳𝗶𝘅: [ %s ]

 ╭── ✦ [ 𝗬𝗢𝗨𝗧𝗨𝗕𝗘 𝗠𝗘𝗡𝗨 ] ✦ ──╮
 │ 
 │ ➭ *%splay / %ssong* [name]
 │    _Direct HQ Audio Download_
 │
 │ ➭ *%svideo* [name]
 │    _Direct HD Video Download_
 │
 │ ➭ *%syt* [link]
 │    _Download YT Video/Audio_
 │
 │ ➭ *%syts* [query]
 │    _Search YouTube Videos_
 │
 ╰──────────────────────╯

 ╭── ✦ [ 𝗧𝗜𝗞𝗧𝗢𝗞 𝗠𝗘𝗡𝗨 ] ✦ ──╮
 │ 
 │ ➭ *%stt* [link]
 │    _No-Watermark TT Video_
 │
 │ ➭ *%stt audio* [link]
 │    _Extract TikTok Sound_
 │
 │ ➭ *%stts* [query]
 │    _Search TikTok Trends_
 │
 ╰──────────────────────╯

 ╭── ✦ [ 𝗨𝗡𝗜𝗩𝗘𝗥𝗦𝗔𝗟 𝗠𝗘𝗗𝗜𝗔 ] ✦ ──╮
 │ 
 │ ➭ *%sfb / %sfacebook* [link]
 │    _FB High-Quality Videos_
 │
 │ ➭ *%sig / %sinsta* [link]
 │    _Instagram Reels/IGTV_
 │
 │ ➭ *%stw / %sx* [link]
 │    _X/Twitter Media Extract_
 │
 │ ➭ *%ssnap* [link]
 │    _Snapchat Spotlights_
 │
 │ ➭ *%sthreads* [link]
 │    _Threads Video Download_
 │
 │ ➭ *%spin* [link]
 │    _Pinterest Video/Images_
 │
 │ ➭ *%sreddit* [link]
 │    _Reddit Videos & GIFs_
 │
 ╰──────────────────────╯

 ╭── ✦ [ 🧠 𝗔𝗜 𝗠𝗔𝗦𝗧𝗘𝗥𝗠𝗜𝗡𝗗𝗦 ] ──╮
 │ 
 │ ➭ *%sai / %sask* [text]
 │    _Faisalabadi Smart AI_
 │
 │ ➭ *%sgpt / %schatgpt* [text]
 │    _ChatGPT 4o Persona_
 │
 │ ➭ *%sgemini* [text]
 │    _Google Gemini Pro_
 │
 │ ➭ *%sclaude* [text]
 │    _Anthropic Claude 3_
 │
 │ ➭ *%sllama / %sgroq* [text]
 │    _Meta Llama 3 Fast Engine_
 │
 ╰──────────────────────╯

 ╭── ✦ [ 𝗢𝗪𝗡𝗘𝗥 𝗠𝗘𝗡𝗨 ] ✦ ──╮
 │ 
 │ ➭ *%ssetprefix* [symbol]
 │    _Change Bot Prefix_
 │
 │ ➭ *%smode* [public/private/admin]
 │    _Change Bot Work Mode_
 │
 │ ➭ *%spair* [number]
 │    _Connect New Bot Session_
 │
 ╰──────────────────────╯

     ⚡ ━━━ ✦ 💖 𝙎𝙞𝙡𝙚𝙣𝙩 𝙃𝙖𝙘𝙠𝙚𝙧𝙨 💖 ✦ ━━━ ⚡`, 
	strings.ToUpper(settings.Mode), uptimeStr, settings.Prefix, 
	settings.Prefix, settings.Prefix, settings.Prefix, settings.Prefix, settings.Prefix, 
	settings.Prefix, settings.Prefix, settings.Prefix, settings.Prefix, settings.Prefix, 
	settings.Prefix, settings.Prefix, settings.Prefix, settings.Prefix, settings.Prefix, 
	settings.Prefix, settings.Prefix, settings.Prefix, settings.Prefix, settings.Prefix, 
	settings.Prefix, settings.Prefix, settings.Prefix, settings.Prefix, settings.Prefix, 
	settings.Prefix, settings.Prefix, settings.Prefix)

	// 🖼️ 1. تصویر لوڈ کریں
	imageData, err := os.ReadFile("pic.png")
	if err != nil {
		fmt.Printf("⚠️ Image error: %v\n", err)
		replyMessage(client, v, menu) // اگر تصویر نہ ملے تو صرف ٹیکسٹ بھیج دے
		return
	}

	// 📤 2. تصویر واٹس ایپ پر اپلوڈ کریں
	resp, err := client.Upload(context.Background(), imageData, whatsmeow.MediaImage)
	if err != nil {
		replyMessage(client, v, menu)
		return
	}

	// 🛡️ 3. ویریفائیڈ اسٹیٹس اور تصویر کے ساتھ میسج بھیجیں
	client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
		ImageMessage: &waProto.ImageMessage{
			URL:           proto.String(resp.URL),
			DirectPath:    proto.String(resp.DirectPath),
			MediaKey:      resp.MediaKey,
			Mimetype:      proto.String("image/png"),
			FileEncSHA256: resp.FileEncSHA256,
			FileSHA256:    resp.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(imageData))),
			Caption:       proto.String(menu),
			ContextInfo: &waProto.ContextInfo{
				StanzaID:      proto.String(v.Info.ID),
				Participant:   proto.String("0@s.whatsapp.net"), // 👈 ویریفائیڈ لک کے لیے
				QuotedMessage: &waProto.Message{
					Conversation: proto.String("𝗦𝗜𝗟𝗘𝗡𝗧 𝗛𝗮𝗰𝗸𝗲𝗿𝘀 𝗢𝗳𝗳𝗶𝗰𝗶𝗮𝗹 𝗕𝗼𝘁 ✅"),
				},
			},
		},
	})
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
