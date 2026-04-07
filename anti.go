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
// 🛠️ DATABASE INIT (New Personal Table)
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
	
	// زبردستی رو (Row) بنائیں تاکہ UPDATE فیل نہ ہو
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
		
		// ڈیٹا بیس اپڈیٹ کریں اور چیک کریں کہ کیا واقعی اپڈیٹ ہوا ہے؟
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
// 📥 CACHE SAVER & FORWARDERS
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

func handleAntiDeleteRevoke(client *whatsmeow.Client, v *events.Message) {
	if v.Info.IsGroup || v.Info.IsFromMe { return }

	botJID := client.Store.ID.ToNonAD().User
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

	client.SendMessage(context.Background(), targetJID, &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String(warningText),
			ContextInfo: &waProto.ContextInfo{ MentionedJID: []string{v.Info.Sender.String()} },
		},
	})
	
	client.SendMessage(context.Background(), targetJID, &originalMsg)
}

func handleAntiVVLogic(client *whatsmeow.Client, v *events.Message) {
	if v.Info.IsGroup || v.Message == nil || v.Info.IsFromMe { return }

	botJID := client.Store.ID.ToNonAD().User
	var logGroup string
	err := settingsDB.QueryRow("SELECT anti_vv_group FROM personal_log_settings WHERE bot_jid = ?", botJID).Scan(&logGroup)
	if err != nil || logGroup == "" { return }

	targetJID, _ := types.ParseJID(logGroup)

	vo1 := v.Message.GetViewOnceMessage()
	vo2 := v.Message.GetViewOnceMessageV2()
	vo3 := v.Message.GetViewOnceMessageV2Extension()

	if vo1 == nil && vo2 == nil && vo3 == nil { return }

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
	up, err := client.Upload(ctx, data, mType)
	if err != nil { return }

	loc, _ := time.LoadLocation("Asia/Karachi")
	recvTime := time.Now().In(loc).Format("02 Jan 2006, 03:04 PM")

	cleanSender := strings.Split(v.Info.Sender.String(), "@")[0]
	caption := fmt.Sprintf(`❖ ── ✦ 👁️ 𝗣𝗥𝗜𝗩𝗔𝗧𝗘 𝗔𝗡𝗧𝗜-𝗩𝗜𝗘𝗪 𝗢𝗡𝗖𝗘 ✦ ── ❖

👤 *Sender:* @%s
🕒 *Time:* %s
╰──────────────────────╯`, cleanSender, recvTime)

	var finalMsg waProto.Message

	if imgMsg != nil {
		finalMsg.ImageMessage = &waProto.ImageMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, Mimetype: proto.String("image/jpeg"),
			FileSHA256: up.FileSHA256, FileEncSHA256: up.FileEncSHA256,
			FileLength: proto.Uint64(uint64(len(data))), Caption: proto.String(caption),
			ContextInfo: &waProto.ContextInfo{ MentionedJID: []string{v.Info.Sender.String()} },
		}
	} else if vidMsg != nil {
		finalMsg.VideoMessage = &waProto.VideoMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, Mimetype: proto.String("video/mp4"),
			FileSHA256: up.FileEncSHA256, FileEncSHA256: up.FileEncSHA256,
			FileLength: proto.Uint64(uint64(len(data))), Caption: proto.String(caption),
			ContextInfo: &waProto.ContextInfo{ MentionedJID: []string{v.Info.Sender.String()} },
		}
	} else if audMsg != nil {
		client.SendMessage(ctx, targetJID, &waProto.Message{
			ExtendedTextMessage: &waProto.ExtendedTextMessage{ 
				Text: proto.String(caption),
				ContextInfo: &waProto.ContextInfo{ MentionedJID: []string{v.Info.Sender.String()} },
			},
		})
		finalMsg.AudioMessage = &waProto.AudioMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
			MediaKey: up.MediaKey, Mimetype: proto.String("audio/ogg; codecs=opus"),
			FileSHA256: up.FileEncSHA256, FileEncSHA256: up.FileEncSHA256,
			FileLength: proto.Uint64(uint64(len(data))), PTT: proto.Bool(true),
		}
	}

	client.SendMessage(ctx, targetJID, &finalMsg)
}