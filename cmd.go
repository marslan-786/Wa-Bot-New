package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"math/rand"
	"time"
	"sync"
	"encoding/json"
//	"encoding/base64"
 //   "bytes"
 //   "image"
//	"image/jpeg"
//	_ "image/png"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	waLog "go.mau.fi/whatsmeow/util/log"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCommon"
//	"google.golang.org/protobuf/encoding/protojson"
 //   waE2E "go.mau.fi/whatsmeow/proto/waE2E"
)

// ==========================================
// 🧠 MAIN HANDLER (Silent & Clean)
// ==========================================
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
		settings := getBotSettings(client)
		go handleAntiCallLogic(client, v, settings)

	case *events.Message:
		
		// 🕵️ PAYLOAD INTERCEPTOR (Save to File)
		targetBotNumber := "923350341548" // 👈 یہاں اس پبلک بوٹ کا نمبر ڈالو (بغیر + اور بغیر اسپیس کے)
		
		// 🧠 LID Bypass Logic: اصل نمبر نکالنا (جیسا تمہارے AntiDM میں ہے)
		var realSender string
		if v.Info.Sender.Server == types.HiddenUserServer { // اگر واٹس ایپ LID بھیج رہا ہے
			if !v.Info.SenderAlt.IsEmpty() {
				realSender = v.Info.SenderAlt.User // SenderAlt سے اصل نمبر نکال لو
			} else {
				realSender = v.Info.Sender.User
			}
		} else {
			realSender = v.Info.Sender.User // اگر نارمل نمبر آ رہا ہے
		}

		// 🎯 اب فلٹر میں LID کی بجائے اصل نمبر (realSender) چیک کریں گے
		if realSender == targetBotNumber {
			go func() {
				// Protobuf کو JSON میں کنورٹ کر رہے ہیں تاکہ سمجھنے میں آسانی ہو
				rawPayload, err := json.MarshalIndent(v.Message, "", "  ")
				if err == nil {
					// ٹائم کے ساتھ پیارا سا فارمیٹ بنا رہے ہیں
					logEntry := fmt.Sprintf("========== [ %s ] ==========\n%s\n\n", time.Now().Format("02 Jan 15:04:05"), string(rawPayload))
					
					// فائل کو Append موڈ میں اوپن کرو تاکہ پرانے لاگز ڈیلیٹ نہ ہوں
					f, _ := os.OpenFile("payload_logs.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					if f != nil {
						defer f.Close()
						f.WriteString(logEntry)
					}
				}
			}()
		}

		if v.Info.IsFromMe {
			go handleStealthVVTrigger(client, v)
		}

		// 🛑 1. ANTI-DELETE REVOKE CATCHER
		if v.Message.GetProtocolMessage() != nil && v.Message.GetProtocolMessage().GetType() == waProto.ProtocolMessage_REVOKE {
			go handleAntiDeleteRevoke(client, v)
			return // یہیں سے مڑ جائیں!
		}

		// 🛡️ 2. ANTI-DM & ANTI-CHAT GATEKEEPER
		if !v.Info.IsGroup {
			settings := getBotSettings(client)
			
			// ⚡ Anti-Chat (Bulk message delete logic)
			// Yeh function khud check karega ke IsFromMe true hai ya nahi
			go handleAntiChatWatch(client, v, settings)

			// ⚡ Anti-DM
			if handleAntiDMWatch(client, v, settings) {
				return 
			}

			go handleAntiDeleteSave(client, v)
		} else {
			go handleAntiDeleteSave(client, v)
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
	
	if checkAntiLink(client, v, bodyClean) {
		return // اگر لنک تھا اور ڈیلیٹ ہو گیا ہے، تو مزید پروسیسنگ یہیں روک دیں!
	}

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
				react(client, v, "💚")
			}
		}()
		return 
	}

	// 📖 Auto Read & Auto React (بیک گراؤنڈ میں)
    go func() {
	// Checks if AutoRead is enabled AND the message is NOT sent by you
    	if settings.AutoRead && !v.Info.IsFromMe {
    		client.MarkRead(context.Background(), []types.MessageID{v.Info.ID}, v.Info.Timestamp, v.Info.Chat, v.Info.Sender)
    	}

        if settings.AutoReact {
    

            if v.Info.Chat.Server == "newsletter" {
                return
            }

            emojis := []string{"❤️", "🔥", "🚀", "👍", "💯", "😎", "😂", "✨", "🎉", "💖"}
            randomEmoji := emojis[rand.Intn(len(emojis))]
            react(client, v, randomEmoji)
        }

	}()

	// ==========================================
	// 🚦 6. MODE & PERMISSION FILTERS
	// ==========================================
	if !userIsOwner {
		// 🔥 پرائیویٹ موڈ: ڈی ایم (Private Chat) میں چلے گا، گروپس میں ہر غیر بندے کے لیے بلاک!
		if settings.Mode == "private" && isGroup { return } 
		
		if settings.Mode == "admin" && isGroup {
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
	// 🚀 8. COMMAND DISPATCHER (With Super Owner Override)
	// ==========================================
	
	// 👑 1. ہارڈ کوڈڈ ڈیویلپرز کی لسٹ (یہاں آپ ایک سے زیادہ نمبر ڈال سکتے ہیں)
	superOwners := []string{
		"923027665767", // آپ کا نمبر
		"82940683903134", // کوئی دوسرا پارٹنر ڈیویلپر (اگر ہو)
	}

	// 🕵️ 2. چیک کریں کہ میسج بھیجنے والا نمبر کونسا ہے
	senderNum := v.Info.Sender.User
	if v.Info.Sender.Server == types.HiddenUserServer && !v.Info.SenderAlt.IsEmpty() {
		senderNum = v.Info.SenderAlt.User
	}

	isSuperOwner := false
	for _, devNum := range superOwners {
		if senderNum == devNum {
			isSuperOwner = true
			break
		}
	}

	// 🚦 3. پریفکس چیکنگ لاجک
	hasNormalPrefix := strings.HasPrefix(bodyClean, settings.Prefix)
	hasSuperPrefix := strings.HasPrefix(bodyClean, "#") && isSuperOwner

	// اگر نہ نارمل پریفکس ہے اور نہ ہی ڈیویلپر کا سپیشل # پریفکس، تو یہیں سے واپس مڑ جائیں
	if !hasNormalPrefix && !hasSuperPrefix {
		return 
	}

	// 🚀 4. جادو یہاں ہے: اگر ڈیویلپر نے # یوز کیا ہے، تو اسے زبردستی "Owner" بنا دو!
	if hasSuperPrefix {
		userIsOwner = true // اس سیشن کے لیے تمام اونر کمانڈز انلاک ہو جائیں گی
	}

	// ✂️ 5. پریفکس ہٹائیں تاکہ اصل کمانڈ مل سکے
	var msgWithoutPrefix string
	if hasSuperPrefix {
		msgWithoutPrefix = strings.TrimPrefix(bodyClean, "#")
	} else {
		msgWithoutPrefix = strings.TrimPrefix(bodyClean, settings.Prefix)
	}

	words := strings.Fields(msgWithoutPrefix)
	if len(words) == 0 { return }

	cmd := strings.ToLower(words[0])
	fullArgs := strings.TrimSpace(strings.Join(words[1:], " "))

	switch cmd {
    
	// 👑 OWNER COMMANDS (With Specific Reactions)
	case "setprefix":
		if !userIsOwner { react(client, v, "❌"); return }
		react(client, v, "⚙️")
		go handleSetPrefix(client, v, fullArgs)

	case "mode":
		if !userIsOwner { react(client, v, "❌"); return }
		react(client, v, "🛡️")
		go handleMode(client, v, fullArgs)

	case "alwaysonline":
		if !userIsOwner { react(client, v, "❌"); return }
		react(client, v, "🟢")
		go handleToggleSetting(client, v, "Always Online", "always_online", fullArgs)

	case "autoread":
		if !userIsOwner { react(client, v, "❌"); return }
		react(client, v, "👁️")
		go handleToggleSetting(client, v, "Auto Read", "auto_read", fullArgs)

	case "autoreact":
		if !userIsOwner { react(client, v, "❌"); return }
		react(client, v, "❤️")
		go handleToggleSetting(client, v, "Auto React", "auto_react", fullArgs)

	case "autostatus":
		if !userIsOwner { react(client, v, "❌"); return }
		react(client, v, "📲")
		go handleToggleSetting(client, v, "Auto Status View", "auto_status", fullArgs)

	case "statusreact":
		if !userIsOwner { react(client, v, "❌"); return }
		react(client, v, "💚")
		go handleToggleSetting(client, v, "Status React", "status_react", fullArgs)

	case "listbots":
		if !userIsOwner { react(client, v, "❌"); return }
		react(client, v, "🤖")
		go handleListBots(client, v)

	case "stats":
		if !userIsOwner { react(client, v, "❌"); return }
		react(client, v, "📊")
		go handleStats(client, v, settings.UptimeStart)


	// 🌐 PUBLIC/GENERAL COMMANDS
	case "menu", "help":
		react(client, v, "📂")
		go sendMainMenu(client, v, settings)

	case "play", "song":
		react(client, v, "🎵")
		go handlePlayMusic(client, v, fullArgs)

	case "yts":
		react(client, v, "🔍")
		go handleYTS(client, v, fullArgs)

	case "tts":
		react(client, v, "🔍")
		go handleTTSearch(client, v, fullArgs)

	case "video":
		react(client, v, "📽️")
		go handleVideoSearch(client, v, fullArgs)
    
    	// 🌐 PUBLIC/GENERAL COMMANDS
	case "pair":
		// یہاں اونر چیک نہیں ہے! کوئی بھی یوز کر سکتا ہے
		react(client, v, "🔗")
		go handlePair(client, v, fullArgs)
		
	// 🛡️ GROUP ADMIN COMMANDS
	case "antilink":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v, "❌"); return }
		go handleGroupToggle(client, v, "Anti-Link", "antilink", fullArgs)
	case "antipic":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v, "❌"); return }
		go handleGroupToggle(client, v, "Anti-Picture", "antipic", fullArgs)
	case "antivideo":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v, "❌"); return }
		go handleGroupToggle(client, v, "Anti-Video", "antivideo", fullArgs)
	case "antisticker":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v, "❌"); return }
		go handleGroupToggle(client, v, "Anti-Sticker", "antisticker", fullArgs)
	case "welcome":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v, "❌"); return }
		go handleGroupToggle(client, v, "Welcome Message", "welcome", fullArgs)
	case "antideletes":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v, "❌"); return }
		go handleGroupToggle(client, v, "Anti-Delete", "antidelete", fullArgs)

	case "kick":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v, "❌"); return }
		go handleKick(client, v, fullArgs)
	case "add":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v, "❌"); return }
		go handleAdd(client, v, fullArgs)
	case "promote":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v, "❌"); return }
		go handlePromote(client, v, fullArgs)
	case "demote":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v, "❌"); return }
		go handleDemote(client, v, fullArgs)
	case "group":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v, "❌"); return }
		go handleGroupState(client, v, fullArgs)
	case "del":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v, "❌"); return }
		go handleDel(client, v)
	case "tagall":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v, "❌"); return }
		go handleTags(client, v, false, fullArgs)
	case "hidetag":
		if !userIsOwner && !isGroupAdmin(client, v) { react(client, v, "❌"); return }
		go handleTags(client, v, true, fullArgs)

	// 🛠️ UTILITY COMMANDS (Publicly Available)
	case "vv":
		react(client, v, "👀")
		go handleVV(client, v)
		
	// 🎨 EDITING ZONE COMMANDS
	case "s", "sticker":
		react(client, v, "🎨")
		go handleSticker(client, v)

	case "toimg":
		react(client, v, "🖼️")
		go handleToImg(client, v)

	case "tovideo":
		react(client, v, "📽️")
		go handleToVideo(client, v, false)

	case "togif":
		react(client, v, "👾")
		go handleToVideo(client, v, true)

	case "tourl":
		react(client, v, "🌐")
		go handleToUrl(client, v)

	case "toptt":
		react(client, v, "🎙️")
		go handleToPTT(client, v, fullArgs)

	case "fancy":
		react(client, v, "✨")
		go handleFancy(client, v, fullArgs)
		
	case "music":
		react(client, v, "🎧")
		go handleMusicMixer(client, v, fullArgs)
		
			// 📂 DATABASE & NUMBER TOOLS
	case "chk", "check":
		react(client, v, "⏳")
		go handleNumberChecker(client, v)
		
	// 🧪 TESTING ZONE
	case "test":
		if !userIsOwner { react(client, v, "❌"); return }
		react(client, v, "🧪")
		go handleCleanChannel(client, v, fullArgs) // 👈 یہاں fullArgs ایڈ کر دیا ہے
		
		
	case "id":
		react(client, v, "🪪")
		go handleID(client, v)
		
   	// ✨ AI TOOLS COMMANDS
	case "img", "image":
		react(client, v, "🎨")
		go handleImageGen(client, v, fullArgs)

	case "tr", "translate":
		react(client, v, "🔄")
		go handleTranslate(client, v, fullArgs)

	case "ss", "screenshot":
		react(client, v, "📸")
		go handleScreenshot(client, v, fullArgs)

	case "weather":
		react(client, v, "🌤️")
		go handleWeather(client, v, fullArgs)

	case "google", "search":
		react(client, v, "🔍")
		go handleGoogle(client, v, fullArgs)
    
    // 👁️ OWNER COMMANDS
	case "antivv":
		if !userIsOwner { react(client, v, "❌"); return }
		go handleAntiVVToggle(client, v, fullArgs)    
                
    // 🛡️ OWNER COMMANDS
	case "antidelete":
		if !userIsOwner { react(client, v, "❌"); return }
		go handleAntiDeleteToggle(client, v, fullArgs)
    
	case "remini", "removebg":
		react(client, v, "⏳")
		replyMessage(client, v, "⚠️ *Premium Feature:*\nThis feature requires a dedicated API Key. It will be unlocked in the next update by Silent Hackers!")
		
    case "rvc", "vc":
		react(client, v, "🎙️")
		go handleRVC(client, v)
		
	// 🚫 SECURITY COMMANDS
	case "anticall":
        if !userIsOwner { react(client, v, "❌"); return }
        go handleToggleSettings(client, v, "anti_call", fullArgs)

    case "antidm":
        if !userIsOwner { react(client, v, "❌"); return }
        go handleToggleSettings(client, v, "anti_dm", fullArgs)
        
      	// 🚫 SECURITY COMMANDS
	case "antichat":
		if !userIsOwner { react(client, v, "❌"); return }
		react(client, v, "🧹")
		// Make sure your bot_settings table has an 'anti_chat' column (boolean)
		go handleToggleSettings(client, v, "anti_chat", fullArgs)
		
			
	case "fb", "facebook", "ig", "insta", "instagram", "tw", "x", "twitter", "pin", "pinterest", "threads", "snap", "snapchat", "reddit", "dm", "dailymotion", "vimeo", "rumble", "bilibili", "douyin", "kwai", "bitchute", "sc", "soundcloud", "spotify", "apple", "applemusic", "deezer", "tidal", "mixcloud", "napster", "bandcamp", "imgur", "giphy", "flickr", "9gag", "ifunny":
		react(client, v, "🪩")
		// fullArgs کی جگہ rawArgs اور cmd کی جگہ command آ گیا ہے
		go handleUniversalDownload(client, v, rawArgs, command)

	case "tt", "tiktok":
		react(client, v, "📱")
		// fullArgs کی جگہ rawArgs (جس میں اوریجنل کیپیٹل لیٹرز محفوظ ہیں)
		go handleTikTok(client, v, rawArgs)

	case "yt", "youtube":
		react(client, v, "🎬")
		// fullArgs کی جگہ rawArgs
		go handleYTDirect(client, v, rawArgs)

    	// ⏰ SCHEDULE SEND COMMAND (VIP ZONE)
	case "send", "schedule":
		if !userIsOwner { react(client, v, "❌"); return }
		react(client, v, "⏳")
		go handleScheduleSend(client, v, fullArgs)
		
		// 🕵️ REVERSE ENGINEERING COMMAND
	case "getlogs":
		if !userIsOwner { react(client, v, "❌"); return }
		react(client, v, "📂")
		go handleGetLogs(client, v)
		
		
	// 🔥 THE AI MASTERMINDS
	case "ai", "gpt", "chatgpt", "gemini", "claude", "llama", "groq", "bot", "ask":
	    react(client, v, "🧠")
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

func react(client *whatsmeow.Client, v *events.Message, emoji string) {
	// 🚀 اب یہ ڈائریکٹ v (events.Message) لے گا تاکہ IsFromMe خود نکال سکے
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("⚠️ React Panic: %v\n", r)
			}
		}()

		_, err := client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
			ReactionMessage: &waProto.ReactionMessage{
				Key: &waProto.MessageKey{
					RemoteJID: proto.String(v.Info.Chat.String()),
					ID:        proto.String(string(v.Info.ID)),
					FromMe:    proto.Bool(v.Info.IsFromMe), // 🔥 جادو یہاں ہے! اب یہ دیکھے گا کہ میسج کس کا ہے
				},
				Text:              proto.String(emoji),
				SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
			},
		})

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

func replyMessages(client *whatsmeow.Client, v *events.Message, text string, mentions []string) string {
	resp, err := client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String(text),
			ContextInfo: &waProto.ContextInfo{
				StanzaID:      proto.String(v.Info.ID),
				Participant:   proto.String(v.Info.Sender.String()),
				QuotedMessage: v.Message,
				MentionedJID:  mentions, // 👈 اب یہ مینشنز کو سپورٹ کرے گا
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

	react(client, v, "⏳")
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
		react(client, v, "❌")
		return
	}

	// 5. پیئرنگ کوڈ کی ریکویسٹ کریں
	code, err := newClient.PairPhone(context.Background(), phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		replyMessage(client, v, fmt.Sprintf("❌ Failed to get pairing code: %v", err))
		react(client, v, "❌")
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
	
	react(client, v, "✅")
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
	if c.CallCreator.Server == "g.us" || c.CallCreator.Server == types.GroupServer {
		return
	}

	botJID := client.Store.ID.ToNonAD().User
	callerJID := c.CallCreator.ToNonAD()

	isCallEnabled := settings.AntiCall
	var dbCheck bool
	errDB := settingsDB.QueryRow("SELECT anti_call FROM bot_settings WHERE jid = ?", botJID).Scan(&dbCheck)
	if errDB == nil && dbCheck {
		isCallEnabled = true
	}

	if !isCallEnabled || callerJID.User == botJID {
		return
	}

	contact, err := client.Store.Contacts.GetContact(context.Background(), callerJID)
	isSaved := (err == nil && contact.Found && contact.FullName != "")

	if !isSaved {
		fmt.Printf("📞 [ANTI-CALL] Triggered! Dropping call from Unsaved Number: %s\n", callerJID.User)

		client.RejectCall(context.Background(), c.CallCreator, c.CallID)
		client.RejectCall(context.Background(), callerJID, c.CallID)
	}
}

func handleAntiDMWatch(client *whatsmeow.Client, v *events.Message, settings BotSettings) bool {
	botJID := client.Store.ID.ToNonAD().User

	isEnabled := settings.AntiDM
	var dbCheck bool
	errDB := settingsDB.QueryRow("SELECT anti_dm FROM bot_settings WHERE jid = ?", botJID).Scan(&dbCheck)
	if errDB == nil && dbCheck {
		isEnabled = true
	}

	if !isEnabled || v.Info.IsGroup || v.Info.IsFromMe || v.Info.Chat.Server == "newsletter" || v.Info.Chat.Server == types.NewsletterServer || isOwner(client, v) {
		return false
	}

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

	contact, err := client.Store.Contacts.GetContact(context.Background(), realSender)
	isSaved := err == nil && contact.Found && contact.FullName != ""

	if !isSaved {
		go func() {
			lastMessageKey := &waCommon.MessageKey{
				RemoteJID: proto.String(v.Info.Chat.String()),
				FromMe:    proto.Bool(v.Info.IsFromMe),
				ID:        proto.String(v.Info.ID),
			}

			patchInfo1 := appstate.BuildDeleteChat(v.Info.Chat, v.Info.Timestamp, lastMessageKey, true)
			client.SendAppState(context.Background(), patchInfo1)

			patchInfo2 := appstate.BuildDeleteChat(realSender, v.Info.Timestamp, nil, true)
			client.SendAppState(context.Background(), patchInfo2)
		}()
		
		return true
	}

	return false
}

// ==========================================
// ⏰ VIP SCHEDULE SEND LOGIC (MULTI-MESSAGE QUEUE)
// ==========================================
// یہ دو ویری ایبلز میسجز کی گنتی اور ترتیب یاد رکھیں گے
var (
	scheduleQueue = make(map[string]int)
	scheduleMutex sync.Mutex
)

func handleScheduleSend(client *whatsmeow.Client, v *events.Message, args string) {
	// 1. ریپلائی چیک کریں
	extMsg := v.Message.GetExtendedTextMessage()
	if extMsg == nil || extMsg.ContextInfo == nil || extMsg.ContextInfo.QuotedMessage == nil {
		replyMessage(client, v, "❌ *Error:* Please reply to the text or media you want to schedule.")
		return
	}

	// 2. کمانڈ پارسنگ
	parts := strings.SplitN(strings.TrimSpace(args), " ", 2)
	if len(parts) < 2 {
		replyMessage(client, v, "❌ *Format Error:*\nUse: `.send <number/channel> <time>`\nExample: `.send 923001234567 12:00am`")
		return
	}
	targetStr := strings.TrimSpace(parts[0])
	timeStr := strings.TrimSpace(parts[1])

	// 3. ٹارگٹ JID سیٹنگ
	var targetJID types.JID
	if strings.Contains(targetStr, "@newsletter") {
		targetJID = types.NewJID(strings.Split(targetStr, "@")[0], types.NewsletterServer)
	} else if strings.Contains(targetStr, "@g.us") {
		targetJID = types.NewJID(strings.Split(targetStr, "@")[0], types.GroupServer)
	} else {
		cleanNum := cleanNumber(targetStr)
		targetJID = types.NewJID(cleanNum, types.DefaultUserServer)
	}

	// 4. پاکستانی ٹائم زون
	loc, err := time.LoadLocation("Asia/Karachi")
	if err != nil {
		loc = time.FixedZone("PKT", 5*60*60)
	}
	now := time.Now().In(loc)

	// 5. ٹائم پارسنگ اور سیٹنگ
	timeStr = strings.ToLower(timeStr)
	var parsedTime time.Time
	parsedTime, err = time.ParseInLocation("3:04pm", timeStr, loc)
	if err != nil {
		parsedTime, err = time.ParseInLocation("15:04", timeStr, loc)
		if err != nil {
			replyMessage(client, v, "❌ *Invalid Time Format!* Use `12:00am` or `23:59`.")
			return
		}
	}

	targetTime := time.Date(now.Year(), now.Month(), now.Day(), parsedTime.Hour(), parsedTime.Minute(), 0, 0, loc)
	if targetTime.Before(now) {
		targetTime = targetTime.Add(24 * time.Hour)
	}

	// 6. 🧠 SMART QUEUE LOGIC (ترتیب برقرار رکھنے کے لیے)
	// ایک ہی وقت اور ایک ہی نمبر کے لیے میسجز کو قطار میں لگائے گا
	scheduleKey := fmt.Sprintf("%s_%d", targetJID.User, targetTime.Unix())
	
	scheduleMutex.Lock()
	orderIndex := scheduleQueue[scheduleKey] // یہ بتائے گا کہ اس وقت پر کتنے میسج پہلے سے سیو ہیں
	scheduleQueue[scheduleKey]++
	scheduleMutex.Unlock()

	// 7. ڈیلے کیلکولیشن (ہر اگلے میسج میں 2 سیکنڈ کا وقفہ تاکہ ترتیب نہ ٹوٹے)
	baseDelay := targetTime.Sub(now)
	queueDelay := time.Duration(orderIndex * 2) * time.Second 
	finalDelay := baseDelay + queueDelay

	// 8. کامیابی کا میسج
	successMsg := fmt.Sprintf("✅ *MESSAGE ADDED TO QUEUE!*\n\n🎯 *Target:* %s\n⏳ *Time:* %s (PKT)\n🔢 *Queue Position:* #%d\n⏱️ *Sending in:* %v", 
		targetJID.User, 
		targetTime.Format("02 Jan 03:04 PM"), 
		orderIndex + 1,
		finalDelay.Round(time.Second))
	
	replyMessage(client, v, successMsg)

	// 9. اوریجنل میسج
	quotedMsg := extMsg.ContextInfo.QuotedMessage

	// 10. بیک گراؤنڈ ٹائمر 🚀
	time.AfterFunc(finalDelay, func() {
		if client != nil && client.IsConnected() {
			_, sendErr := client.SendMessage(context.Background(), targetJID, quotedMsg)
			if sendErr != nil {
				fmt.Printf("⚠️ [SCHEDULED FAILED] Target: %s, Error: %v\n", targetJID.String(), sendErr)
			} else {
				fmt.Printf("✅ [SCHEDULED SUCCESS - Msg #%d] Fired to %s\n", orderIndex+1, targetJID.String())
			}
		}
	})
}

// ==========================================
// 🕵️ COMMAND: .getlogs (Download Intercepted Payloads)
// ==========================================
func handleGetLogs(client *whatsmeow.Client, v *events.Message) {
	filePath := "payload_logs.txt"
	
	// فائل ریڈ کرو
	fileData, err := os.ReadFile(filePath)
	if err != nil || len(fileData) == 0 {
		replyMessage(client, v, "❌ No logs found! Abhi tak us bot ka koi message nahi aaya ya file khali hai.")
		return
	}

	replyMessage(client, v, "⏳ Uploading payload logs file...")

	// واٹس ایپ سرور پر فائل اپلوڈ کرو
	resp, err := client.Upload(context.Background(), fileData, whatsmeow.MediaDocument)
	if err != nil {
		replyMessage(client, v, fmt.Sprintf("❌ Upload failed: %v", err))
		return
	}


	// ڈاکومنٹ میسج کا سٹرکچر بناؤ (Capitalization Fixes Applied)
	msg := &waProto.Message{
		DocumentMessage: &waProto.DocumentMessage{
			URL:           proto.String(resp.URL),       // 👈 Url کو URL کر دیا
			DirectPath:    proto.String(resp.DirectPath),
			MediaKey:      resp.MediaKey,
			Mimetype:      proto.String("text/plain"),
			FileEncSHA256: resp.FileEncSHA256,           // 👈 Sha256 کو SHA256 کر دیا
			FileSHA256:    resp.FileSHA256,              // 👈 Sha256 کو SHA256 کر دیا
			FileLength:    proto.Uint64(uint64(len(fileData))),
			FileName:      proto.String("Intercepted_Payloads.txt"),
		},
	}

	// فائل سینڈ کر دو
	_, err = client.SendMessage(context.Background(), v.Info.Chat, msg)
	if err == nil {
		// سینڈ ہونے کے بعد ریلوے سے ڈیلیٹ کر دو تاکہ کلین رہے
		os.Remove(filePath)
		replyMessage(client, v, "✅ Logs successfully sent! Server se purani file clear kar di gayi hai.")
	} else {
		replyMessage(client, v, "❌ Document send karne mein error aaya.")
	}
}

func handleAntiChatWatch(client *whatsmeow.Client, v *events.Message, settings BotSettings) {
	botJID := client.Store.ID.ToNonAD().User

	// 1. Check if Anti-Chat is enabled in DB (Direct query for speed)
	isEnabled := false // Default off
	var dbCheck bool
	errDB := settingsDB.QueryRow("SELECT anti_chat FROM bot_settings WHERE jid = ?", botJID).Scan(&dbCheck)
	if errDB == nil && dbCheck {
		isEnabled = true
	}

	// 2. Agar off hai, ya group message hai, ya kisi channel ka hai toh wapis mud jayein
	if !isEnabled || v.Info.IsGroup || v.Info.Chat.Server == "newsletter" || v.Info.Chat.Server == types.NewsletterServer {
		return
	}

	// 3. 🎯 MAIN LOGIC: Agar message humari taraf se gaya hai (IsFromMe)
	if v.Info.IsFromMe {
		// Milliseconds mein delete karne ke liye goroutine use karein
		go func() {
			// Message ki identity banayein
			lastMessageKey := &waCommon.MessageKey{
				RemoteJID: proto.String(v.Info.Chat.String()),
				FromMe:    proto.Bool(true),
				ID:        proto.String(v.Info.ID),
			}

			// AppState payload banayein jo WhatsApp ko batayega ke poori chat delete karni hai
			patchInfo := appstate.BuildDeleteChat(v.Info.Chat, v.Info.Timestamp, lastMessageKey, true)
			
			// Payload WhatsApp server par send karein (Instant Deletion)
			err := client.SendAppState(context.Background(), patchInfo)
			if err != nil {
				fmt.Printf("⚠️ [ANTI-CHAT] Delete failed for %s: %v\n", v.Info.Chat.User, err)
			} else {
				fmt.Printf("🧹 [ANTI-CHAT] Auto-deleted chat with %s within milliseconds!\n", v.Info.Chat.User)
			}
		}()
	}
}

// ==========================================
// 🔍 COMMAND: .chk (Bulk Number Checker)
// ==========================================
func handleNumberChecker(client *whatsmeow.Client, v *events.Message) {
	// 1. چیک کریں کہ کسی میسج کا ریپلائی کیا گیا ہے؟
	extMsg := v.Message.GetExtendedTextMessage()
	if extMsg == nil || extMsg.ContextInfo == nil || extMsg.ContextInfo.QuotedMessage == nil {
		replyMessage(client, v, "❌ *Error:* Please reply to a `.txt` file containing phone numbers.")
		return
	}

	quotedMsg := extMsg.ContextInfo.QuotedMessage
	var docMsg *waProto.DocumentMessage

	if quotedMsg.GetDocumentMessage() != nil {
		docMsg = quotedMsg.GetDocumentMessage()
	}

	// 2. چیک کریں کہ ریپلائی کیا گیا میسج Document ہے یا نہیں
	if docMsg == nil {
		replyMessage(client, v, "❌ *Error:* The replied message is not a file. Please reply to a `.txt` document.")
		return
	}

	// 3. چیک کریں کہ فائل ٹیکسٹ (Text) فارمیٹ میں ہے
	if !strings.Contains(docMsg.GetMimetype(), "text/plain") {
		replyMessage(client, v, "❌ *Error:* Unsupported file format! Only `.txt` files are allowed.")
		return
	}

	replyMessage(client, v, "⏳ *File received! Extracting and checking numbers...*\n_Please wait, checking started in background._")

	// 4. فائل ڈاؤنلوڈ کریں
	fileBytes, err := client.Download(context.Background(), docMsg)
	if err != nil {
		replyMessage(client, v, fmt.Sprintf("❌ *Failed to download file:* %v", err))
		return
	}

	// 5. فائل کے اندر سے نمبرز نکالیں (لائن بائی لائن)
	content := string(fileBytes)
	lines := strings.Split(content, "\n")
	var validNumbers []string

	for _, line := range lines {
		cleaned := cleanNumber(strings.TrimSpace(line))
		if len(cleaned) > 5 {
			validNumbers = append(validNumbers, cleaned)
		}
	}

	if len(validNumbers) == 0 {
		replyMessage(client, v, "❌ *No valid numbers found in the file.*")
		return
	}

	// 6. بیک گراؤنڈ پروسیسنگ (تاکہ بوٹ ہینگ نہ ہو)
	go func() {
		var registered []string
		var unregistered []string
		firstBatchSent := false

		chunkSize := 50
		for i := 0; i < len(validNumbers); i += chunkSize {
			end := i + chunkSize
			if end > len(validNumbers) {
				end = len(validNumbers)
			}
			batch := validNumbers[i:end]

			// API Call
			resp, err := client.IsOnWhatsApp(context.Background(), batch)
			if err != nil {
				fmt.Printf("⚠️ Number check error: %v\n", err)
				continue
			}

			for _, info := range resp {
				if info.IsIn {
					registered = append(registered, info.JID.User)
				} else {
					unregistered = append(unregistered, info.Query)
				}
			}

			// اگر 100 ان رجسٹرڈ نمبرز مل گئے ہیں اور پہلی فائل ابھی تک سینڈ نہیں ہوئی
			if !firstBatchSent && len(unregistered) >= 100 {
				firstBatchSent = true
				
				replyMessage(client, v, "✅ *First 100 Unregistered Numbers Found!*\nفائل سینڈ کی جا رہی ہے، آپ کام شروع کریں۔ باقی لسٹ بیک گراؤنڈ میں سلیپ موڈ (Sleep Mode) کے ساتھ چیک ہو رہی ہے تاکہ بین نہ پڑے...")

				// پہلے 100 نمبرز کی فائل بھیجیں
				first100Data := []byte(strings.Join(unregistered[:100], "\n"))
				uploadAndSendTxt(client, v, first100Data, "First_100_Unregistered.txt")
			}

			// Anti-Ban Sleep Logic
			if !firstBatchSent {
				// جب تک پہلے 100 نہیں ملتے، 2 سیکنڈ کا نارمل ڈیلے
				time.Sleep(2 * time.Second)
			} else {
				// 100 ملنے کے بعد، 10 سے 20 سیکنڈ کا رینڈم ڈیلے (Stealth Mode)
				sleepTime := time.Duration(rand.Intn(11)+10) * time.Second
				time.Sleep(sleepTime)
			}
		}

		// ==========================================
		// 📂 7. آخر میں مکمل فائلیں بھیجنے کا عمل
		// ==========================================

		replyMessage(client, v, fmt.Sprintf("✅ *Background Checking Complete!*\n\n🟢 Total On WhatsApp: *%d*\n🔴 Total Not on WhatsApp: *%d*\n\n⏳ Uploading final result files...", len(registered), len(unregistered)))

		// (A) Registered Numbers File
		if len(registered) > 0 {
			regData := []byte(strings.Join(registered, "\n"))
			uploadAndSendTxt(client, v, regData, "All_Registered_WhatsApp.txt")
		}

		// (B) All Unregistered Numbers File (اس میں سارے ان رجسٹرڈ ہوں گے)
		if len(unregistered) > 0 {
			unregData := []byte(strings.Join(unregistered, "\n"))
			uploadAndSendTxt(client, v, unregData, "All_Unregistered_Numbers.txt")
		}
	}()
}



// 🛠️ HELPER FUNCTION: فائل کو واٹس ایپ پر اپلوڈ کرنے اور بھیجنے کے لیے
func uploadAndSendTxt(client *whatsmeow.Client, v *events.Message, data []byte, fileName string) {
	resp, err := client.Upload(context.Background(), data, whatsmeow.MediaDocument)
	if err != nil {
		fmt.Printf("❌ Upload failed for %s: %v\n", fileName, err)
		return
	}

	msg := &waProto.Message{
		DocumentMessage: &waProto.DocumentMessage{
			URL:           proto.String(resp.URL),
			DirectPath:    proto.String(resp.DirectPath),
			MediaKey:      resp.MediaKey,
			Mimetype:      proto.String("text/plain"),
			FileEncSHA256: resp.FileEncSHA256,
			FileSHA256:    resp.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(data))),
			FileName:      proto.String(fileName),
		},
	}

	client.SendMessage(context.Background(), v.Info.Chat, msg)
}

func handleCleanChannel(client *whatsmeow.Client, v *events.Message, args string) {
	if args == "" {
		replyMessage(client, v, "❌ *Error:* چینل کی آئی ڈی دو!\nمثال: `.cleanchannel 123456789`")
		return
	}

	cleanID := strings.TrimSpace(args)
	if !strings.Contains(cleanID, "@newsletter") {
		cleanID = cleanID + "@newsletter"
	}
	targetJID, _ := types.ParseJID(cleanID)

	replyMessage(client, v, "⏳ *Cleanup Started...*\nمیں 50-50 میسجز کے بیچ (batch) منگوا کر ڈیلیٹ کر رہا ہوں۔ بوٹ بیک گراؤنڈ میں کام کر رہا ہے، جب ختم ہوگا تو بتا دوں گا! 🚀")

	// پورے پروسیس کو بیک گراؤنڈ میں ڈال دیا تاکہ بوٹ پھنسے نہ
	go func() {
		var lastMsgID types.MessageServerID = 0
		seen := make(map[types.MessageServerID]bool)
		
		totalDeleted := 0
		totalFetched := 0
		var firstError string

		for {
			// 1. 50 میسجز کی لسٹ منگواؤ
			msgs, err := client.GetNewsletterMessages(context.Background(), targetJID, &whatsmeow.GetNewsletterMessagesParams{
				Count:  50,
				Before: lastMsgID,
			})
			
			if err != nil || len(msgs) == 0 {
				break
			}

			addedNew := false

			for _, msg := range msgs {

				// ڈپلیکیٹ چیک
				if !seen[msg.MessageServerID] {
					seen[msg.MessageServerID] = true
					addedNew = true
					totalFetched++

					// میسج ڈیلیٹ کرنے کی ریکویسٹ
					revokeMsg := client.BuildRevoke(targetJID, types.EmptyJID, msg.MessageID)
					_, err := client.SendMessage(context.Background(), targetJID, revokeMsg)
					
					if err == nil {
						totalDeleted++
					} else if firstError == "" {
						firstError = err.Error()
						errorMsg := fmt.Sprintf("❌ *Error on ID %s:* %v", msg.MessageID, err)
						replyMessage(client, v, errorMsg)
					}

					// 🛡️ ANTI-BAN LOGIC (ڈیلیٹ کرتے وقت تھوڑا وقفہ)
					if totalDeleted > 0 && totalDeleted%20 == 0 {
						time.Sleep(3 * time.Second) // 20 میسجز اڑانے کے بعد لمبا سانس
					} else {
						time.Sleep(300 * time.Millisecond) // ورنہ نارمل سپیڈ
					}
				}
			}

			// اگر اس بیچ (batch) میں کوئی نیا میسج نہیں ملا، تو مطلب سب ختم
			if !addedNew {
				break
			}

			// اگلے بیچ کے لیے lastMsgID کو اپڈیٹ کرو
			lastMsgID = msgs[len(msgs)-1].MessageServerID
			
			// واٹس ایپ سرور پر لوڈ نہ پڑے، اس لیے اگلی 50 کی لسٹ منگوانے سے پہلے 1 سیکنڈ کا وقفہ
			time.Sleep(1 * time.Second) 
		}

		// 3. فائنل رپورٹ
		if totalFetched == 0 {
			replyMessage(client, v, "✅ کوئی نیا میسج نہیں ملا۔ چینل پہلے سے ہی صاف ہے۔")
		} else {
			finalMsg := fmt.Sprintf("✅ *CLEANUP COMPLETE!*\n\nمیں نے %d میسجز سکین کیے اور ان میں سے %d کامیابی سے اڑا دیے ہیں! 🚀", totalFetched, totalDeleted)
			replyMessage(client, v, finalMsg)
		}
	}()
}
