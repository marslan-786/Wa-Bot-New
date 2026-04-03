package main

import (
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

// 🛡️ Updated Bot Settings Structure
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

// 📂 Initialize Database
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
		status_react BOOLEAN DEFAULT 0
	);`
	_, err = settingsDB.Exec(createTableQuery)
	if err != nil {
		log.Fatal("❌ Table Creation Error:", err)
	}
}

// 🔄 Fetch settings for any session
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

// ⚙️ Generic Toggle Function for On/Off Commands
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
	
	react(client, v.Info.Chat, v.Info.ID, "✅")
	replyMessage(client, v, fmt.Sprintf("✅ *%s* has been turned *%s*", settingName, strings.ToUpper(args)))

	// Special case for Always Online to apply immediately
	if dbColumn == "always_online" {
		if state {
			client.SendPresence(types.PresenceAvailable)
		} else {
			client.SendPresence(types.PresenceUnavailable)
		}
	}
}

// 📊 List all active bots
func handleListBots(client *whatsmeow.Client, v *events.Message) {
	var count int
	err := settingsDB.QueryRow("SELECT COUNT(*) FROM bot_settings").Scan(&count)
	if err != nil { count = 1 }

	replyMessage(client, v, fmt.Sprintf("🤖 *SILENT NEXUS ENGINE*\n\n🟢 Active Sessions: *%d*\n⚡ Powered by Whatsmeow", count))
}

// 💻 System Stats
func handleStats(client *whatsmeow.Client, v *events.Message, uptimeStart int64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	uptimeStr := getUptimeString(uptimeStart)
	ramUsage := m.Alloc / 1024 / 1024

	stats := fmt.Sprintf("📊 *SYSTEM POWER*\n\n⏱️ *Uptime:* %s\n💾 *RAM Usage:* %v MB\n⚙️ *Go Routines:* %d\n⚡ *Engine:* Llama 3 Fast", uptimeStr, ramUsage, runtime.NumGoroutine())
	replyMessage(client, v, stats)
}

// 🔄 Fetch settings for any session
func getBotSettings(jid string) BotSettings {
	cleanJID := strings.Split(jid, ":")[0] // Remove device ID

	var settings BotSettings
	err := settingsDB.QueryRow("SELECT prefix, mode, uptime_start FROM bot_settings WHERE jid = ?", cleanJID).Scan(&settings.Prefix, &settings.Mode, &settings.UptimeStart)
	
	if err == sql.ErrNoRows {
		// Save default settings and uptime for new sessions
		now := time.Now().Unix()
		settingsDB.Exec("INSERT INTO bot_settings (jid, prefix, mode, uptime_start) VALUES (?, '.', 'public', ?)", cleanJID, now)
		return BotSettings{Prefix: ".", Mode: "public", UptimeStart: now}
	}
	return settings
}

// ⚙️ Update Prefix
func handleSetPrefix(client *whatsmeow.Client, v *events.Message, newPrefix string) {
	if newPrefix == "" {
		replyMessage(client, v, "❌ Please provide a new prefix!\nExample: `.setprefix !`")
		return
	}
	cleanJID := strings.Split(client.Store.ID.User, ":")[0]
	settingsDB.Exec("UPDATE bot_settings SET prefix = ? WHERE jid = ?", newPrefix, cleanJID)
	react(client, v.Info.Chat, v.Info.ID, "✅")
	replyMessage(client, v, fmt.Sprintf("✅ Prefix successfully changed to `%s`", newPrefix))
}

// ⚙️ Update Mode
func handleMode(client *whatsmeow.Client, v *events.Message, mode string) {
	mode = strings.ToLower(mode)
	if mode != "public" && mode != "private" && mode != "admin" {
		replyMessage(client, v, "❌ Invalid mode!\nAvailable modes: `public`, `private`, `admin`")
		return
	}
	cleanJID := strings.Split(client.Store.ID.User, ":")[0]
	settingsDB.Exec("UPDATE bot_settings SET mode = ? WHERE jid = ?", mode, cleanJID)
	react(client, v.Info.Chat, v.Info.ID, "✅")
	replyMessage(client, v, fmt.Sprintf("✅ Bot mode has been successfully set to *%s*", strings.ToUpper(mode)))
}

// ⏱️ Format Uptime
func getUptimeString(startTime int64) string {
	duration := time.Since(time.Unix(startTime, 0))
	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	return fmt.Sprintf("%d Days, %d Hours, %d Mins", days, hours, minutes)
}
