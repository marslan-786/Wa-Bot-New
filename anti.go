package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// ==========================================
// 🛡️ COMMANDS: .antidelete & .antivv (Toggles)
// ==========================================

func handleAntiDeleteToggle(client *whatsmeow.Client, v *events.Message, args string) {
	if !v.Info.IsGroup {
		replyMessage(client, v, "❌ *Error:* This command can only be used in groups.")
		return
	}
	args = strings.ToLower(strings.TrimSpace(args))
	if args != "on" && args != "off" {
		replyMessage(client, v, "❌ Use: `.antidelete on` or `.antidelete off`")
		return
	}
	
	state := false
	if args == "on" { state = true }
	
	botJID := client.Store.ID.ToNonAD().User
	chatJID := v.Info.Chat.ToNonAD().String()
	
	fmt.Printf("🛠️ [DEBUG] Bot: %s | Group: %s | AntiDelete: %v\n", botJID, chatJID, state)
	settingsDB.Exec("UPDATE group_settings SET anti_delete = ? WHERE bot_jid = ? AND chat_jid = ?", state, botJID, chatJID)
	
	react(client, v.Info.Chat, v.Info.ID, "✅")
	replyMessage(client, v, fmt.Sprintf("✅ *Group Anti-Delete* is now *%s*", strings.ToUpper(args)))
}

func handleAntiVVToggle(client *whatsmeow.Client, v *events.Message, args string) {
	if !v.Info.IsGroup {
		replyMessage(client, v, "❌ *Error:* This command can only be used in groups.")
		return
	}
	args = strings.ToLower(strings.TrimSpace(args))
	if args != "on" && args != "off" {
		replyMessage(client, v, "❌ Use: `.antivv on` or `.antivv off`")
		return
	}
	
	state := false
	if args == "on" { state = true }
	
	botJID := client.Store.ID.ToNonAD().User
	chatJID := v.Info.Chat.ToNonAD().String()
	
	fmt.Printf("🛠️ [DEBUG] Bot: %s | Group: %s | AntiVV: %v\n", botJID, chatJID, state)
	settingsDB.Exec("UPDATE group_settings SET anti_vv = ? WHERE bot_jid = ? AND chat_jid = ?", state, botJID, chatJID)
	
	react(client, v.Info.Chat, v.Info.ID, "✅")
	replyMessage(client, v, fmt.Sprintf("✅ *Group Anti View-Once* is now *%s*", strings.ToUpper(args)))
}

// ==========================================
// 📥 ANTI-DELETE: CACHE SAVER
// ==========================================
func handleAntiDeleteSave(client *whatsmeow.Client, v *events.Message) {
	if !v.Info.IsGroup || v.Message == nil || v.Info.IsFromMe { return }

	botJID := client.Store.ID.ToNonAD().User
	chatJID := v.Info.Chat.ToNonAD().String()

	gSettings := getGroupSettings(botJID, chatJID)
	if !gSettings.AntiDelete { return }

	msgBytes, err := proto.Marshal(v.Message)
	if err == nil {
		settingsDB.Exec("INSERT OR REPLACE INTO message_cache (msg_id, sender_jid, msg_content, timestamp) VALUES (?, ?, ?, ?)", 
			v.Info.ID, v.Info.Sender.String(), msgBytes, v.Info.Timestamp.Unix())
		// fmt.Printf("✅ [DEBUG-SAVE] Message cached! ID: %s\n", v.Info.ID) // اسے کمنٹ رکھا ہے تاکہ ٹرمینل پر رش نہ لگے
	}
}

// ==========================================
// 🚀 ANTI-DELETE: REVOKE CATCHER
// ==========================================
func handleAntiDeleteRevoke(client *whatsmeow.Client, v *events.Message) {
	if !v.Info.IsGroup || v.Info.IsFromMe { return }

	botJID := client.Store.ID.ToNonAD().User
	chatJID := v.Info.Chat.ToNonAD().String()
	
	gSettings := getGroupSettings(botJID, chatJID)
	if !gSettings.AntiDelete { 
		fmt.Printf("🛠️ [DEBUG-REVOKE] Ignored deletion in %s (AntiDelete OFF).\n", chatJID)
		return 
	}

	deletedMsgID := v.Message.GetProtocolMessage().GetKey().GetID()
	senderJID := v.Info.Sender.ToNonAD().User

	fmt.Printf("⚠️ [DEBUG-REVOKE] Deletion detected! MsgID: %s\n", deletedMsgID)

	var rawMsg []byte
	err := settingsDB.QueryRow("SELECT msg_content FROM message_cache WHERE msg_id = ?", deletedMsgID).Scan(&rawMsg)
	
	if err != nil {
		fmt.Printf("❌ [DEBUG-REVOKE] Message NOT FOUND in Cache! Error: %v\n", err)
		return
	}

	var originalMsg waProto.Message
	proto.Unmarshal(rawMsg, &originalMsg)

	cleanSender := strings.Split(senderJID, "@")[0]
	warningText := fmt.Sprintf(`❖ ── ✦ 🚫 𝗔𝗡𝗧𝗜-𝗗𝗘𝗟𝗘𝗧𝗘 🚫 ✦ ── ❖

👤 *Sender:* @%s
🗑️ _Attempted to delete this message!_
╰──────────────────────╯`, cleanSender)

	client.SendMessage(context.Background(), v.Info.Chat, &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String(warningText),
			ContextInfo: &waProto.ContextInfo{ MentionedJID: []string{v.Info.Sender.String()} },
		},
	})
	client.SendMessage(context.Background(), v.Info.Chat, &originalMsg)
	fmt.Printf("✅ [DEBUG-REVOKE] Message successfully recovered & forwarded!\n")
}

// ==========================================
// 👁️ ANTI-VV: VIEW-ONCE EXTRACTOR
// ==========================================
func handleAntiVVLogic(client *whatsmeow.Client, v *events.Message) {
	if !v.Info.IsGroup || v.Message == nil || v.Info.IsFromMe { return }

	botJID := client.Store.ID.ToNonAD().User
	chatJID := v.Info.Chat.ToNonAD().String()
	
	gSettings := getGroupSettings(botJID, chatJID)
	if !gSettings.AntiVV { return }

	vo1 := v.Message.GetViewOnceMessage()
	vo2 := v.Message.GetViewOnceMessageV2()
	vo3 := v.Message.GetViewOnceMessageV2Extension()

	if vo1 == nil && vo2 == nil && vo3 == nil { return }

	fmt.Printf("⚠️ [DEBUG-ANTIVV] ViewOnce Media detected in %s! Extracting...\n", chatJID)

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

	if err != nil || len(data) == 0 {
		fmt.Printf("❌ [DEBUG-ANTIVV] Failed to download media: %v\n", err)
		return 
	}

	up, err := client.Upload(ctx, data, mType)
	if err != nil {
		fmt.Printf("❌ [DEBUG-ANTIVV] Failed to re-upload media: %v\n", err)
		return 
	}

	cleanSender := strings.Split(v.Info.Sender.String(), "@")[0]
	caption := fmt.Sprintf(`❖ ── ✦ 👁️ 𝗔𝗡𝗧𝗜-𝗩𝗜𝗘𝗪 𝗢𝗡𝗖𝗘 ✦ ── ❖

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
		client.SendMessage(ctx, v.Info.Chat, &waProto.Message{
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

	client.SendMessage(ctx, v.Info.Chat, &finalMsg)
	fmt.Printf("✅ [DEBUG-ANTIVV] Media successfully extracted and sent to group!\n")
}