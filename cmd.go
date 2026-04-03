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
	waLog "go.mau.fi/whatsmeow/util/log"
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

	// 🔥 1. سیشن کی سیٹنگز لائیں (نئے کلین طریقے سے)
	settings := getBotSettings(client)

	// 🔥 2. چیک کریں کہ یوزر اونر ہے یا نہیں
	userIsOwner := isOwner(client, v)
	isGroup := strings.Contains(v.Info.Chat.String(), "@g.us")

	// ==========================================
	// 🌟 AUTO FEATURES ENGINE (Run before commands)
	// ==========================================
	if v.Info.Chat.User == "status" { 
		if settings.AutoStatus {
			// FIX: context.Background() ایڈ کیا گیا ہے
			client.MarkRead(context.Background(), []types.MessageID{v.Info.ID}, v.Info.Timestamp, v.Info.Chat, v.Info.Sender)
		}
		if settings.StatusReact {
			react(client, v.Info.Chat, v.Info.ID, "💚") 
		}
		return 
	}

	if settings.AutoRead {
		// FIX: context.Background() ایڈ کیا گیا ہے
		client.MarkRead(context.Background(), []types.MessageID{v.Info.ID}, v.Info.Timestamp, v.Info.Chat, v.Info.Sender)
	}


	if settings.AutoReact && !isGroup && !v.Info.IsFromMe {
		react(client, v.Info.Chat, v.Info.ID, "🚀")
	}
	// ==========================================

	// 🔥 3. موڈ کے حساب سے فلٹر کریں
	if !userIsOwner { // اونر پر موڈ کی پابندی نہیں ہوتی
		if settings.Mode == "private" && isGroup {
			return // گروپس میں بلاک
		}
		if settings.Mode == "admin" && isGroup {
			groupInfo, err := client.GetGroupInfo(context.Background(), v.Info.Chat)
			if err != nil { return }
			isAdmin := false
			for _, participant := range groupInfo.Participants {
				// ToNonAD() یوز کر کے کلین آئی ڈی میچ کریں گے
				if participant.JID.User == v.Info.Sender.ToNonAD().User && (participant.IsAdmin || participant.IsSuperAdmin) {
					isAdmin = true
					break
				}
			}
			if !isAdmin { return } // اگر ایڈمن نہیں تو اگنور کرو
		}
	}

	// مینو ریپلائی چیک
	extMsg := v.Message.GetExtendedTextMessage()
	if extMsg != nil && extMsg.ContextInfo != nil && extMsg.ContextInfo.StanzaID != nil {
		qID := *extMsg.ContextInfo.StanzaID
		if HandleMenuReplies(client, v, bodyClean, qID) { return }
	}

	// 🔥 4. ڈائنامک پریفکس چیک کریں
	if !strings.HasPrefix(bodyClean, settings.Prefix) {
		return
	}

	msgWithoutPrefix := strings.TrimPrefix(bodyClean, settings.Prefix)
	words := strings.Fields(msgWithoutPrefix)
	if len(words) == 0 { return }

	cmd := strings.ToLower(words[0])
	fullArgs := strings.TrimSpace(strings.Join(words[1:], " "))

	// ==========================================
	// 🎯 COMMAND SWITCH ENGINE
	// ==========================================
	switch cmd {
    
	// 👑 OWNER COMMANDS (With Specific Reactions)
	case "setprefix":
		if !userIsOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		react(client, v.Info.Chat, v.Info.ID, "⚙️")
		go handleSetPrefix(client, v, fullArgs)

	case "mode":
		if !userIsOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		react(client, v.Info.Chat, v.Info.ID, "🛡️")
		go handleMode(client, v, fullArgs)

	case "alwaysonline":
		if !userIsOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		react(client, v.Info.Chat, v.Info.ID, "🟢")
		go handleToggleSetting(client, v, "Always Online", "always_online", fullArgs)

	case "autoread":
		if !userIsOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		react(client, v.Info.Chat, v.Info.ID, "👁️")
		go handleToggleSetting(client, v, "Auto Read", "auto_read", fullArgs)

	case "autoreact":
		if !userIsOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		react(client, v.Info.Chat, v.Info.ID, "❤️")
		go handleToggleSetting(client, v, "Auto React", "auto_react", fullArgs)

	case "autostatus":
		if !userIsOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		react(client, v.Info.Chat, v.Info.ID, "📲")
		go handleToggleSetting(client, v, "Auto Status View", "auto_status", fullArgs)

	case "statusreact":
		if !userIsOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		react(client, v.Info.Chat, v.Info.ID, "💚")
		go handleToggleSetting(client, v, "Status React", "status_react", fullArgs)

	case "listbots":
		if !userIsOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		react(client, v.Info.Chat, v.Info.ID, "🤖")
		go handleListBots(client, v)

	case "stats":
		if !userIsOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		react(client, v.Info.Chat, v.Info.ID, "📊")
		go handleStats(client, v, settings.UptimeStart)


	// 🌐 PUBLIC/GENERAL COMMANDS
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
    
    	// 🌐 PUBLIC/GENERAL COMMANDS
	case "pair":
		// یہاں اونر چیک نہیں ہے! کوئی بھی یوز کر سکتا ہے
		react(client, v.Info.Chat, v.Info.ID, "🔗")
		go handlePair(client, v, fullArgs)
		
	// 🛡️ GROUP ADMIN COMMANDS
	case "antilink":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleGroupToggle(client, v, "Anti-Link", "antilink", fullArgs)
	case "antipic":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleGroupToggle(client, v, "Anti-Picture", "antipic", fullArgs)
	case "antivideo":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleGroupToggle(client, v, "Anti-Video", "antivideo", fullArgs)
	case "antisticker":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleGroupToggle(client, v, "Anti-Sticker", "antisticker", fullArgs)
	case "welcome":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleGroupToggle(client, v, "Welcome Message", "welcome", fullArgs)
	case "antidelete":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleGroupToggle(client, v, "Anti-Delete", "antidelete", fullArgs)

	case "kick":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleKick(client, v, fullArgs)
	case "add":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleAdd(client, v, fullArgs)
	case "promote":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handlePromote(client, v, fullArgs)
	case "demote":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleDemote(client, v, fullArgs)
	case "group":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleGroupState(client, v, fullArgs)
	case "del":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleDel(client, v)
	case "tagall":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleTags(client, v, false, fullArgs)
	case "hidetag":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleTags(client, v, true, fullArgs)

	// 🛠️ UTILITY COMMANDS (Publicly Available)
	case "vv":
		react(client, v.Info.Chat, v.Info.ID, "👀")
		go handleVV(client, v)
		
    
	case "fb", "facebook", "ig", "insta", "instagram", "tw", "x", "twitter", "pin", "pinterest", "threads", "snap", "snapchat", "reddit", "dm", "dailymotion", "vimeo", "rumble", "bilibili", "douyin", "kwai", "bitchute", "sc", "soundcloud", "spotify", "apple", "applemusic", "deezer", "tidal", "mixcloud", "napster", "bandcamp", "imgur", "giphy", "flickr", "9gag", "ifunny":
	    react(client, v.Info.Chat, v.Info.ID, "🪩")
		go handleUniversalDownload(client, v, fullArgs, cmd)
		
	// 🔥 THE AI MASTERMINDS
	case "ai", "gpt", "chatgpt", "gemini", "claude", "llama", "groq", "bot", "ask":
	    react(client, v.Info.Chat, v.Info.ID, "🧠")
		go handleAICommand(client, v, fullArgs, cmd)
	}
}

func sendMainMenu(client *whatsmeow.Client, v *events.Message, settings BotSettings) {
	// اپ ٹائم حاصل کریں
	uptimeStr := getUptimeString(settings.UptimeStart)

	// 🔥 %[1]s = Mode, %[2]s = Uptime, %[3]s = Prefix 
	// اس ٹرک کی وجہ سے ہمیں بار بار settings.Prefix نہیں لکھنا پڑے گا!
	menu := fmt.Sprintf(`❖ ── ✦ 𝗦𝗜𝗟𝗘𝗡𝗧 𝙃𝙖𝙘𝙠𝙚𝙧𝙨 ✦ ── ❖
 
 👤 𝗢𝘄𝗻𝗲𝗿: 𝗦𝗜𝗟𝗘𝗡𝗧 𝙃𝙖𝙘𝙠𝙚𝙧𝙨
 ⚙️ 𝗠𝗼𝗱𝗲: %[1]s
 ⏱️ 𝗨𝗽𝘁𝗶𝗺𝗲: %[2]s
 ⚡ 𝗣𝗿𝗲𝗳𝗶𝘅: [ %[3]s ]

 ╭── ✦ [ 𝗬𝗢𝗨𝗧𝗨𝗕𝗘 𝗠𝗘𝗡𝗨 ] ✦ ──╮
 │ 
 │ ➭ *%[3]splay / %[3]ssong* [name]
 │    _Direct HQ Audio Download_
 │
 │ ➭ *%[3]svideo* [name]
 │    _Direct HD Video Download_
 │
 │ ➭ *%[3]syt* [link]
 │    _Download YT Video/Audio_
 │
 │ ➭ *%[3]syts* [query]
 │    _Search YouTube Videos_
 │
 ╰──────────────────────╯

 ╭── ✦ [ 𝗧𝗜𝗞𝗧𝗢𝗞 𝗠𝗘𝗡𝗨 ] ✦ ──╮
 │ 
 │ ➭ *%[3]stt* [link]
 │    _No-Watermark TT Video_
 │
 │ ➭ *%[3]stt audio* [link]
 │    _Extract TikTok Sound_
 │
 │ ➭ *%[3]stts* [query]
 │    _Search TikTok Trends_
 │
 ╰──────────────────────╯

 ╭── ✦ [ 𝗨𝗡𝗜𝗩𝗘𝗥𝗦𝗔𝗟 𝗠𝗘𝗗𝗜𝗔 ] ✦ ──╮
 │ 
 │ ➭ *%[3]sfb / %[3]sfacebook* [link]
 │    _FB High-Quality Videos_
 │
 │ ➭ *%[3]sig / %[3]sinsta* [link]
 │    _Instagram Reels/IGTV_
 │
 │ ➭ *%[3]stw / %[3]sx* [link]
 │    _X/Twitter Media Extract_
 │
 │ ➭ *%[3]ssnap* [link]
 │    _Snapchat Spotlights_
 │
 │ ➭ *%[3]sthreads* [link]
 │    _Threads Video Download_
 │
 │ ➭ *%[3]spin* [link]
 │    _Pinterest Video/Images_
 │
 │ ➭ *%[3]sreddit* [link]
 │    _Reddit Videos & GIFs_
 │
 ╰──────────────────────╯

 ╭── ✦ [ 🧠 𝗔𝗜 𝗠𝗔𝗦𝗧𝗘𝗥𝗠𝗜𝗡𝗗𝗦 ] ──╮
 │ 
 │ ➭ *%[3]sai / %[3]sask* [text]
 │    _Faisalabadi Smart AI_
 │
 │ ➭ *%[3]sgpt / %[3]schatgpt* [text]
 │    _ChatGPT 4o Persona_
 │
 │ ➭ *%[3]sgemini* [text]
 │    _Google Gemini Pro_
 │
 │ ➭ *%[3]sclaude* [text]
 │    _Anthropic Claude 3_
 │
 │ ➭ *%[3]sllama / %[3]sgroq* [text]
 │    _Meta Llama 3 Fast Engine_
 │
 ╰──────────────────────╯

 ╭── ✦ [ 𝗢𝗪𝗡𝗘𝗥 𝗠𝗘𝗡𝗨 ] ✦ ──╮
 │ 
 │ ➭ *%[3]ssetprefix* [symbol]
 │    _Change Bot Prefix_
 │
 │ ➭ *%[3]smode* [public/private/admin]
 │    _Change Bot Work Mode_
 │
 │ ➭ *%[3]salwaysonline* [on/off]
 │    _Force Online Status_
 │
 │ ➭ *%[3]sautoread* [on/off]
 │    _Auto Seen Messages_
 │
 │ ➭ *%[3]sautoreact* [on/off]
 │    _Auto Like Messages_
 │
 │ ➭ *%[3]sautostatus* [on/off]
 │    _Auto View Status_
 │
 │ ➭ *%[3]sstatusreact* [on/off]
 │    _Auto Like Status_
 │
 │ ➭ *%[3]slistbots*
 │    _Show Active Sessions_
 │
 │ ➭ *%[3]sstats*
 │    _Check System Power_
 │
 │ ➭ *%[3]spair* [number]
 │    _Connect New Bot Session_
 │
 ╰──────────────────────╯
 
 ╭── ✦ [ 🛡️ 𝗚𝗥𝗢𝗨𝗣 𝗠𝗘𝗡𝗨 🛡️ ] ──╮
 │ 
 │ ➭ *%[3]santilink* [on/off]
 │ ➭ *%[3]santipic* [on/off]
 │ ➭ *%[3]santivideo* [on/off]
 │ ➭ *%[3]santisticker* [on/off]
 │ ➭ *%[3]swelcome* [on/off]
 │ ➭ *%[3]santidelete* [on/off]
 │ ➭ *%[3]skick* [@tag/reply]
 │ ➭ *%[3]sadd* [number]
 │ ➭ *%[3]spromote* [@tag/reply]
 │ ➭ *%[3]sdemote* [@tag/reply]
 │ ➭ *%[3]stagall* [text]
 │ ➭ *%[3]shidetag* [text]
 │ ➭ *%[3]sgroup* [open/close]
 │ ➭ *%[3]sdel* [reply]
 │ 
 ╰──────────────────────╯

 ╭── ✦ [ 🛠️ 𝗨𝗧𝗜𝗟𝗜𝗧𝗬 ] ──╮
 │ 
 │ ➭ *%[3]svv* [reply to media]
 │    _Anti View-Once Media Extract_
 │ 
 ╰──────────────────────╯
 

     ⚡ ━━━ ✦ 💖 𝙎𝙞𝙡𝙚𝙣𝙩 𝙃𝙖𝙘𝙠𝙚𝙧𝙨 💖 ✦ ━━━ ⚡`, 
	strings.ToUpper(settings.Mode), uptimeStr, settings.Prefix)

	client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String(menu),
			ContextInfo: &waProto.ContextInfo{
				StanzaID:      proto.String(v.Info.ID),
				Participant:   proto.String("0@s.whatsapp.net"), // 👈 ویریفائیڈ لک کے لیے
				RemoteJID:     proto.String("status@broadcast"), // 🔥 یہ لائن اسے "Status" کا روپ دے گی!
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
// ==========================================
// 🔗 COMMAND: .pair (Public Pairing)
// ==========================================
func handlePair(client *whatsmeow.Client, v *events.Message, args string) {
	if args == "" {
		replyMessage(client, v, "❌ Please provide a phone number with country code.\nExample: `.pair 923001234567`")
		return
	}

	// 1. نمبر کو کلین کریں (اگر کسی نے + یا اسپیس ڈال دی ہے تو وہ ریموو ہو جائے)
	phone := strings.ReplaceAll(args, "+", "")
	phone = strings.ReplaceAll(phone, " ", "")
	phone = strings.ReplaceAll(phone, "-", "")

	react(client, v.Info.Chat, v.Info.ID, "⏳")
	replyMessage(client, v, "⏳ Generating pairing code... Please wait.")

	// 2. نیا ڈیوائس اسٹور بنائیں (main.go والا dbContainer یوز ہو رہا ہے)
	deviceStore := dbContainer.NewDevice()
	
	// لاگز کو Noop رکھا ہے تاکہ کنسول میں رش نہ لگے
	clientLog := waLog.Noop
	newClient := whatsmeow.NewClient(deviceStore, clientLog)

	// 3. ایونٹ ہینڈلر اٹیچ کریں تاکہ کنیکٹ ہونے کے بعد بوٹ کام شروع کر دے
	newClient.AddEventHandler(func(evt interface{}) {
		EventHandler(newClient, evt)
	})

	// 4. واٹس ایپ سرور سے کنیکٹ کریں
	err := newClient.Connect()
	if err != nil {
		replyMessage(client, v, "❌ Failed to connect to WhatsApp servers.")
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return
	}

	// 5. پیئرنگ کوڈ کی ریکویسٹ کریں
	code, err := newClient.PairPhone(context.Background(), phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		replyMessage(client, v, fmt.Sprintf("❌ Failed to get pairing code: %v", err))
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return
	}

	// 6. کوڈ کو پروفیشنل لک دینے کے لیے درمیان میں ڈیش (-) لگا دیں (e.g. ABCD-EFGH)
	formattedCode := code
	if len(code) == 8 {
		formattedCode = code[:4] + "-" + code[4:]
	}

	// 7. پہلا میسج: ہدایات اور نیچے کی طرف اشارہ
	successMsg := fmt.Sprintf("✅ *PAIRING CODE GENERATED*\n\n📱 *Phone:* +%s\n\n_1. Open WhatsApp on target phone_\n_2. Go to Linked Devices -> Link a Device_\n_3. Select 'Link with phone number instead'_\n_4. Enter the code below_ 👇\n\n⚠️ _This code expires in 2 minutes._", phone)
	replyMessage(client, v, successMsg)
	
	// 8. دوسرا میسج: صرف پیئرنگ کوڈ (ڈائریکٹ کاپی کرنے کے لیے)
	replyMessage(client, v, formattedCode)
	
	react(client, v.Info.Chat, v.Info.ID, "✅")
}
