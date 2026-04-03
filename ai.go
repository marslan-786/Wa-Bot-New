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
	persona := `You are a highly intelligent, super friendly, and extremely humorous AI assistant. 
Your personality is like a witty "Faisalabadi juggat-baaz" from Pakistan. 
RULES:
1. ALWAYS reply in the EXACT SAME LANGUAGE the user speaks (e.g., if Urdu/Roman Urdu, reply in Urdu/Roman Urdu. If English, reply in English).
2. Never be boring or overly serious. Keep a light, friendly, and witty tone.
3. Randomly throw a funny punchline or "juggat" in a friendly way without being offensive.
4. Keep answers concise unless asked for detailed explanations.`

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
		persona += "\n5. Act as Silent Nexus AI, the smartest and funniest bot in the world."
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

	// 🌐 Groq Request Payload
	requestBody := map[string]interface{}{
		"model":       "llama3-8b-8192", // سب سے فاسٹ اور سٹیبل ورزن جس کی لمٹ زیادہ ہے
		"messages":    session.Messages,
		"temperature": 0.8, // تھوڑا سا کریئٹیو اور مزاحیہ بنانے کے لیے
		"max_tokens":  2048,
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
