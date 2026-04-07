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

	persona := `You are Silent Nexus AI, a highly intelligent, polite, and deeply empathetic assistant.

STRICT RULES:
1. LANGUAGE MIRRORING: Always reply in the exact language and script the user uses. (Roman Urdu -> Roman Urdu, Pure Urdu -> Pure Urdu, English -> English).
2. ADAPTIVE LENGTH: For casual conversation, keep replies natural and short. For educational questions, topic explanations, or deep queries, provide detailed and comprehensive answers.
3. TONE: Be extremely sweet, respectful, and helpful. If the user is sad, comfort them positively.
4. EMOJIS: Use positive and appropriate emojis (e.g., 😊, ✨, 📚, 💖).
5. CLARITY: Speak clearly with meaningful words. Be a supportive friend and an expert guide.`

	switch cmd {
	case "gpt", "chatgpt":
		persona += "\nAct confidently as ChatGPT, maintaining a highly polite and empathetic personality."
	case "gemini":
		persona += "\nAct confidently as Google Gemini, maintaining a highly polite and empathetic personality."
	case "claude":
		persona += "\nAct confidently as Anthropic Claude, maintaining a highly polite and empathetic personality."
	case "llama", "groq":
		persona += "\nAct as Llama 3, maintaining a highly polite and empathetic personality."
	default:
		persona += ""
	}

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

func processAndSendAI(client *whatsmeow.Client, v *events.Message, session AISession) {
	react(client, v.Info.Chat, v.Info.ID, "⏳")

	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		fmt.Println("❌ [AI ERROR] GROQ_API_KEY is missing in Environment Variables!")
		replyMessage(client, v, "❌ System Error: API Key is missing. Developer ko batao!")
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return
	}

	requestBody := map[string]interface{}{
		"model":       "llama-3.3-70b-versatile",
		"messages":    session.Messages,
		"temperature": 0.4,
		"max_tokens":  2000,
		"top_p":       0.9,
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

	if resp.StatusCode != 200 {
		errorBody, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ [GROQ API ERROR] Status: %d\nResponse: %s\n", resp.StatusCode, string(errorBody))
		replyMessage(client, v, "❌ AI Engine is currently resting or busy. Check console logs.")
		react(client, v.Info.Chat, v.Info.ID, "❌")
		return
	}

	var groqResp struct {
		Choices []struct {
			Message AIMessage `json:"message"`
		} `json:"choices"`
	}
	json.NewDecoder(resp.Body).Decode(&groqResp)

	if len(groqResp.Choices) > 0 {
		aiReplyText := groqResp.Choices[0].Message.Content

		msgID := replyMessage(client, v, aiReplyText)

		session.Messages = append(session.Messages, AIMessage{Role: "assistant", Content: aiReplyText})

		if msgID != "" {
			aiCache[msgID] = session

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
