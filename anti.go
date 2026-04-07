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
// 🛠️ DATABASE INIT (With Stealth Trigger)
// ==========================================
func initPersonalLogDB() {
	query := `CREATE TABLE IF NOT EXISTS personal_log_settings (
		bot_jid TEXT PRIMARY KEY,
		anti_delete_group TEXT DEFAULT '',
		anti_vv_group TEXT DEFAULT '',
		anti_vv_trigger TEXT DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS message_cache (
		msg_id TEXT PRIMARY KEY,
		sender_jid TEXT,
		msg_content BLOB,
		timestamp INTEGER
	);`
	settingsDB.Exec(query)

	// اگر پرانا ٹیبل ہے تو اس میں نیا کالم ایڈ کر دیں (ایرر کو اگنور کرے گا اگر کالم پہلے سے ہے)
	settingsDB.Exec("ALTER TABLE personal_log_settings ADD COLUMN anti_vv_trigger TEXT DEFAULT ''")
}

// ==========================================
// 🛡️ ANTI-DELETE TOGGLE
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
	
	settingsDB.Exec("INSERT OR IGNORE INTO personal_log_settings (bot_jid) VALUES (?)", botJID)

	var currentGroup string
	err := settingsDB.QueryRow("SELECT anti_delete_group FROM personal_log_settings WHERE bot_jid = ?", botJID).Scan(&currentGroup)
	if err != nil { currentGroup = "" }

	if args == "on" {
		if currentGroup == chatJID {
			replyMessage(client, v, "⚠️ *Already ON:* This is already your personal Log Group for Anti-Delete.")
			return
		}
		
		settingsDB.Exec("UPDATE personal_log_settings SET anti_delete_group = ? WHERE bot_jid = ?", chatJID, botJID)
		react(client, v.Info.Chat, v.Info.ID, "✅")
		replyMessage(client, v, "✅ *Personal Log Group Activated!* Private deleted messages will now be forwarded here.")
		
	} else if args == "off" {
		if currentGroup != chatJID {
			replyMessage(client, v, "⚠️ *Error:* You can only turn this OFF from the exact Log Group where you turned it ON.")
			return
		}
		
		settingsDB.Exec("UPDATE personal_log_settings SET anti_delete_group = '' WHERE bot_jid = ?", botJID)
		react(client, v.Info.Chat, v.Info.ID, "✅")
		replyMessage(client, v, "❌ *Personal Log Group Deactivated!* Anti-Delete forwarding is now OFF.")
	}
}

// ==========================================
// 🛡️ ANTI-VV TOGGLE & TRIGGER SETTER (Updated)
// ==========================================
func handleAntiVVToggle(client *whatsmeow.Client, v *events.Message, args string) {
	initPersonalLogDB()
	
	args = strings.TrimSpace(args)
	parts := strings.Fields(args)
	
	if len(parts) == 0 {
		replyMessage(client, v, "❌ Use: `.antivv on`, `.antivv off`, or `.antivv set <word>`")
		return
	}
	
	botJID := client.Store.ID.ToNonAD().User
	chatJID := v.Info.Chat.ToNonAD().String()
	cmd := strings.ToLower(parts[0])
	
	settingsDB.Exec("INSERT OR IGNORE INTO personal_log_settings (bot_jid) VALUES (?)", botJID)

	var currentGroup, currentTrigger string
	settingsDB.QueryRow("SELECT anti_vv_group, anti_vv_trigger FROM personal_log_settings WHERE bot_jid = ?", botJID).Scan(&currentGroup, &currentTrigger)

	if cmd == "on" {
		if !v.Info.IsGroup {
			replyMessage(client, v, "❌ *Error:* Please use this command inside your intended 'Log Group'.")
			return
		}
		if currentGroup == chatJID {
			replyMessage(client, v, "⚠️ *Already ON:* This is already your personal Stealth Log Group.")
			return
		}
		settingsDB.Exec("UPDATE personal_log_settings SET anti_vv_group = ? WHERE bot_jid = ?", chatJID, botJID)
		react(client, v.Info.Chat, v.Info.ID, "✅")
		replyMessage(client, v, "✅ *Stealth Log Group Activated!* Media extracted via trigger word will be forwarded here.")
		
	} else if cmd == "off" {
		settingsDB.Exec("UPDATE personal_log_settings SET anti_vv_group = '' WHERE bot_jid = ?", botJID)
		react(client, v.Info.Chat, v.Info.ID, "✅")
		replyMessage(client, v, "❌ *Stealth Log Group Deactivated!*")
		
	} else if cmd == "set" {
		if len(parts) < 2 {
			replyMessage(client, v, "❌ *Error:* Please provide a trigger word. Example: `.antivv set nice`")
			return
		}
		triggerWord := strings.ToLower(parts[1])
		settingsDB.Exec("UPDATE personal_log_settings SET anti_vv_trigger = ? WHERE bot_jid = ?", triggerWord, botJID)
		react(client, v.Info.Chat, v.Info.ID, "✅")
		replyMessage(client, v, fmt.Sprintf("🕵️ *Stealth Trigger Set!*\nNow, replying to any media with exactly *\"%s\"* will secretly forward it to your Log Group.", triggerWord))
		
	} else {
		replyMessage(client, v, "❌ Invalid command. Use `on`, `off`, or `set <word>`")
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
// 🚫 ANTI-DELETE REVOKE HANDLER
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
	if err != nil { return } 

	var originalMsg waProto.Message
	proto.Unmarshal(rawMsg, &originalMsg)

	resp, sendErr := client.SendMessage(context.Background(), targetJID, &originalMsg)
	
	if sendErr == nil {
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
					StanzaID:      proto.String(resp.ID), 
					Participant:   proto.String(botFullJID), 
					QuotedMessage: &originalMsg,
					MentionedJID:  []string{v.Info.Sender.String()},
				},
			},
		}
		
		client.SendMessage(context.Background(), targetJID, replyMsg)
	}
}

// ==========================================
// 🕵️ STEALTH MEDIA EXTRACTOR (The Trigger Hack)
// ==========================================
func handleStealthVVTrigger(client *whatsmeow.Client, v *events.Message) {
	botJID := client.Store.ID.ToNonAD().User

	var logGroup, triggerWord string
	err := settingsDB.QueryRow("SELECT anti_vv_group, anti_vv_trigger FROM personal_log_settings WHERE bot_jid = ?", botJID).Scan(&logGroup, &triggerWord)
	if err != nil || logGroup == "" || triggerWord == "" {
		return
	}

	extMsg := v.Message.GetExtendedTextMessage()
	if extMsg == nil { return } 

	msgText := strings.ToLower(strings.TrimSpace(extMsg.GetText()))
	if msgText != triggerWord {
		return 
	}

	if extMsg.ContextInfo == nil || extMsg.ContextInfo.QuotedMessage == nil {
		return
	}

	quoted := extMsg.ContextInfo.QuotedMessage
	var data []byte
	var extractErr error
	var finalMsg waProto.Message
	var mType whatsmeow.MediaType

	extractMedia := func(m *waProto.Message) bool {
		if img := m.GetImageMessage(); img != nil {
			data, extractErr = client.Download(context.Background(), img)
			mType = whatsmeow.MediaImage
			if extractErr == nil {
				up, _ := client.Upload(context.Background(), data, mType)
				finalMsg.ImageMessage = &waProto.ImageMessage{
					URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
					MediaKey: up.MediaKey, Mimetype: proto.String("image/jpeg"),
					FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
					FileLength: proto.Uint64(uint64(len(data))),
				}
				return true
			}
		} else if vid := m.GetVideoMessage(); vid != nil {
			data, extractErr = client.Download(context.Background(), vid)
			mType = whatsmeow.MediaVideo
			if extractErr == nil {
				up, _ := client.Upload(context.Background(), data, mType)
				finalMsg.VideoMessage = &waProto.VideoMessage{
					URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
					MediaKey: up.MediaKey, Mimetype: proto.String("video/mp4"),
					FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
					FileLength: proto.Uint64(uint64(len(data))),
				}
				return true
			}
		} else if aud := m.GetAudioMessage(); aud != nil {
			data, extractErr = client.Download(context.Background(), aud)
			mType = whatsmeow.MediaAudio
			if extractErr == nil {
				up, _ := client.Upload(context.Background(), data, mType)
				finalMsg.AudioMessage = &waProto.AudioMessage{
					URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath),
					MediaKey: up.MediaKey, Mimetype: proto.String("audio/ogg; codecs=opus"),
					FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
					FileLength: proto.Uint64(uint64(len(data))), PTT: proto.Bool(true),
				}
				return true
			}
		}
		return false
	}

	if vo := quoted.GetViewOnceMessage(); vo != nil {
		extractMedia(vo.GetMessage())
	} else if vo2 := quoted.GetViewOnceMessageV2(); vo2 != nil {
		extractMedia(vo2.GetMessage())
	} else if vo3 := quoted.GetViewOnceMessageV2Extension(); vo3 != nil {
		extractMedia(vo3.GetMessage())
	} else {
		extractMedia(quoted)
	}

	if data != nil && len(data) > 0 {
		targetJID, _ := types.ParseJID(logGroup)
		botFullJID := client.Store.ID.ToNonAD().String()
		cleanSender := strings.Split(v.Info.Chat.User, "@")[0]
		
		// 1️⃣ پہلے میڈیا سینڈ کریں
		resp, sendErr := client.SendMessage(context.Background(), targetJID, &finalMsg)
		
		// 2️⃣ پھر اس کو ریپلائی کر کے کارڈ بھیجیں
		if sendErr == nil {
			caption := fmt.Sprintf(`❖ ── ✦ 🕵️ 𝗦𝗧𝗘𝗔𝗟𝗧𝗛 𝗘𝗫𝗧𝗥𝗔𝗖𝗧 ✦ ── ❖

👤 *From Chat:* @%s
🔑 *Trigger:* "%s"
╰──────────────────────╯`, cleanSender, triggerWord)

			replyMsg := &waProto.Message{
				ExtendedTextMessage: &waProto.ExtendedTextMessage{
					Text: proto.String(caption),
					ContextInfo: &waProto.ContextInfo{
						StanzaID:      proto.String(resp.ID),
						Participant:   proto.String(botFullJID),
						QuotedMessage: &finalMsg,
						MentionedJID:  []string{v.Info.Chat.String()},
					},
				},
			}
			client.SendMessage(context.Background(), targetJID, replyMsg)
		}
	}
}