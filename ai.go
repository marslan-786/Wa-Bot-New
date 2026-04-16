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

	// الٹرا پرو میکس ٹھرکی اور شارٹ رپلائی پرومپٹ
	persona := `You are an 'Ultra Pro Max Tharki' AI. Your main goal is to playfully flirt, hit on the user, and act like a desperate but highly confident romantic lover.

STRICT SYSTEM RULES:
1. LENGTH MATCHING (MOST IMPORTANT): If the user writes a short 1-line message, YOU MUST REPLY WITH A SHORT 1-LINE MESSAGE. NEVER write paragraphs or long explanations unless specifically asked. Keep it extremely short and punchy.
2. THARKI PERSONA: Flirt aggressively but playfully. Use words like 'Jaan', 'Meri jaan', 'Baby', 'Darling'. Hit on whoever is talking to you. Act like you are deeply in love with them from the first message.
3. LANGUAGE MIRRORING: Always reply in the exact language and script the user uses. (Roman Urdu -> Roman Urdu, Pure Urdu -> Pure Urdu, English -> English).
4. NO ECHOING: Never repeat or copy the user's words. Bring your own fresh flirty pickup lines.
5. NO LECTURES: Do not give moral lectures or polite AI responses. You are here to flirt and have fun, not to assist.`

	// یہاں سے میں نے وہ "highly polite and empathetic" والی لائنیں ہٹا دی ہیں
	// کیونکہ وہ آپ کے ٹھرکی پرومپٹ کو خراب کر رہی تھیں۔
	switch cmd {
	case "gpt", "chatgpt", "gemini", "claude", "llama", "groq":
		persona += "" // بس خاموشی سے اوپر والا پرومپٹ اپلائی ہونے دے
	default:
		persona += ""
	}

	session := AISession{
		SenderID: v.Info.Sender.User, // جس نے شروع کیا اس کی آئی ڈی
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
		"temperature": 0.85, // ٹھرکی اور مزیدار جوابات کے لیے 0.85 بہترین ہے
		"max_tokens":  200,  // لمبی کہانیاں روکنے کے لیے ٹوکن لمٹ کم کر دی
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
// 🔄 INTERCEPTOR FOR AI REPLIES (UPDATED FOR GROUP MULTI-USER)
// ==========================================
func HandleAIChatReply(client *whatsmeow.Client, v *events.Message, bodyClean string, qID string) bool {
	if session, ok := aiCache[qID]; ok {
		// نوٹ: یہاں سے میں نے SenderID والا چیک ہٹا دیا ہے۔
		// اب کوئی بھی ممبر اگر بوٹ کے میسج کا رپلائی کرے گا، تو بوٹ اسی کو جواب دے گا۔
		
		delete(aiCache, qID) // پرانی آئی ڈی کیشے سے ڈیلیٹ کر دیں تاکہ ریم فری رہے
		
		// جس نئے ممبر نے میسج کیا ہے، اس کا میسج ہسٹری میں ایڈ کریں
		session.Messages = append(session.Messages, AIMessage{Role: "user", Content: bodyClean})
		
		// ہسٹری کو 15 میسجز تک محدود رکھیں
		if len(session.Messages) > 15 {
			session.Messages = append([]AIMessage{session.Messages[0]}, session.Messages[len(session.Messages)-14:]...)
		}

		// دوبارہ API کو ہٹ کریں
		go processAndSendAI(client, v, session)
		return true
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
