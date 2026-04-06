package main

import (
	"context"
	"fmt"
//	"os"
	"strings"
	"math/rand"
	"time"
//	"encoding/json"
//    "bytes"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	waLog "go.mau.fi/whatsmeow/util/log"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCommon"
)

// ==========================================
// 🧠 MAIN HANDLER (Zero-Delay Interceptor)
// ==========================================
// فائل کے اوپر امپورٹس میں "encoding/json" لازمی ایڈ کر لینا

func EventHandler(client *whatsmeow.Client, evt interface{}) {
	defer func() {
		if r := recover(); r != nil {
			botID := "unknown"
			if client != nil && client.Store != nil && client.Store.ID != nil {
				botID = getCleanID(client.Store.ID.User)
			}
			fmt.Printf("⚠️ [CRASH PREVENTED in EventHandler] Bot %s error: %v\n", botID, r)
		}
	}()

	switch v := evt.(type) {
	
	case *events.CallOffer:
		go handleAntiCallLogic(client, v, getBotSettings(client))

	case *events.Message:
		
		// 🛑 1. ANTI-DELETE REVOKE CATCHER
		if v.Message.GetProtocolMessage() != nil && v.Message.GetProtocolMessage().GetType() == waProto.ProtocolMessage_REVOKE {
			go handleAntiDeleteRevoke(client, v)
			return // یہیں سے مڑ جائیں!
		}

		// 🛡️ 2. ANTI-DM GATEKEEPER (صرف پرائیویٹ چیٹس کے لیے)
		if !v.Info.IsGroup {
			settings := getBotSettings(client)
			
			// ⚡ اگر Anti-DM نے بلاک مار دیا ہے، تو پروسیسنگ فوراً یہیں روک دو!
			if handleAntiDMWatch(client, v, settings) {
				return 
			}

			// اگر بلاک نہیں ہوا (یعنی سیو نمبر ہے) تو کیشے اور ویو ونس چیک کرو
			go handleAntiDeleteSave(client, v)
			go handleAntiVVLogic(client, v)
		} else {
			// اگر گروپ ہے تو سیدھا کیشے اور ویو ونس چیک کرو
			go handleAntiDeleteSave(client, v)
			go handleAntiVVLogic(client, v)
		}

		// ⏱️ 3. TIME FILTER
		if time.Since(v.Info.Timestamp) > 60*time.Second { 
			return 
		}

		// 🚀 4. MAIN PROCESSOR
		go processMessageAsync(client, v)
		
	case *events.Connected:
		if client.Store != nil && client.Store.ID != nil {
			botCleanID := getCleanID(client.Store.ID.User)
			fmt.Printf("🟢 [ONLINE] Bot %s is secured & ready to rock!\n", botCleanID)
		}
	}
}

func processMessageAsync(client *whatsmeow.Client, v *events.Message) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("⚠️ [VIP CRASH PREVENTED]: %v\n", r)
		}
	}()

	if v.Message == nil { return }

	// 🚫 سب سے پہلا اور سخت فلٹر: واٹس ایپ چینل (Newsletter) کو نظر انداز کریں!
	if v.Info.Chat.Server == "newsletter" || v.Info.Chat.Server == types.NewsletterServer {
		return 
	}

	settings := getBotSettings(client)
	
	// 🌟 FIX: botJID والا ایرر ختم کر دیا، اب یہ وہیں ڈکلیئر ہوگا جہاں اس کی ضرورت ہے۔
	userIsOwner := isOwner(client, v) || v.Info.IsFromMe
	isGroup := v.Info.IsGroup

	// 📝 میسج ٹیکسٹ نکالنا...
	body := ""
	if v.Message.GetConversation() != "" {
		body = v.Message.GetConversation()
	} else if v.Message.GetExtendedTextMessage() != nil {
		body = v.Message.GetExtendedTextMessage().GetText()
	} else if v.Message.GetImageMessage() != nil {
		body = v.Message.GetImageMessage().GetCaption()
	} else if v.Message.GetVideoMessage() != nil {
		body = v.Message.GetVideoMessage().GetCaption()
	}
	
	// 🔥 1. اصل میسج (جس میں کیپیٹل لیٹرز محفوظ ہیں)
	rawBody := strings.TrimSpace(body)
	
	// ⚠️ 2. یہ آپ کا پرانا طریقہ ہے (اسے رہنے دیا ہے تاکہ پرانی کمانڈز نہ ٹوٹیں)
	bodyClean := strings.ToLower(rawBody)

	// 🎯 3. جادو یہاں ہے: میسج کو 2 حصوں میں توڑ لیا (کمانڈ اور لنک)
	command := ""
	rawArgs := ""
	
	parts := strings.SplitN(rawBody, " ", 2) // سپیس کی بنیاد پر دو ٹکڑے کیے
	if len(parts) > 0 {
		// کمانڈ کو ہم نے چھوٹا کر دیا (تاکہ .tt ہو یا .TT، دونوں چلیں)
		command = strings.ToLower(parts[0]) 
	}
	if len(parts) > 1 {
		// آگے والا حصہ (جیسے ٹک ٹاک کا لنک) بالکل اپنی اصلی حالت میں محفوظ ہے!
		rawArgs = strings.TrimSpace(parts[1]) 
	}

	// ==========================================
	// ⚡ 5. AUTO FEATURES ENGINE (Non-Blocking)
	// ==========================================
	
	// 🟢 Status / Broadcast Logic
	if v.Info.Chat.User == "status" {
		go func() {
			if settings.AutoStatus {
				client.MarkRead(context.Background(), []types.MessageID{v.Info.ID}, v.Info.Timestamp, v.Info.Chat, v.Info.Sender)
			}
			if settings.StatusReact {
				react(client, v.Info.Chat, v.Info.ID, "💚")
			}
		}()
		return 
	}

	// 📖 Auto Read & Auto React (بیک گراؤنڈ میں)
	go func() {
		if settings.AutoRead {
			client.MarkRead(context.Background(), []types.MessageID{v.Info.ID}, v.Info.Timestamp, v.Info.Chat, v.Info.Sender)
		}

        if settings.AutoReact {
    

            if v.Info.Chat.Server == "newsletter" {
                return
            }

            emojis := []string{"❤️", "🔥", "🚀", "👍", "💯", "😎", "😂", "✨", "🎉", "💖"}
            randomEmoji := emojis[rand.Intn(len(emojis))]
            react(client, v.Info.Chat, v.Info.ID, randomEmoji)
        }

	}()

	// ==========================================
	// 🚦 6. MODE & PERMISSION FILTERS
	// ==========================================
	if !userIsOwner {
		if settings.Mode == "private" && isGroup { return }
		if settings.Mode == "admin" && isGroup {
			// ایڈمن چیک لاجک (بیک گراؤنڈ میں نہیں ہو سکتی کیونکہ رزلٹ چاہیے)
			groupInfo, err := client.GetGroupInfo(context.Background(), v.Info.Chat)
			if err != nil { return }
			isAdmin := false
			for _, p := range groupInfo.Participants {
				if p.JID.User == v.Info.Sender.ToNonAD().User && (p.IsAdmin || p.IsSuperAdmin) {
					isAdmin = true
					break
				}
			}
			if !isAdmin { return }
		}
	}

	// 7. مینو ریپلائی چیک
	if v.Message.GetExtendedTextMessage() != nil && v.Message.GetExtendedTextMessage().ContextInfo != nil {
		qID := v.Message.GetExtendedTextMessage().ContextInfo.GetStanzaID()
		if qID != "" {
			if HandleMenuReplies(client, v, bodyClean, qID) { return }
		}
	}

	// ==========================================
	// 🚀 8. COMMAND DISPATCHER
	// ==========================================
	
	// پریفکس چیک (اگر اونر ہے تو بغیر پریفکس کے بھی کمانڈز چل سکتی ہیں، لیکن ہم پریفکس برقرار رکھیں گے)
	if !strings.HasPrefix(bodyClean, settings.Prefix) { return }

	msgWithoutPrefix := strings.TrimPrefix(bodyClean, settings.Prefix)
	words := strings.Fields(msgWithoutPrefix)
	if len(words) == 0 { return }

	cmd := strings.ToLower(words[0])
	fullArgs := strings.TrimSpace(strings.Join(words[1:], " "))

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

	case "yts":
		react(client, v.Info.Chat, v.Info.ID, "🔍")
		go handleYTS(client, v, fullArgs)

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
		// fullArgs کی جگہ rawArgs اور cmd کی جگہ command آ گیا ہے
		go handleUniversalDownload(client, v, rawArgs, command)

	case "tt", "tiktok":
		react(client, v.Info.Chat, v.Info.ID, "📱")
		// fullArgs کی جگہ rawArgs (جس میں اوریجنل کیپیٹل لیٹرز محفوظ ہیں)
		go handleTikTok(client, v, rawArgs)

	case "yt", "youtube":
		react(client, v.Info.Chat, v.Info.ID, "🎬")
		// fullArgs کی جگہ rawArgs
		go handleYTDirect(client, v, rawArgs)

		
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
 
 ╭── ✦ [ ☠️ 𝗗𝗔𝗡𝗚𝗘𝗥𝗢𝗨𝗦 𝗭𝗢𝗡𝗘 ] ──╮
 │ 
 │ ➭ *%[3]santidelete* [on/off]
 │    _Auto Recover Deleted Msgs_
 │
 │ ➭ *%[3]santivv* [on/off]
 │    _Auto Save View-Once Media_
 │
 │ ➭ *%[3]santicall* [on/off]
 │    _Auto Block Incoming Calls_
 │
 │ ➭ *%[3]santidm* [on/off]
 │    _Auto Block Unsaved DMs_
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
	// 🚀 Goroutine: یہ فوراً الگ تھریڈ میں چلا جائے گا اور مین کوڈ کو نہیں روکے گا
	go func() {
		// 🛡️ Panic Recovery: اگر ری ایکشن میں کوئی ایرر آئے تو بوٹ کریش نہ ہو
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("⚠️ React Panic: %v\n", r)
			}
		}()

		// یہ میسج اب بیک گراؤنڈ میں جائے گا
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

		// اگر آپ ایرر دیکھنا چاہتے ہیں (Optional)
		if err != nil {
			fmt.Printf("❌ React Failed: %v\n", err)
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

func handleAntiCallLogic(client *whatsmeow.Client, c *events.CallOffer, settings BotSettings) {
	// 1. گروپ کال بائی پاس
	if c.CallCreator.Server == "g.us" || c.CallCreator.Server == types.GroupServer {
		return 
	}

	botJID := client.Store.ID.ToNonAD().User
	callerJID := c.CallCreator.ToNonAD()

	// 🌟 2. DIRECT DATABASE CHECK (تاکہ getBotSettings کا کوئی بھی بگ اسے روک نہ سکے)
	isCallEnabled := settings.AntiCall
	var dbCheck bool
	errDB := settingsDB.QueryRow("SELECT anti_call FROM bot_settings WHERE jid = ?", botJID).Scan(&dbCheck)
	if errDB == nil && dbCheck {
		isCallEnabled = true // اگر ڈیٹا بیس میں آن ہے، تو زبردستی آن کر دو!
	}

	// اگر آف ہے یا اپنا نمبر ہے تو لاگ پرنٹ کر کے واپس
	if !isCallEnabled || callerJID.User == botJID { 
		// fmt.Println("⚠️ [ANTI-CALL] Skipped: Anti-Call is OFF or Caller is Bot.")
		return 
	}

	// 3. واٹس میو سٹور سے سیو نمبر چیک کریں
	contact, err := client.Store.Contacts.GetContact(context.Background(), callerJID)
	isSaved := (err == nil && contact.Found && contact.FullName != "")

	// 🛑 ایکشن ٹائم!
	if !isSaved {
		fmt.Printf("📞 [ANTI-CALL] Triggered! Dropping call from Unsaved Number: %s\n", callerJID.User)

		// ⚡ 1. MILLISECOND DROP (فوراً کال کاٹیں)
		client.RejectCall(context.Background(), c.CallCreator, c.CallID)
		client.RejectCall(context.Background(), callerJID, c.CallID) // ڈبل فائر (Safety Backup)

		// ⚡ 2. وارننگ میسج (تاکہ واٹس ایپ کال کٹنے کا پراسیس مکمل کر لے)
		warning := "⚠️ *Silent Nexus Security*\n\nVoice/Video calls from unsaved numbers are automatically rejected. You are being blocked."
		client.SendMessage(context.Background(), callerJID, &waProto.Message{
			Conversation: proto.String(warning),
		})

		time.Sleep(1 * time.Second) // 1 سیکنڈ کا ڈیلے ضروری ہے ورنہ بلاک کی کمانڈ فیل ہو سکتی ہے

		// ⚡ 3. بلاک اور چیٹ ڈیلیٹ
		client.UpdateBlocklist(context.Background(), callerJID, events.BlocklistChangeActionBlock)
		
		patch := appstate.BuildDeleteChat(callerJID, time.Now(), nil, true)
		client.SendAppState(context.Background(), patch)
		
		fmt.Printf("✅ [ANTI-CALL] Successfully Blocked & Deleted: %s\n", callerJID.User)
	} else {
		// اگر واٹس ایپ اسے سیو نمبر مان رہا ہے تو ٹرمینل پر بتا دے گا
		fmt.Printf("ℹ️ [ANTI-CALL] Skipped: WhatsApp thinks %s is a SAVED contact.\n", callerJID.User)
	}
}

// ==========================================
// 🛡️ ANTI-DM LOGIC (Double Trigger: Block & Delete for JID + LID)
// ==========================================
// ==========================================
// 🛡️ ANTI-DM LOGIC (User's Proven Logic + SQLite)
// ==========================================
func handleAntiDMWatch(client *whatsmeow.Client, v *events.Message, settings BotSettings) bool {
	botJID := client.Store.ID.ToNonAD().User

	// 1. DIRECT DATABASE CHECK (SQLite)
	isEnabled := settings.AntiDM
	var dbCheck bool
	errDB := settingsDB.QueryRow("SELECT anti_dm FROM bot_settings WHERE jid = ?", botJID).Scan(&dbCheck)
	if errDB == nil && dbCheck {
		isEnabled = true 
	}

	// 2. بیسک فلٹرز
	if !isEnabled || v.Info.IsGroup || v.Info.IsFromMe || v.Info.Chat.Server == "newsletter" || v.Info.Chat.Server == types.NewsletterServer || isOwner(client, v) {
		return false
	}

	// =========================================================
	// 🟢 JID EXTRACTION LOGIC (تمہاری پرانی اور کامیاب لاجک)
	// =========================================================
	var realSender types.JID
	if v.Info.Sender.Server == types.HiddenUserServer {
		if !v.Info.SenderAlt.IsEmpty() {
			realSender = v.Info.SenderAlt.ToNonAD() 
		} else {
			realSender = v.Info.Sender.ToNonAD()
		}
	} else {
		realSender = v.Info.Sender.ToNonAD()
	}

	// 4. کانٹیکٹ چیک کریں
	contact, err := client.Store.Contacts.GetContact(context.Background(), realSender)
	isSaved := err == nil && contact.Found && contact.FullName != ""
	
	// 5. اگر نمبر سیو نہیں ہے (Unknown Number)
	if !isSaved {
		fmt.Printf("🛡️ [ANTI-DM] TRIGGERED [Bot: %s]: Unsaved number -> %s\n", botJID, realSender.User)
		
		warning := "⚠️ *Silent Nexus Security*\n\nDirect messages from unsaved numbers are not allowed. You are being blocked automatically."
		client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
			Conversation: proto.String(warning),
		})
		
		time.Sleep(1 * time.Second) // وارننگ جانے کا ویٹ

		// 🛑 بلاک کرنے کی کوشش (Dual Try - تمہاری لاجک)
		_, errBlock1 := client.UpdateBlocklist(context.Background(), v.Info.Sender.ToNonAD(), events.BlocklistChangeActionBlock)
		if errBlock1 != nil {
			_, errBlock2 := client.UpdateBlocklist(context.Background(), realSender, events.BlocklistChangeActionBlock)
			if errBlock2 == nil {
				fmt.Printf("✅ [ANTI-DM] Successfully blocked real number: %s\n", realSender.String())
			} else {
				fmt.Printf("❌ [ANTI-DM ERROR] Block failed: %v\n", errBlock2)
			}
		} else {
			fmt.Printf("✅ [ANTI-DM] Successfully blocked LID: %s\n", v.Info.Sender.String())
		}

		// 🛑 چیٹ ڈیلیٹ کریں (Dual Patch - تمہاری لاجک)
		lastMessageKey := &waCommon.MessageKey{
			RemoteJID: proto.String(v.Info.Chat.String()),
			FromMe:    proto.Bool(v.Info.IsFromMe),
			ID:        proto.String(v.Info.ID),
		}

		patchInfo1 := appstate.BuildDeleteChat(v.Info.Chat, v.Info.Timestamp, lastMessageKey, true)
		errPatch1 := client.SendAppState(context.Background(), patchInfo1)
		
		// اصلی نمبر کے لیے بھی ڈیلیٹ کی کمانڈ بھیجیں (بغیر میسج آئی ڈی کے)
		patchInfo2 := appstate.BuildDeleteChat(realSender, v.Info.Timestamp, nil, true)
		errPatch2 := client.SendAppState(context.Background(), patchInfo2)
		
		if errPatch1 == nil || errPatch2 == nil {
			fmt.Printf("✅ [ANTI-DM] Chat DELETED from WhatsApp screen for: %s\n", realSender.User)
		} else {
			fmt.Printf("❌ [ANTI-DM ERROR] Delete failed. Patch1: %v | Patch2: %v\n", errPatch1, errPatch2)
		}

		return true 
	}

	return false
}