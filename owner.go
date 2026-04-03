package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types/events"
)

var settingsDB *sql.DB

// BotSettings اسٹور کرنے کے لیے اسٹرکچر
type BotSettings struct {
	Prefix      string
	Mode        string // "public", "private", "admin"
	UptimeStart int64
}

// 📂 ڈیٹا بیس انیشلائز کریں (اسے main.go میں کال کریں گے)
func initSettingsDB() {
	var err error
	settingsDB, err = sql.Open("sqlite3", "file:./data/settings.db?_foreign_keys=on")
	if err != nil {
		log.Fatal("❌ Settings DB Error:", err)
	}

	createTableQuery := `
	CREATE TABLE IF NOT EXISTS bot_settings (
		jid TEXT PRIMARY KEY,
		prefix TEXT DEFAULT '.',
		mode TEXT DEFAULT 'public',
		uptime_start INTEGER
	);`
	_, err = settingsDB.Exec(createTableQuery)
	if err != nil {
		log.Fatal("❌ Table Creation Error:", err)
	}
}

// 🔄 کسی بھی سیشن کی سیٹنگز حاصل کریں
func getBotSettings(jid string) BotSettings {
	cleanJID := strings.Split(jid, ":")[0] // ڈیوائس آئی ڈی ریموو کرنے کے لیے

	var settings BotSettings
	err := settingsDB.QueryRow("SELECT prefix, mode, uptime_start FROM bot_settings WHERE jid = ?", cleanJID).Scan(&settings.Prefix, &settings.Mode, &settings.UptimeStart)
	
	if err == sql.ErrNoRows {
		// اگر نیا سیشن ہے تو ڈیفالٹ سیٹنگز اور اپ ٹائم سیو کریں
		now := time.Now().Unix()
		settingsDB.Exec("INSERT INTO bot_settings (jid, prefix, mode, uptime_start) VALUES (?, '.', 'public', ?)", cleanJID, now)
		return BotSettings{Prefix: ".", Mode: "public", UptimeStart: now}
	}
	return settings
}

// ⚙️ پریفکس اپڈیٹ کریں
func handleSetPrefix(client *whatsmeow.Client, v *events.Message, newPrefix string) {
	if newPrefix == "" {
		replyMessage(client, v, "❌ نیا پریفکس بھی لکھیں!\nمثال: `.setprefix !`")
		return
	}
	cleanJID := strings.Split(client.Store.ID.User, ":")[0]
	settingsDB.Exec("UPDATE bot_settings SET prefix = ? WHERE jid = ?", newPrefix, cleanJID)
	react(client, v.Info.Chat, v.Info.ID, "✅")
	replyMessage(client, v, fmt.Sprintf("✅ پریفکس کامیابی سے `%s` میں تبدیل ہو گیا ہے۔", newPrefix))
}

// ⚙️ موڈ اپڈیٹ کریں
func handleMode(client *whatsmeow.Client, v *events.Message, mode string) {
	mode = strings.ToLower(mode)
	if mode != "public" && mode != "private" && mode != "admin" {
		replyMessage(client, v, "❌ غلط موڈ!\nصرف یہ موڈز دستیاب ہیں: `public`, `private`, `admin`")
		return
	}
	cleanJID := strings.Split(client.Store.ID.User, ":")[0]
	settingsDB.Exec("UPDATE bot_settings SET mode = ? WHERE jid = ?", mode, cleanJID)
	react(client, v.Info.Chat, v.Info.ID, "✅")
	replyMessage(client, v, fmt.Sprintf("✅ بوٹ کا موڈ اب *%s* پر سیٹ ہو گیا ہے۔", strings.ToUpper(mode)))
}

// ⏱️ اپ ٹائم فارمیٹ کریں
func getUptimeString(startTime int64) string {
	duration := time.Since(time.Unix(startTime, 0))
	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	return fmt.Sprintf("%d Days, %d Hours, %d Mins", days, hours, minutes)
}
