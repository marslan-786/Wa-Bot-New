package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"runtime"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

var settingsDB *sql.DB

type BotSettings struct {
	Prefix          string
	Mode            string
	UptimeStart     int64
	AlwaysOnline    bool
	AutoRead        bool
	AutoReact       bool
	AutoStatus      bool
	StatusReact     bool
}

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
		uptime_start INTEGER,
		always_online BOOLEAN DEFAULT 0,
		auto_read BOOLEAN DEFAULT 0,
		auto_react BOOLEAN DEFAULT 0,
		auto_status BOOLEAN DEFAULT 0,
		status_react BOOLEAN DEFAULT 0,
		private_antidelete BOOLEAN DEFAULT 0
		anti_vv BOOLEAN DEFAULT 0
		
	);`
	settingsDB.Exec(createTableQuery)

	// 🛡️ ANTI-DELETE CACHE TABLE
	createCacheQuery := `
	CREATE TABLE IF NOT EXISTS message_cache (
		msg_id TEXT PRIMARY KEY,
		sender_jid TEXT,
		msg_content BLOB,
		timestamp INTEGER
	);`
	settingsDB.Exec(createCacheQuery)

	go func() {
		for {
			time.Sleep(1 * time.Hour)
			oneDayAgo := time.Now().Unix() - (24 * 60 * 60)
			settingsDB.Exec("DELETE FROM message_cache WHERE timestamp < ?", oneDayAgo)
		// settingsDB.Exec کے نیچے یہ لائن ایڈ کریں یا ALTER کمانڈ چلائیں
            settingsDB.Exec("ALTER TABLE bot_settings ADD COLUMN anti_dm BOOLEAN DEFAULT 0;")
		}
	}()
}

func getBotSettings(client *whatsmeow.Client) BotSettings {
	cleanJID := client.Store.ID.ToNonAD().User

	var settings BotSettings
	err := settingsDB.QueryRow("SELECT prefix, mode, uptime_start, always_online, auto_read, auto_react, auto_status, status_react FROM bot_settings WHERE jid = ?", cleanJID).Scan(
		&settings.Prefix, &settings.Mode, &settings.UptimeStart, &settings.AlwaysOnline, &settings.AutoRead, &settings.AutoReact, &settings.AutoStatus, &settings.StatusReact)
	
	if err == sql.ErrNoRows {
		now := time.Now().Unix()
		settingsDB.Exec("INSERT INTO bot_settings (jid, uptime_start) VALUES (?, ?)", cleanJID, now)
		return BotSettings{Prefix: ".", Mode: "public", UptimeStart: now}
	}
	return settings
}

// 👑 OWNER CHECKER FUNCTION
func isOwner(client *whatsmeow.Client, v *events.Message) bool {
	if v.Info.IsFromMe {
		return true
	}
	masterOwner := "923017552805" // 👈 یہاں اپنا اصل نمبر ڈال لینا
	senderNum := v.Info.Sender.ToNonAD().User
	return senderNum == masterOwner
}

func handleToggleSetting(client *whatsmeow.Client, v *events.Message, settingName string, dbColumn string, args string) {
	args = strings.ToLower(strings.TrimSpace(args))
	if args != "on" && args != "off" {
		replyMessage(client, v, fmt.Sprintf("❌ Invalid usage! Use: `%s on` or `%s off`", settingName, settingName))
		return
	}

	state := false
	if args == "on" { state = true }

	cleanJID := client.Store.ID.ToNonAD().User
	query := fmt.Sprintf("UPDATE bot_settings SET %s = ? WHERE jid = ?", dbColumn)
	settingsDB.Exec(query, state, cleanJID)
	
	// FIX: SendPresence اب context مانگتا ہے
	if dbColumn == "always_online" {
		if state {
			client.SendPresence(context.Background(), types.PresenceAvailable)
		} else {
			client.SendPresence(context.Background(), types.PresenceUnavailable)
		}
	}
	
	replyMessage(client, v, fmt.Sprintf("✅ *%s* has been turned *%s*", settingName, strings.ToUpper(args)))
}

func handleSetPrefix(client *whatsmeow.Client, v *events.Message, newPrefix string) {
	if newPrefix == "" {
		replyMessage(client, v, "❌ Please provide a new prefix!\nExample: `.setprefix !`")
		return
	}
	cleanJID := client.Store.ID.ToNonAD().User
	settingsDB.Exec("UPDATE bot_settings SET prefix = ? WHERE jid = ?", newPrefix, cleanJID)
	replyMessage(client, v, fmt.Sprintf("✅ Prefix successfully changed to `%s`", newPrefix))
}

func handleMode(client *whatsmeow.Client, v *events.Message, mode string) {
	mode = strings.ToLower(mode)
	if mode != "public" && mode != "private" && mode != "admin" {
		replyMessage(client, v, "❌ Invalid mode!\nAvailable modes: `public`, `private`, `admin`")
		return
	}
	cleanJID := client.Store.ID.ToNonAD().User
	settingsDB.Exec("UPDATE bot_settings SET mode = ? WHERE jid = ?", mode, cleanJID)
	replyMessage(client, v, fmt.Sprintf("✅ Bot mode has been successfully set to *%s*", strings.ToUpper(mode)))
}

func getUptimeString(startTime int64) string {
	duration := time.Since(time.Unix(startTime, 0))
	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	return fmt.Sprintf("%d Days, %d Hours, %d Mins", days, hours, minutes)
}

func handleListBots(client *whatsmeow.Client, v *events.Message) {
	var count int
	err := settingsDB.QueryRow("SELECT COUNT(*) FROM bot_settings").Scan(&count)
	if err != nil { count = 1 }
	replyMessage(client, v, fmt.Sprintf("🤖 *SILENT NEXUS ENGINE*\n\n🟢 Active Sessions: *%d*\n⚡ Powered by Whatsmeow", count))
}

func handleStats(client *whatsmeow.Client, v *events.Message, uptimeStart int64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	uptimeStr := getUptimeString(uptimeStart)
	ramUsage := m.Alloc / 1024 / 1024
	stats := fmt.Sprintf("📊 *SYSTEM POWER*\n\n⏱️ *Uptime:* %s\n💾 *RAM Usage:* %v MB\n⚙️ *Go Routines:* %d\n⚡ *Engine:* Llama 3 Fast", uptimeStr, ramUsage, runtime.NumGoroutine())
	replyMessage(client, v, stats)
}
