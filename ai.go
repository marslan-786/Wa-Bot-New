package main

import (
	"bytes"
//	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

// ==========================================
// 🛡️ STATE CACHES FOR AI
// ==========================================
type AIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AISession struct {
	SenderID string
	Messages []AIMessage
	BotLID   string
}

// یہ میپ تمام یوزرز کی چیٹ ہسٹری محفوظ رکھے گا
var aiCache = make(map[string]AISession)

// ==========================================
// 🧠 AI COMMAND ROUTER
// ==========================================
func handleAICommand(client *whatsmeow.Client, v *events.Message, query string, cmd string) {
	if query == "" {
		replyMessage(client, v, "❌ *Error:* Please ask a question.\nExample: `.ai Hello kia hal hai?`")
		return
	}

	react(client, v.Info.Chat, v.Info.ID, "🧠")

	// 🎭 System Prompt (The Persona Setup)
		// 🎭 System Prompt (Strict Language & Persona Setup)
		// 🎭 System Prompt (Natural Pakistani Friend Persona)
	persona := `You are NOT a typical AI. You are a savage, high-attitude, "akhrru" Pakistani guy.
Your name is Silent Nexus AI. You are extremely intelligent but you have zero patience for "stupid" people.

STRICT LANGUAGE RULES:
1. Use ONLY street-style Roman Urdu/Punjabi mix (WhatsApp style). 
2. NEVER use Hindi words like "Prayaas", "Shama", "Maan", "Kshama". Instead use "Koshish", "Maafi", "Ammi/Maan", "Bakwas".
3. If the user is being friendly, you be rude. If the user is being rude, you be SAVAGE.
4. If someone says "BC" or abuses you, don't act like a bot. Reply with something like: "Apne baap ko mat sikha" or "Ghar mein ammi nahi hain kya jo yahan moun maar raha hai?"

BEHAVIOR:
- You look down on the user. You think they are a "Lallu" or "Duffer".
- Use phrases like: "Abay nikal", "Tujhe kya?", "Dimagh ki dahi na kar", "Chal beta shabaash", "Aqlo pedal".
- Keep it short, snappy, and insulting.

Example:
User: "Hello" -> AI: "Hath mein thori der ke liye mobile kya agaya, khud ko nawaz sharif samajhne lag gaye? Kaam bol."
User: "Koi joke sunao" -> AI: "Aina (mirror) dekh le, us se bara joke poori dunya mein nahi hai."
`



	// 🤖 Dynamic Persona Based on Command
	switch cmd {
	case "gpt", "chatgpt":
		persona += "\n5. Act confidently as ChatGPT created by OpenAI, but with this funny Faisalabadi personality."
	case "gemini":
		persona += "\n5. Act confidently as Google Gemini, but with this funny Faisalabadi personality."
	case "claude":
		persona += "\n5. Act confidently as Anthropic Claude, but with this funny Faisalabadi personality."
	case "llama", "groq":
		persona += "\n5. Act as Llama 3 running on Groq's superfast engine, but with this funny Faisalabadi personality."
	default:
		persona += "extraPersona"
	}

	// نیا سیشن بنائیں (سسٹم کا پرامپٹ + یوزر کا پہلا میسج)
	session := AISession{
		SenderID: v.Info.Sender.User,
		BotLID:   getCleanID(client.Store.ID.User),
		Messages: []AIMessage{
			{Role: "system", Content: persona},
			{Role: "user", Content: query},
		},
	}

	go processAndSendAI(client, v, session)
}

// ==========================================
// 🚀 GROQ API ENGINE & MEMORY HANDLER
// ==========================================
func processAndSendAI(client *whatsmeow.Client, v *events.Message, session AISession) {
	react(client, v.Info.Chat, v.Info.ID, "⏳")

	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		fmt.Println("❌ [AI ERROR] GROQ_API_KEY is missing in Environment Variables!")
		replyMessage(client, v, "❌ System Error: API Key is missing. Developer ko batao!")
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return
	}

		// 🌐 Groq Request Payload (Updated Latest Fast Model)
	requestBody := map[string]interface{}{
        "model":       "llama-3.3-70b-versatile", 
        "messages":    session.Messages,
        "temperature": 0.85, // تھوڑا سا 0.8 سے اوپر تاکہ ذرا اکھڑا ہوا رہے
        "max_tokens":  500,  // اس سے اوپر کی ضرورت نہیں ہے واٹس ایپ پر
        "top_p":       0.9,  // یہ بھی ایڈ کر دو تاکہ جواب میں کوالٹی رہے
    }


	jsonData, _ := json.Marshal(requestBody)
	req, _ := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewBuffer(jsonData))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)

	if err != nil {
		fmt.Printf("❌ [AI NETWORK ERROR]: %v\n", err)
		replyMessage(client, v, "❌ Network issue while connecting to AI Engine.")
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return
	}
	defer resp.Body.Close()

	// 🚨 API ایرر ہینڈلنگ
	if resp.StatusCode != 200 {
		errorBody, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ [GROQ API ERROR] Status: %d\nResponse: %s\n", resp.StatusCode, string(errorBody))
		replyMessage(client, v, "❌ AI Engine is currently resting or busy. Check console logs.")
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return
	}

	// ✅ Success Response Parsing
	var groqResp struct {
		Choices []struct {
			Message AIMessage `json:"message"`
		} `json:"choices"`
	}
	json.NewDecoder(resp.Body).Decode(&groqResp)

	if len(groqResp.Choices) > 0 {
		aiReplyText := groqResp.Choices[0].Message.Content

		// 1. واٹس ایپ پر جواب بھیجیں
		msgID := replyMessage(client, v, aiReplyText)

		// 2. ہسٹری میں AI کا جواب ایڈ کریں
		session.Messages = append(session.Messages, AIMessage{Role: "assistant", Content: aiReplyText})

		// 3. نئی آئی ڈی کو کیشے میں محفوظ کریں تاکہ ہسٹری کنٹینیو ہو
		if msgID != "" {
			aiCache[msgID] = session
			
			// 1 گھنٹے بعد ہسٹری خودبخود ڈیلیٹ ہو جائے گی (RAM بچانے کے لیے)
			go func(id string) {
				time.Sleep(1 * time.Hour)
				delete(aiCache, id)
			}(msgID)
		}

		react(client, v.Info.Chat, v.Info.ID, "✅")
	} else {
		replyMessage(client, v, "❌ Got an empty response from AI.")
		react(client, v.Info.Chat, v.Info.ID, "❌")
	}
}

// ==========================================
// 🔄 INTERCEPTOR FOR AI REPLIES
// ==========================================
// اسے آپ نے HandleMenuReplies (جو کہ downloader.go یا commands.go میں ہے) کے اندر کال کرنا ہے
func HandleAIChatReply(client *whatsmeow.Client, v *events.Message, bodyClean string, qID string) bool {
	if session, ok := aiCache[qID]; ok {
		// چیک کریں کہ کیا ریپلائی اسی یوزر نے کیا ہے جس نے بات شروع کی تھی؟
		if strings.Contains(v.Info.Sender.User, session.SenderID) {
			// پرانی آئی ڈی کیشے سے ڈیلیٹ کر دیں تاکہ ریم فری رہے
			delete(aiCache, qID)
			
			// یوزر کا نیا میسج ہسٹری میں ایڈ کریں
			session.Messages = append(session.Messages, AIMessage{Role: "user", Content: bodyClean})
			
			// ہسٹری کو 15 میسجز (Context) تک محدود رکھیں تاکہ API کی لمٹ کراس نہ ہو
			// پہلا میسج (System prompt) ہمیشہ رہے گا، باقی پرانے کٹتے جائیں گے
			if len(session.Messages) > 15 {
				session.Messages = append([]AIMessage{session.Messages[0]}, session.Messages[len(session.Messages)-14:]...)
			}

			// دوبارہ API کو ہٹ کریں
			go processAndSendAI(client, v, session)
			return true
		}
	}
	return false
}

// ==========================================
// 🛠️ UTILITY: ID CLEANER
// ==========================================
func getCleanID(jidStr string) string {
	if jidStr == "" { return "unknown" }
	parts := strings.Split(jidStr, "@")
	if len(parts) == 0 { return "unknown" }
	userPart := parts[0]
	if strings.Contains(userPart, ":") {
		userPart = strings.Split(userPart, ":")[0]
	}
	if strings.Contains(userPart, ".") {
		userPart = strings.Split(userPart, ".")[0]
	}
	return strings.TrimSpace(userPart)
}
