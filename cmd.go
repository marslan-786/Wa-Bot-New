package main

import (
	"context"
	"fmt"
//	"os"
	"strings"
	"math/rand"
	"time"
//	"encoding/json"
    "bytes"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	waLog "go.mau.fi/whatsmeow/util/log"
	"go.mau.fi/whatsmeow/appstate"
)

// ==========================================
// 🧠 MAIN HANDLER (Zero-Delay Interceptor)
// ==========================================
// فائل کے اوپر امپورٹس میں "encoding/json" لازمی ایڈ کر لینا

func EventHandler(client *whatsmeow.Client, evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		if time.Since(v.Info.Timestamp) > 60*time.Second { return }
		go processMessageAsync(client, v)

	case *events.CallOffer: // 📞 کال ڈیٹیکٹ کرنے کے لیے
		settings := getBotSettings(client)
		go handleAntiCallLogic(client, v, settings)
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
	
	if v.Info.IsFromMe { return }

	settings := getBotSettings(client)

	// 🛡️ ANTI-DM WATCHER (یہ سب سے اوپر ہونا چاہیے!)
	// اگر اس فنکشن نے True ریٹرن کیا (یعنی بندہ بلاک ہو گیا)، تو یہیں سے واپس مڑ جاؤ
	if handleAntiDMWatch(client, v, settings) {
		return 
	}

	body := ""
	if v.Message.Conversation != nil {
		body = *v.Message.Conversation
	} else if v.Message.ExtendedTextMessage != nil && v.Message.ExtendedTextMessage.Text != nil {
		body = *v.Message.ExtendedTextMessage.Text
	}
	bodyClean = strings.TrimSpace(body)
	if bodyClean == "" { return }

	// 🔥 1. سیشن کی سیٹنگز لائیں (نئے کلین طریقے سے)
	settings := getBotSettings(client)
	handleAntiDeleteLogic(client, v, settings)
	handleAntiVVLogic(client, v, settings)

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


		// 3. Auto React (ملٹیپل ایموجیز کے ساتھ)
	if settings.AutoReact && !isGroup && !v.Info.IsFromMe {
		// 🎭 یہاں تم اپنی مرضی کے جتنے مرضی ایموجیز ڈال سکتے ہو
		emojis := []string{"❤️", "🔥", "🚀", "👍", "💯", "😎", "😂", "✨", "🎉", "💖", "🥰", "🫡", "👀", "🌟"}
		
		// 🎲 ان میں سے کوئی ایک رینڈم ایموجی سلیکٹ کرو
		randomEmoji := emojis[rand.Intn(len(emojis))]
		
		react(client, v.Info.Chat, v.Info.ID, randomEmoji)
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
	case "antideletes":
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
		
	// 🎨 EDITING ZONE COMMANDS
	case "s", "sticker":
		react(client, v.Info.Chat, v.Info.ID, "🎨")
		go handleSticker(client, v)

	case "toimg":
		react(client, v.Info.Chat, v.Info.ID, "🖼️")
		go handleToImg(client, v)

	case "tovideo":
		react(client, v.Info.Chat, v.Info.ID, "📽️")
		go handleToVideo(client, v, false)

	case "togif":
		react(client, v.Info.Chat, v.Info.ID, "👾")
		go handleToVideo(client, v, true)

	case "tourl":
		react(client, v.Info.Chat, v.Info.ID, "🌐")
		go handleToUrl(client, v)

	case "toptt":
		react(client, v.Info.Chat, v.Info.ID, "🎙️")
		go handleToPTT(client, v, fullArgs)

	case "fancy":
		react(client, v.Info.Chat, v.Info.ID, "✨")
		go handleFancy(client, v, fullArgs)
		
		
	case "id":
		react(client, v.Info.Chat, v.Info.ID, "🪪")
		go handleID(client, v)
		
   	// ✨ AI TOOLS COMMANDS
	case "img", "image":
		react(client, v.Info.Chat, v.Info.ID, "🎨")
		go handleImageGen(client, v, fullArgs)

	case "tr", "translate":
		react(client, v.Info.Chat, v.Info.ID, "🔄")
		go handleTranslate(client, v, fullArgs)

	case "ss", "screenshot":
		react(client, v.Info.Chat, v.Info.ID, "📸")
		go handleScreenshot(client, v, fullArgs)

	case "weather":
		react(client, v.Info.Chat, v.Info.ID, "🌤️")
		go handleWeather(client, v, fullArgs)

	case "google", "search":
		react(client, v.Info.Chat, v.Info.ID, "🔍")
		go handleGoogle(client, v, fullArgs)
    
    // 👁️ OWNER COMMANDS
	case "antivv":
		if !userIsOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleAntiVVToggle(client, v, fullArgs)    
                
    // 🛡️ OWNER COMMANDS
	case "antidelete":
		if !userIsOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
		go handleAntiDeleteToggle(client, v, fullArgs)
    
	case "remini", "removebg":
		react(client, v.Info.Chat, v.Info.ID, "⏳")
		replyMessage(client, v, "⚠️ *Premium Feature:*\nThis feature requires a dedicated API Key. It will be unlocked in the next update by Silent Hackers!")
		
    case "rvc", "vc":
		react(client, v.Info.Chat, v.Info.ID, "🎙️")
		go handleRVC(client, v)
		
	// 🚫 SECURITY COMMANDS
	case "anticall":
        if !userIsOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
        go handleToggleSettings(client, v, "anti_call", fullArgs)

    case "antidm":
        if !userIsOwner { react(client, v.Info.Chat, v.Info.ID, "❌"); return }
        go handleToggleSettings(client, v, "anti_dm", fullArgs)
			
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
 │    _Block Links in Group_
 │
 │ ➭ *%[3]santipic* [on/off]
 │    _Block Image Sharing_
 │
 │ ➭ *%[3]santivideo* [on/off]
 │    _Block Video Sharing_
 │
 │ ➭ *%[3]santisticker* [on/off]
 │    _Block Sticker Sharing_
 │
 │ ➭ *%[3]swelcome* [on/off]
 │    _Welcome New Members_
 │
 │ ➭ *%[3]santidelete* [on/off]
 │    _Anti Delete Messages_
 │
 │ ➭ *%[3]skick* [@tag/reply]
 │    _Remove Member_
 │
 │ ➭ *%[3]sadd* [number]
 │    _Add New Member_
 │
 │ ➭ *%[3]spromote* [@tag/reply]
 │    _Make Group Admin_
 │
 │ ➭ *%[3]sdemote* [@tag/reply]
 │    _Remove Admin Role_
 │
 │ ➭ *%[3]stagall* [text]
 │    _Mention All Members_
 │
 │ ➭ *%[3]shidetag* [text]
 │    _Silent Tag All Members_
 │
 │ ➭ *%[3]sgroup* [open/close]
 │    _Change Group Settings_
 │
 │ ➭ *%[3]sdel* [reply]
 │    _Delete For Everyone_
 │ 
 ╰──────────────────────╯

 ╭── ✦ [ 🛠️ 𝗨𝗧𝗜𝗟𝗜𝗧𝗬 ] ──╮
 │ 
 │ ➭ *%[3]svv* [reply to media]
 │    _Anti View-Once Media Extract_
 │
 │ ➭ *%[3]sid*
 │    _Get Your Chat ID_
 │
 │ ➭ *%[3]svc* [Reply Voice] + [nmbr]
 │    _change your voice_
 │ 
 ╰──────────────────────╯
 
 ╭── ✦ [ 🎨 𝗘𝗗𝗜𝗧𝗜𝗡𝗚 𝗭𝗢𝗡𝗘 🎨 ] ──╮
 │ 
 │ ➭ *%[3]ss* / *%[3]ssticker* [reply image]
 │    _Convert Image to Sticker_
 │
 │ ➭ *%[3]stoimg* [reply sticker]
 │    _Convert Sticker to Image_
 │
 │ ➭ *%[3]stogif* [reply sticker]
 │    _Convert Sticker to GIF_
 │
 │ ➭ *%[3]stovideo* [reply sticker]
 │    _Convert Sticker to Video_
 │
 │ ➭ *%[3]stourl* [reply media]
 │    _Upload Media to Link_
 │
 │ ➭ *%[3]stoptt* [reply audio]
 │    _Convert Text to Voice Note_
 │
 │ ➭ *%[3]sfancy* [text]
 │    _Generate Fancy Fonts_
 │ 
 ╰──────────────────────╯
 
 ╭── ✦ [ ✨ 𝗔𝗜 𝗧𝗢𝗢𝗟𝗦 ✨ ] ──╮
 │ 
 │ ➭ *%[3]simg* [prompt]
 │    _Generate AI Image_
 │
 │ ➭ *%[3]sremini* [reply img]
 │    _Enhance Image Quality_
 │
 │ ➭ *%[3]sremovebg* [reply img]
 │    _Remove Background_
 │
 │ ➭ *%[3]str* [lang] [text]
 │    _Translate Text_
 │
 │ ➭ *%[3]sss* [website link]
 │    _Take Website Screenshot_
 │
 │ ➭ *%[3]sgoogle* [query]
 │    _Search on Google_
 │
 │ ➭ *%[3]sweather* [city]
 │    _Check City Weather_
 │ 
 ╰──────────────────────╯


  ⚡━ ✦ 💖 𝙎𝙞𝙡𝙚𝙣𝙩 𝙃𝙖𝙘𝙠𝙚𝙧𝙨 💖 ✦ ━ ⚡`, 
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

// ==========================================
// 🪪 COMMAND: .id (Get JID Info)
// ==========================================
func handleID(client *whatsmeow.Client, v *events.Message) {
	// 1. چیٹ اور سینڈر کی آئی ڈی نکالیں
	chatJID := v.Info.Chat.String()
	senderJID := v.Info.Sender.ToNonAD().String()

	// 2. چیک کریں کہ گروپ ہے یا پرائیویٹ چیٹ
	chatType := "👤 𝗣𝗿𝗶𝘃𝗮𝘁𝗲 𝗖𝗵𝗮𝘁"
	if strings.Contains(chatJID, "@g.us") {
		chatType = "👥 𝗚𝗿𝗼𝘂𝗽 𝗖𝗵𝗮𝘁"
	}

	// 3. وی آئی پی کارڈ ڈیزائن بنانا شروع کریں
	card := fmt.Sprintf(`❖ ── ✦ 🪪 𝗜𝗗 𝗖𝗔𝗥𝗗 ✦ ── ❖

 %s
 ➭ *%s*

 👤 𝗦𝗲𝗻𝗱𝗲𝗿
 ➭ *%s*`, chatType, chatJID, senderJID)

	// 4. اگر کسی میسج کا ریپلائی کیا ہے، تو اس کا ڈیٹا بھی نکالیں
	extMsg := v.Message.GetExtendedTextMessage()
	if extMsg != nil && extMsg.ContextInfo != nil && extMsg.ContextInfo.Participant != nil {
		quotedJID := *extMsg.ContextInfo.Participant
		card += fmt.Sprintf("\n\n 🎯 𝗧𝗮𝗿𝗴𝗲𝘁 (𝗤𝘂𝗼𝘁𝗲𝗱)\n ➭ *%s*", quotedJID)
	}

	// کارڈ کا اینڈ
	card += "\n\n ╰──────────────────────╯"

	// 5. میسج سینڈ کریں
	replyMessage(client, v, card)
}

// ==========================================
// 🛡️ ANTI-DELETE SYSTEM (Auto-Forwarding)
// ==========================================
func handleAntiDeleteLogic(client *whatsmeow.Client, v *events.Message, settings BotSettings) {
	if protoMsg := v.Message.GetProtocolMessage(); protoMsg != nil && protoMsg.GetType() == waProto.ProtocolMessage_REVOKE {
		if !settings.PrivateAntiDelete { return }

		targetMsgID := protoMsg.GetKey().GetID()
		senderJID := protoMsg.GetKey().GetParticipant()
		if senderJID == "" { senderJID = v.Info.Sender.String() }

		var rawMsg []byte
		var msgTimestamp int64
		err := settingsDB.QueryRow("SELECT msg_content, timestamp FROM message_cache WHERE msg_id = ?", targetMsgID).Scan(&rawMsg, &msgTimestamp)
		
		if err == nil && len(rawMsg) > 0 {
			var originalMsg waProto.Message
			proto.Unmarshal(rawMsg, &originalMsg)

			loc, _ := time.LoadLocation("Asia/Karachi")
			sentTime := time.Unix(msgTimestamp, 0).In(loc).Format("02 Jan 2006, 03:04 PM")
			deletedTime := time.Now().In(loc).Format("02 Jan 2006, 03:04 PM")
			
			cleanSender := strings.Split(senderJID, "@")[0]

			warningText := fmt.Sprintf(`❖ ── ✦ 🚫 𝗔𝗡𝗧𝗜-𝗗𝗘𝗟𝗘𝗧𝗘 🚫 ✦ ── ❖

👤 *Sender:* @%s
📅 *Sent At:* %s
🗑️ *Deleted At:* %s

_Attempted to delete this message!_
╰──────────────────────╯`, cleanSender, sentTime, deletedTime)

			ownerJID := client.Store.ID.ToNonAD() 

			client.SendMessage(context.Background(), ownerJID, &waProto.Message{
				ExtendedTextMessage: &waProto.ExtendedTextMessage{
					Text: proto.String(warningText),
					ContextInfo: &waProto.ContextInfo{ MentionedJID: []string{senderJID} },
				},
			})
			client.SendMessage(context.Background(), ownerJID, &originalMsg)
		}
		return
	}

	if !strings.Contains(v.Info.Chat.String(), "@g.us") && v.Message != nil {
		msgBytes, err := proto.Marshal(v.Message)
		if err == nil {
			settingsDB.Exec("INSERT OR REPLACE INTO message_cache (msg_id, sender_jid, msg_content, timestamp) VALUES (?, ?, ?, ?)", 
				v.Info.ID, v.Info.Sender.String(), msgBytes, v.Info.Timestamp.Unix())
		}
	}
}

// ==========================================
// 🛡️ COMMAND: .antidelete (On/Off)
// ==========================================
func handleAntiDeleteToggle(client *whatsmeow.Client, v *events.Message, args string) {
	args = strings.ToLower(strings.TrimSpace(args))
	if args != "on" && args != "off" {
		replyMessage(client, v, "❌ Use: `.antidelete on` or `.antidelete off`")
		return
	}
	
	state := false
	if args == "on" { state = true }
	
	cleanJID := client.Store.ID.ToNonAD().User
	settingsDB.Exec("UPDATE bot_settings SET private_antidelete = ? WHERE jid = ?", state, cleanJID)
	
	react(client, v.Info.Chat, v.Info.ID, "✅")
	replyMessage(client, v, fmt.Sprintf("✅ *Private Anti-Delete* is now *%s*", strings.ToUpper(args)))
}

// ==========================================
// 👁️ ANTI-VIEWONCE SYSTEM (Auto-Forwarding)
// ==========================================
func handleAntiVVLogic(client *whatsmeow.Client, v *events.Message, settings BotSettings) {
	if !settings.AntiVV || v.Message == nil || v.Info.IsFromMe {
		return
	}

	// 1. چیک کریں کہ کیا یہ ویو ونس میسج ہے؟ (V1, V2 یا Audio Extension)
	vo1 := v.Message.GetViewOnceMessage()
	vo2 := v.Message.GetViewOnceMessageV2()
	vo3 := v.Message.GetViewOnceMessageV2Extension()

	if vo1 == nil && vo2 == nil && vo3 == nil {
		return // ویو ونس نہیں ہے، واپس چلے جاؤ
	}

	// 2. اصل میڈیا میسج نکالیں
	var imgMsg *waProto.ImageMessage
	var vidMsg *waProto.VideoMessage
	var audMsg *waProto.AudioMessage

	if vo1 != nil {
		imgMsg = vo1.GetMessage().GetImageMessage()
		vidMsg = vo1.GetMessage().GetVideoMessage()
	} else if vo2 != nil {
		imgMsg = vo2.GetMessage().GetImageMessage()
		vidMsg = vo2.GetMessage().GetVideoMessage()
	} else if vo3 != nil {
		audMsg = vo3.GetMessage().GetAudioMessage()
	}

	// 3. میڈیا ڈاؤنلوڈ کریں
	ctx := context.Background()
	var data []byte
	var err error
	var mType whatsmeow.MediaType

	if imgMsg != nil {
		data, err = client.Download(ctx, imgMsg)
		mType = whatsmeow.MediaImage
	} else if vidMsg != nil {
		data, err = client.Download(ctx, vidMsg)
		mType = whatsmeow.MediaVideo
	} else if audMsg != nil {
		data, err = client.Download(ctx, audMsg)
		mType = whatsmeow.MediaAudio
	}

	if err != nil || len(data) == 0 { return }

	// 4. دوبارہ اپلوڈ کریں
	up, err := client.Upload(ctx, data, mType)
	if err != nil { return }

	// 5. کیپشن تیار کریں (پاکستانی ٹائم کے ساتھ)
	senderJID := v.Info.Sender.String()
	cleanSender := strings.Split(senderJID, "@")[0]
	loc, _ := time.LoadLocation("Asia/Karachi")
	recvTime := time.Now().In(loc).Format("02 Jan 2006, 03:04 PM")

	chatType := "👤 Private Chat"
	if strings.Contains(v.Info.Chat.String(), "@g.us") {
		chatType = "👥 Group Chat"
	}

	caption := fmt.Sprintf(`❖ ── ✦ 👁️ 𝗔𝗡𝗧𝗜-𝗩𝗜𝗘𝗪 𝗢𝗡𝗖𝗘 ✦ ── ❖

👤 *Sender:* @%s
📍 *Source:* %s
🕒 *Time:* %s

_Attempted to send View Once media!_
╰──────────────────────╯`, cleanSender, chatType, recvTime)

	// 6. میسج تیار کر کے اونر کو بھیجیں
	ownerJID := client.Store.ID.ToNonAD()
	var finalMsg waProto.Message

	if imgMsg != nil {
		finalMsg.ImageMessage = &waProto.ImageMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, Mimetype: proto.String("image/jpeg"),
			FileSHA256: up.FileSHA256, FileEncSHA256: up.FileEncSHA256,
			FileLength: proto.Uint64(uint64(len(data))), Caption: proto.String(caption),
		}
	} else if vidMsg != nil {
		finalMsg.VideoMessage = &waProto.VideoMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, Mimetype: proto.String("video/mp4"),
			FileSHA256: up.FileEncSHA256, FileEncSHA256: up.FileEncSHA256,
			FileLength: proto.Uint64(uint64(len(data))), Caption: proto.String(caption),
		}
	} else if audMsg != nil {
		// آڈیو کے ساتھ کیپشن ڈائریکٹ نہیں جاتا، اس لیے پہلے ٹیکسٹ بھیجیں گے
		client.SendMessage(ctx, ownerJID, &waProto.Message{
			ExtendedTextMessage: &waProto.ExtendedTextMessage{ Text: proto.String(caption) },
		})
		finalMsg.AudioMessage = &waProto.AudioMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, Mimetype: proto.String("audio/ogg; codecs=opus"),
			FileSHA256: up.FileEncSHA256, FileEncSHA256: up.FileEncSHA256,
			FileLength: proto.Uint64(uint64(len(data))), PTT: proto.Bool(true),
		}
	}

	// 7. فائنل سینڈ
	client.SendMessage(ctx, ownerJID, &finalMsg)
}

// ==========================================
// 👁️ COMMAND: .antivv (On/Off)
// ==========================================
func handleAntiVVToggle(client *whatsmeow.Client, v *events.Message, args string) {
	args = strings.ToLower(strings.TrimSpace(args))
	if args != "on" && args != "off" {
		replyMessage(client, v, "❌ Use: `.antivv on` or `.antivv off`")
		return
	}
	
	state := false
	if args == "on" { state = true }
	
	cleanJID := client.Store.ID.ToNonAD().User
	settingsDB.Exec("UPDATE bot_settings SET anti_vv = ? WHERE jid = ?", state, cleanJID)
	
	react(client, v.Info.Chat, v.Info.ID, "✅")
	replyMessage(client, v, fmt.Sprintf("✅ *Anti View-Once* is now *%s*", strings.ToUpper(args)))
}

// ==========================================
// 🛡️ ANTI-DM & ANTI-CALL LOGIC
// ==========================================

func handleAntiDMWatch(client *whatsmeow.Client, v *events.Message, settings BotSettings) bool {
	if !settings.AntiDM || v.Info.IsGroup || v.Info.IsFromMe || isOwner(client, v) {
		return false
	}

	realSender := v.Info.Sender.ToNonAD()
	// 🌟 FIX: context.Background() کا اضافہ کیا گیا ہے
	contact, err := client.Store.Contacts.GetContact(context.Background(), realSender)
	isSaved := (err == nil && contact.Found && contact.FullName != "")
	
	if !isSaved {
		warning := "⚠️ *Silent Nexus Security*\n\nUnsaved number detected! Automatic block initiated."
		client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
			Conversation: proto.String(warning),
		})

		client.UpdateBlocklist(context.Background(), v.Info.Sender.ToNonAD(), events.BlocklistChangeActionBlock)
		patch := appstate.BuildDeleteChat(v.Info.Chat, v.Info.Timestamp, nil, true)
		client.SendAppState(context.Background(), patch)

		return true 
	}
	return false
}

func handleAntiCallLogic(client *whatsmeow.Client, c *events.CallOffer, settings BotSettings) {
    if !settings.AntiCall || isCallOwner(client, c.CallCreator) { return }

	// 🌟 FIX: context.Background() ایڈ کر دیا ہے
	contact, err := client.Store.Contacts.GetContact(context.Background(), c.CallCreator)
	if err == nil && contact.Found && contact.FullName != "" { return }

	// 🌟 FIX: RejectCall میں پہلا آرگیومنٹ Context ہے
	client.RejectCall(context.Background(), c.CallCreator, c.CallID)

	warning := "⚠️ *Anti-Call System*\n\nUnsaved numbers are not allowed to call. You are being blocked."
	client.SendMessage(context.Background(), c.CallCreator, &waProto.Message{
		Conversation: proto.String(warning),
	})

	client.UpdateBlocklist(context.Background(), c.CallCreator, events.BlocklistChangeActionBlock)
	patch := appstate.BuildDeleteChat(c.CallCreator, time.Now(), nil, true)
	client.SendAppState(context.Background(), patch)
}