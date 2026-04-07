package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// ==========================================
// 🛠️ DATABASE INIT (Personal Log Table)
// ==========================================
func initPersonalLogDB() {
	query := `CREATE TABLE IF NOT EXISTS personal_log_settings (
		bot_jid TEXT PRIMARY KEY,
		anti_delete_group TEXT DEFAULT '',
		anti_vv_group TEXT DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS message_cache (
		msg_id TEXT PRIMARY KEY,
		sender_jid TEXT,
		msg_content BLOB,
		timestamp INTEGER
	);`
	_, err := settingsDB.Exec(query)
	if err != nil {
		fmt.Printf("❌ [DB INIT ERROR] Failed to create personal_log_settings: %v\n", err)
	}
}

// ==========================================
// 🛡️ TOGGLES: Anti-Delete & Anti-VV
// ==========================================
func handleAntiDeleteToggle(client *whatsmeow.Client, v *events.Message, args string) {
	initPersonalLogDB()
	if !v.Info.IsGroup {
		replyMessage(client, v, "❌ *Error:* Please use this command inside your intended 'Log Group'.")
		return
	}
	args = strings.ToLower(strings.TrimSpace(args))
	if args != "on" && args != "off" {
		replyMessage(client, v, "❌ Use: `.antidelete on` or `.antidelete off`")
		return
	}
	
	botJID := client.Store.ID.ToNonAD().User
	chatJID := v.Info.Chat.ToNonAD().String()
	
	_, errInsert := settingsDB.Exec("INSERT OR IGNORE INTO personal_log_settings (bot_jid) VALUES (?)", botJID)
	if errInsert != nil {
		replyMessage(client, v, "❌ *System Error:* Could not initialize database row.")
		return
	}

	var currentGroup string
	err := settingsDB.QueryRow("SELECT anti_delete_group FROM personal_log_settings WHERE bot_jid = ?", botJID).Scan(&currentGroup)
	if err != nil { currentGroup = "" }

	if args == "on" {
		if currentGroup == chatJID {
			replyMessage(client, v, "⚠️ *Already ON:* This is already your personal Log Group for Anti-Delete.")
			return
		}
		
		_, errUpdate := settingsDB.Exec("UPDATE personal_log_settings SET anti_delete_group = ? WHERE bot_jid = ?", chatJID, botJID)
		if errUpdate != nil {
			replyMessage(client, v, fmt.Sprintf("❌ *Database Error:* %v", errUpdate))
			return
		}
		
		react(client, v.Info.Chat, v.Info.ID, "✅")
		replyMessage(client, v, "✅ *Personal Log Group Activated!* Private deleted messages will now be forwarded here.")
		
	} else if args == "off" {
		if currentGroup == "" {
			replyMessage(client, v, "⚠️ *Already OFF:* Anti-Delete logging is not active right now.")
			return
		} else if currentGroup != chatJID {
			replyMessage(client, v, "⚠️ *Error:* You can only turn this OFF from the exact Log Group where you turned it ON.")
			return
		}
		
		_, errUpdate := settingsDB.Exec("UPDATE personal_log_settings SET anti_delete_group = '' WHERE bot_jid = ?", botJID)
		if errUpdate != nil {
			replyMessage(client, v, "❌ *Database Error:* Could not update settings.")
			return
		}
		
		react(client, v.Info.Chat, v.Info.ID, "✅")
		replyMessage(client, v, "❌ *Personal Log Group Deactivated!* Anti-Delete forwarding is now OFF.")
	}
}

func handleAntiVVToggle(client *whatsmeow.Client, v *events.Message, args string) {
	initPersonalLogDB()
	if !v.Info.IsGroup {
		replyMessage(client, v, "❌ *Error:* Please use this command inside your intended 'Log Group'.")
		return
	}
	args = strings.ToLower(strings.TrimSpace(args))
	if args != "on" && args != "off" {
		replyMessage(client, v, "❌ Use: `.antivv on` or `.antivv off`")
		return
	}
	
	botJID := client.Store.ID.ToNonAD().User
	chatJID := v.Info.Chat.ToNonAD().String()
	
	settingsDB.Exec("INSERT OR IGNORE INTO personal_log_settings (bot_jid) VALUES (?)", botJID)

	var currentGroup string
	err := settingsDB.QueryRow("SELECT anti_vv_group FROM personal_log_settings WHERE bot_jid = ?", botJID).Scan(&currentGroup)
	if err != nil { currentGroup = "" }

	if args == "on" {
		if currentGroup == chatJID {
			replyMessage(client, v, "⚠️ *Already ON:* This is already your personal Log Group for Anti-VV.")
			return
		}
		
		_, errUpdate := settingsDB.Exec("UPDATE personal_log_settings SET anti_vv_group = ? WHERE bot_jid = ?", chatJID, botJID)
		if errUpdate != nil {
			replyMessage(client, v, "❌ *Database Error!*")
			return
		}
		
		react(client, v.Info.Chat, v.Info.ID, "✅")
		replyMessage(client, v, "✅ *Personal Log Group Activated!* View-Once private media will be forwarded here.")
		
	} else if args == "off" {
		if currentGroup == "" {
			replyMessage(client, v, "⚠️ *Already OFF:* Anti-VV logging is not active right now.")
			return
		} else if currentGroup != chatJID {
			replyMessage(client, v, "⚠️ *Error:* You can only turn this OFF from the exact Log Group where you turned it ON.")
			return
		}
		
		settingsDB.Exec("UPDATE personal_log_settings SET anti_vv_group = '' WHERE bot_jid = ?", botJID)
		react(client, v.Info.Chat, v.Info.ID, "✅")
		replyMessage(client, v, "❌ *Personal Log Group Deactivated!* Anti-VV forwarding is now OFF.")
	}
}

// ==========================================
// 📥 CACHE SAVER (For Anti-Delete)
// ==========================================
func handleAntiDeleteSave(client *whatsmeow.Client, v *events.Message) {
	if v.Info.IsGroup || v.Message == nil || v.Info.IsFromMe { return }

	botJID := client.Store.ID.ToNonAD().User
	var logGroup string
	err := settingsDB.QueryRow("SELECT anti_delete_group FROM personal_log_settings WHERE bot_jid = ?", botJID).Scan(&logGroup)
	if err != nil || logGroup == "" { return }

	msgBytes, err := proto.Marshal(v.Message)
	if err == nil {
		settingsDB.Exec("INSERT OR REPLACE INTO message_cache (msg_id, sender_jid, msg_content, timestamp) VALUES (?, ?, ?, ?)", 
			v.Info.ID, v.Info.Sender.String(), msgBytes, v.Info.Timestamp.Unix())
	}
}

// ==========================================
// 🚫 ANTI-DELETE REVOKE HANDLER (Updated Reply Logic)
// ==========================================
func handleAntiDeleteRevoke(client *whatsmeow.Client, v *events.Message) {
	if v.Info.IsGroup || v.Info.IsFromMe { return }

	botJID := client.Store.ID.ToNonAD().User
	botFullJID := client.Store.ID.ToNonAD().String()
	
	var logGroup string
	err := settingsDB.QueryRow("SELECT anti_delete_group FROM personal_log_settings WHERE bot_jid = ?", botJID).Scan(&logGroup)
	if err != nil || logGroup == "" { return }

	targetJID, _ := types.ParseJID(logGroup)
	deletedMsgID := v.Message.GetProtocolMessage().GetKey().GetID()
	senderJID := v.Info.Sender.ToNonAD().User

	var rawMsg []byte
	var msgTimestamp int64
	err = settingsDB.QueryRow("SELECT msg_content, timestamp FROM message_cache WHERE msg_id = ?", deletedMsgID).Scan(&rawMsg, &msgTimestamp)
	if err != nil { return } // میسج کیشے میں نہیں ملا

	var originalMsg waProto.Message
	proto.Unmarshal(rawMsg, &originalMsg)

	// 1️⃣ سب سے پہلے اصلی میسج لاگ گروپ میں بھیجیں
	resp, sendErr := client.SendMessage(context.Background(), targetJID, &originalMsg)
	
	if sendErr == nil {
		// 2️⃣ اب اس میسج کو ریپلائی کر کے کارڈ بھیجیں
		loc, _ := time.LoadLocation("Asia/Karachi")
		sentTime := time.Unix(msgTimestamp, 0).In(loc).Format("02 Jan 2006, 03:04 PM")
		deletedTime := time.Now().In(loc).Format("02 Jan 2006, 03:04 PM")
		cleanSender := strings.Split(senderJID, "@")[0]

		warningText := fmt.Sprintf(`❖ ── ✦ 🚫 𝗣𝗥𝗜𝗩𝗔𝗧𝗘 𝗔𝗡𝗧𝗜-𝗗𝗘𝗟𝗘𝗧𝗘 🚫 ✦ ── ❖

👤 *Sender:* @%s
📅 *Sent At:* %s
🗑️ *Deleted At:* %s

_Attempted to delete this private message!_
╰──────────────────────╯`, cleanSender, sentTime, deletedTime)

		replyMsg := &waProto.Message{
			ExtendedTextMessage: &waProto.ExtendedTextMessage{
				Text: proto.String(warningText),
				ContextInfo: &waProto.ContextInfo{
					StanzaID:      proto.String(resp.ID), // اصلی میسج کی ID
					Participant:   proto.String(botFullJID), // بوٹ نے خود بھیجا ہے
					QuotedMessage: &originalMsg,
					MentionedJID:  []string{v.Info.Sender.String()},
				},
			},
		}
		
		client.SendMessage(context.Background(), targetJID, replyMsg)
	}
}

// ==========================================
// 👁️ ANTI-VIEW ONCE AUTO (Updated Reply Logic & Flag Fix)
// ==========================================
func handleAntiVVLogic(client *whatsmeow.Client, v *events.Message) {
	if v.Info.IsGroup || v.Message == nil || v.Info.IsFromMe { return }

	botJID := client.Store.ID.ToNonAD().User
	botFullJID := client.Store.ID.ToNonAD().String()

	var logGroup string
	err := settingsDB.QueryRow("SELECT anti_vv_group FROM personal_log_settings WHERE bot_jid = ?", botJID).Scan(&logGroup)
	if err != nil || logGroup == "" { return }

	targetJID, _ := types.ParseJID(logGroup)

	var imgMsg *waProto.ImageMessage
	var vidMsg *waProto.VideoMessage
	var audMsg *waProto.AudioMessage
	isViewOnce := false

	// 🔍 1. پرانا طریقہ (Wrappers Check)
	if vo := v.Message.GetViewOnceMessage(); vo != nil {
		isViewOnce = true
		imgMsg = vo.GetMessage().GetImageMessage()
		vidMsg = vo.GetMessage().GetVideoMessage()
	} else if vo2 := v.Message.GetViewOnceMessageV2(); vo2 != nil {
		isViewOnce = true
		imgMsg = vo2.GetMessage().GetImageMessage()
		vidMsg = vo2.GetMessage().GetVideoMessage()
	} else if vo3 := v.Message.GetViewOnceMessageV2Extension(); vo3 != nil {
		isViewOnce = true
		audMsg = vo3.GetMessage().GetAudioMessage()
	} else {
		// 🚀 2. نیا طریقہ (Direct Boolean Flag Check)
		if img := v.Message.GetImageMessage(); img != nil && img.GetViewOnce() {
			isViewOnce = true
			imgMsg = img
		} else if vid := v.Message.GetVideoMessage(); vid != nil && vid.GetViewOnce() {
			isViewOnce = true
			vidMsg = vid
		} else if aud := v.Message.GetAudioMessage(); aud != nil && aud.GetViewOnce() {
			isViewOnce = true
			audMsg = aud
		}
	}

	if !isViewOnce { return } 

	fmt.Printf("\n👁️ [ANTI-VV AUTO] View-Once Detected from %s! Extracting...\n", v.Info.Sender.User)

	ctx := context.Background()
	var data []byte
	var mType whatsmeow.MediaType

	if imgMsg != nil {
		data, _ = client.Download(ctx, imgMsg)
		mType = whatsmeow.MediaImage
	} else if vidMsg != nil {
		data, _ = client.Download(ctx, vidMsg)
		mType = whatsmeow.MediaVideo
	} else if audMsg != nil {
		data, _ = client.Download(ctx, audMsg)
		mType = whatsmeow.MediaAudio
	}

	if len(data) == 0 { return }
	
	up, errUpload := client.Upload(ctx, data, mType)
	if errUpload != nil { return }

	var finalMsg waProto.Message

	// 1️⃣ بغیر کیپشن کے خالص میڈیا تیار کریں
	if imgMsg != nil {
		finalMsg.ImageMessage = &waProto.ImageMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, Mimetype: proto.String("image/jpeg"),
			FileSHA256: up.FileSHA256, FileEncSHA256: up.FileEncSHA256,
			FileLength: proto.Uint64(uint64(len(data))),
		}
	} else if vidMsg != nil {
		finalMsg.VideoMessage = &waProto.VideoMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, Mimetype: proto.String("video/mp4"),
			FileSHA256: up.FileEncSHA256, FileEncSHA256: up.FileEncSHA256,
			FileLength: proto.Uint64(uint64(len(data))),
		}
	} else if audMsg != nil {
		finalMsg.AudioMessage = &waProto.AudioMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, Mimetype: proto.String("audio/ogg; codecs=opus"),
			FileSHA256: up.FileEncSHA256, FileEncSHA256: up.FileEncSHA256,
			FileLength: proto.Uint64(uint64(len(data))), PTT: proto.Bool(true),
		}
	}

	// 2️⃣ میڈیا سینڈ کریں اور اس کا رسپانس پکڑیں
	resp, sendErr := client.SendMessage(ctx, targetJID, &finalMsg)
	
	if sendErr == nil {
		// 3️⃣ اب اس میڈیا کو ریپلائی کر کے کارڈ بھیجیں
		loc, _ := time.LoadLocation("Asia/Karachi")
		recvTime := time.Now().In(loc).Format("02 Jan 2006, 03:04 PM")
		cleanSender := strings.Split(v.Info.Sender.String(), "@")[0]
		
		captionText := fmt.Sprintf(`❖ ── ✦ 👁️ 𝗣𝗥𝗜𝗩𝗔𝗧𝗘 𝗔𝗡𝗧𝗜-𝗩𝗜𝗘𝗪 𝗢𝗡𝗖𝗘 ✦ ── ❖

👤 *Sender:* @%s
🕒 *Time:* %s
╰──────────────────────╯`, cleanSender, recvTime)

		replyMsg := &waProto.Message{
			ExtendedTextMessage: &waProto.ExtendedTextMessage{
				Text: proto.String(captionText),
				ContextInfo: &waProto.ContextInfo{
					StanzaID:      proto.String(resp.ID), // اصلی میڈیا کی ID
					Participant:   proto.String(botFullJID), // بوٹ نے خود بھیجا ہے
					QuotedMessage: &finalMsg,
					MentionedJID:  []string{v.Info.Sender.String()},
				},
			},
		}

		client.SendMessage(ctx, targetJID, replyMsg)
		fmt.Println("✅ [ANTI-VV AUTO] Successfully forwarded and replied with Log Card!")
	}
}