package main

import (
	"context"
	"fmt"
	"strings"
//	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// ==========================================
// 🛡️ COMMANDS: .antidelete & .antivv (Set Log Group)
// ==========================================

func handleAntiDeleteToggle(client *whatsmeow.Client, v *events.Message, args string) {
	if !v.Info.IsGroup {
		replyMessage(client, v, "❌ *Error:* Please use this command inside your 'Log Group' so the bot knows where to forward deleted messages.")
		return
	}
	args = strings.ToLower(strings.TrimSpace(args))
	
	state := false
	if args == "on" { state = true } else if args != "off" {
		replyMessage(client, v, "❌ Use: `.antidelete on` or `.antidelete off`")
		return
	}
	
	botJID := client.Store.ID.ToNonAD().User
	chatJID := v.Info.Chat.ToNonAD().String()
	
	// 🌟 سمارٹ لاجک: اگر ON کر رہے ہیں، تو پہلے باقی سب گروپس سے OFF کر دیں
	// تاکہ صرف اسی ایک گروپ میں میسجز آئیں
	if state {
		settingsDB.Exec("UPDATE group_settings SET anti_delete = 0 WHERE bot_jid = ?", botJID)
	}
	
	settingsDB.Exec("INSERT OR REPLACE INTO group_settings (bot_jid, chat_jid, anti_delete) VALUES (?, ?, ?)", botJID, chatJID, state)
	
	react(client, v.Info.Chat, v.Info.ID, "✅")
	replyMessage(client, v, fmt.Sprintf("✅ *Log Group Set!* Deleted private messages will now be forwarded here."))
}

func handleAntiVVToggle(client *whatsmeow.Client, v *events.Message, args string) {
	if !v.Info.IsGroup {
		replyMessage(client, v, "❌ *Error:* Please use this command inside your 'Log Group'.")
		return
	}
	args = strings.ToLower(strings.TrimSpace(args))
	
	state := false
	if args == "on" { state = true } else if args != "off" {
		replyMessage(client, v, "❌ Use: `.antivv on` or `.antivv off`")
		return
	}
	
	botJID := client.Store.ID.ToNonAD().User
	chatJID := v.Info.Chat.ToNonAD().String()
	
	if state {
		settingsDB.Exec("UPDATE group_settings SET anti_vv = 0 WHERE bot_jid = ?", botJID)
	}
	
	settingsDB.Exec("UPDATE group_settings SET anti_vv = ? WHERE bot_jid = ? AND chat_jid = ?", state, botJID, chatJID)
	
	react(client, v.Info.Chat, v.Info.ID, "✅")
	replyMessage(client, v, fmt.Sprintf("✅ *Log Group Set!* View-Once private media will now be forwarded here."))
}

// ==========================================
// 📥 CACHE SAVER: (ONLY Saves Private Messages)
// ==========================================
func handleAntiDeleteSave(client *whatsmeow.Client, v *events.Message) {
	// 🚫 اگر میسج گروپ کا ہے، یا بوٹ کا اپنا ہے، تو فوراً واپس! (سٹوریج بچائیں)
	if v.Info.IsGroup || v.Message == nil || v.Info.IsFromMe { return }

	botJID := client.Store.ID.ToNonAD().User

	// چیک کریں کہ کیا کسی بھی گروپ میں اینٹی ڈیلیٹ ON ہے؟
	var logGroup string
	err := settingsDB.QueryRow("SELECT chat_jid FROM group_settings WHERE bot_jid = ? AND anti_delete = 1 LIMIT 1", botJID).Scan(&logGroup)
	if err != nil || logGroup == "" { return } // اگر کوئی لاگ گروپ نہیں ہے تو سیو نہ کریں

	msgBytes, err := proto.Marshal(v.Message)
	if err == nil {
		settingsDB.Exec("INSERT OR REPLACE INTO message_cache (msg_id, sender_jid, msg_content, timestamp) VALUES (?, ?, ?, ?)", 
			v.Info.ID, v.Info.Sender.String(), msgBytes, v.Info.Timestamp.Unix())
	}
}

// ==========================================
// 🚀 REVOKE CATCHER: (Forwards to Log Group)
// ==========================================
func handleAntiDeleteRevoke(client *whatsmeow.Client, v *events.Message) {
	// 🚫 صرف پرائیویٹ چیٹس کا ڈیلیٹ کیچ کریں گے
	if v.Info.IsGroup || v.Info.IsFromMe { return }

	botJID := client.Store.ID.ToNonAD().User
	
	// 🎯 ڈیٹا بیس سے وہ گروپ نکالیں جہاں اینٹی ڈیلیٹ ON ہے
	var logGroup string
	err := settingsDB.QueryRow("SELECT chat_jid FROM group_settings WHERE bot_jid = ? AND anti_delete = 1 LIMIT 1", botJID).Scan(&logGroup)
	if err != nil || logGroup == "" { return }

	targetJID, _ := types.ParseJID(logGroup)
	deletedMsgID := v.Message.GetProtocolMessage().GetKey().GetID()
	senderJID := v.Info.Sender.ToNonAD().User

	var rawMsg []byte
	err = settingsDB.QueryRow("SELECT msg_content FROM message_cache WHERE msg_id = ?", deletedMsgID).Scan(&rawMsg)
	if err != nil { return } // کیشے میں نہیں ملا

	var originalMsg waProto.Message
	proto.Unmarshal(rawMsg, &originalMsg)

	cleanSender := strings.Split(senderJID, "@")[0]
	warningText := fmt.Sprintf(`❖ ── ✦ 🚫 𝗣𝗥𝗜𝗩𝗔𝗧𝗘 𝗔𝗡𝗧𝗜-𝗗𝗘𝗟𝗘𝗧𝗘 🚫 ✦ ── ❖

👤 *Sender:* @%s
🗑️ _Attempted to delete this private message!_
╰──────────────────────╯`, cleanSender)

	// 1. لاگ گروپ میں الرٹ بھیجیں
	client.SendMessage(context.Background(), targetJID, &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String(warningText),
			ContextInfo: &waProto.ContextInfo{ MentionedJID: []string{v.Info.Sender.String()} },
		},
	})
	
	// 2. اصلی میسج لاگ گروپ میں فارورڈ کریں
	client.SendMessage(context.Background(), targetJID, &originalMsg)
}

// ==========================================
// 👁️ ANTI-VV: VIEW-ONCE EXTRACTOR (To Log Group)
// ==========================================
func handleAntiVVLogic(client *whatsmeow.Client, v *events.Message) {
	// 🚫 صرف پرائیویٹ چیٹس کے View Once کیچ کریں گے
	if v.Info.IsGroup || v.Message == nil || v.Info.IsFromMe { return }

	botJID := client.Store.ID.ToNonAD().User
	
	// 🎯 وہ گروپ نکالیں جہاں Anti-VV ON ہے
	var logGroup string
	err := settingsDB.QueryRow("SELECT chat_jid FROM group_settings WHERE bot_jid = ? AND anti_vv = 1 LIMIT 1", botJID).Scan(&logGroup)
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

	cleanSender := strings.Split(v.Info.Sender.String(), "@")[0]
	caption := fmt.Sprintf(`❖ ── ✦ 👁️ 𝗣𝗥𝗜𝗩𝗔𝗧𝗘 𝗔𝗡𝗧𝗜-𝗩𝗜𝗘𝗪 𝗢𝗡𝗖𝗘 ✦ ── ❖

👤 *Sender:* @%s
╰──────────────────────╯`, cleanSender)

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

	// 🌟 لاگ گروپ میں بھیج دیں
	client.SendMessage(ctx, targetJID, &finalMsg)
}