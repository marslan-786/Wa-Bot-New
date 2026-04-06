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
	PrivateAntiDelete bool
	AntiVV            bool
	AntiDM            bool
	AntiCall          bool
	AntiDelete        bool
}

func initSettingsDB() {
	var err error
	// فارن کیز (Foreign Keys) کے ساتھ ڈیٹا بیس اوپن کریں
	settingsDB, err = sql.Open("sqlite3", "file:./data/settings.db?_foreign_keys=on")
	if err != nil {
		log.Fatal("❌ Settings DB Error:", err)
	}

	// 1. سب سے پہلے بیسک ٹیبل بنائیں (صرف بنیادی کالمز کے ساتھ)
	createTableQuery := `
	CREATE TABLE IF NOT EXISTS bot_settings (
		jid TEXT PRIMARY KEY,
		prefix TEXT DEFAULT '.',
		mode TEXT DEFAULT 'public',
		uptime_start INTEGER
	);`
	settingsDB.Exec(createTableQuery)

	// 🛠️ SMART MIGRATION HELPER (یہ چیک کرے گا کہ کالم ہے یا نہیں)
	addColumnSafe := func(tableName, colName, colDef string) {
		query := fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name='%s'", tableName, colName)
		var count int
		err := settingsDB.QueryRow(query).Scan(&count)
		// اگر کالم موجود نہیں ہے (count == 0)، تبھی ALTER ٹیبل چلائے گا
		if err == nil && count == 0 {
			alterQuery := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tableName, colName, colDef)
			settingsDB.Exec(alterQuery)
			fmt.Printf("🔄 DB Migration: Added '%s' to '%s'\n", colName, tableName)
		}
	}

	// 2. اب اپنے سارے ایکسٹرا اور نئے فیچرز یہاں ایڈ کریں (یہ پرانے ڈیٹا بیس کو کریش نہیں ہونے دے گا)
	addColumnSafe("bot_settings", "always_online", "BOOLEAN DEFAULT 0")
	addColumnSafe("bot_settings", "auto_read", "BOOLEAN DEFAULT 0")
	addColumnSafe("bot_settings", "auto_react", "BOOLEAN DEFAULT 0")
	addColumnSafe("bot_settings", "auto_status", "BOOLEAN DEFAULT 0")
	addColumnSafe("bot_settings", "status_react", "BOOLEAN DEFAULT 0")
	addColumnSafe("bot_settings", "private_antidelete", "BOOLEAN DEFAULT 0")
	addColumnSafe("bot_settings", "anti_vv", "BOOLEAN DEFAULT 0")
	addColumnSafe("bot_settings", "anti_dm", "BOOLEAN DEFAULT 0")
	addColumnSafe("bot_settings", "anti_call", "BOOLEAN DEFAULT 0")
	addColumnSafe("bot_settings", "anti_delete", "BOOLEAN DEFAULT 0")

	// 3. اینٹی ڈیلیٹ کے لیے میسج کیش ٹیبل
	createCacheQuery := `
	CREATE TABLE IF NOT EXISTS message_cache (
		msg_id TEXT PRIMARY KEY,
		sender_jid TEXT,
		msg_content BLOB,
		timestamp INTEGER
	);`
	settingsDB.Exec(createCacheQuery)

	// 4. آٹو کلین اپ (24 گھنٹے پرانے میسجز ڈیلیٹ کرنے کے لیے)
	go func() {
		for {
			time.Sleep(1 * time.Hour)
			oneDayAgo := time.Now().Unix() - (24 * 60 * 60)
			settingsDB.Exec("DELETE FROM message_cache WHERE timestamp < ?", oneDayAgo)
		}
	}()
	
	fmt.Println("✅ Database Initialized & Migrated Safely!")
}

func getBotSettings(client *whatsmeow.Client) BotSettings {
	cleanJID := client.Store.ID.ToNonAD().User

	var settings BotSettings
	
	// 🌟 FIX: SELECT کیوری میں private_antidelete, anti_vv اور anti_dm کا اضافہ کر دیا گیا ہے
	err := settingsDB.QueryRow("SELECT prefix, mode, uptime_start, always_online, auto_read, auto_react, auto_status, status_react, private_antidelete, anti_vv, anti_dm FROM bot_settings WHERE jid = ?", cleanJID).Scan(
		&settings.Prefix, 
		&settings.Mode, 
		&settings.UptimeStart, 
		&settings.AlwaysOnline, 
		&settings.AutoRead, 
		&settings.AutoReact, 
		&settings.AutoStatus, 
		&settings.StatusReact, 
		&settings.PrivateAntiDelete, // 👈 نیا
		&settings.AntiVV,            // 👈 نیا
		&settings.AntiDM,            // 👈 نیا
	)
	
	if err == sql.ErrNoRows {
		now := time.Now().Unix()
		// اگر نیا یوزر ہے تو ڈیفالٹ سیٹنگز انسرٹ کریں
		settingsDB.Exec("INSERT INTO bot_settings (jid, uptime_start) VALUES (?, ?)", cleanJID, now)
		return BotSettings{Prefix: ".", Mode: "public", UptimeStart: now}
	}
	
	return settings
}

// 👑 OWNER CHECKER FUNCTION
// ==========================================
// 👑 DYNAMIC OWNER CHECK (Messages)
// ==========================================
func isOwner(client *whatsmeow.Client, v *events.Message) bool {
	// بوٹ کا اپنا اصلی نمبر نکالیں
	botJID := client.Store.ID.ToNonAD().User 
	
	// سینڈر کا اصلی نمبر نکالنے کی لاجک (LID Bypass)
	realSender := v.Info.Sender.ToNonAD().User
	if v.Info.Sender.Server == types.HiddenUserServer && !v.Info.SenderAlt.IsEmpty() {
		realSender = v.Info.SenderAlt.ToNonAD().User
	}

	// اگر سینڈر کا نمبر اور بوٹ کا نمبر سیم ہے، تو وہ اونر ہے!
	return realSender == botJID
}

// ==========================================
// 👑 DYNAMIC OWNER CHECK (Calls)
// ==========================================
// کالز کے ایونٹ میں v.Info نہیں ہوتا، اس لیے اس کا فنکشن الگ سے بنانا پڑتا ہے
func isCallOwner(client *whatsmeow.Client, callerJID types.JID) bool {
	botJID := client.Store.ID.ToNonAD().User
	return callerJID.ToNonAD().User == botJID
}

// ==========================================
// ⚙️ HELPER: Toggle Settings (Dynamic ON/OFF)
// ==========================================
func handleToggleSettings(client *whatsmeow.Client, v *events.Message, columnName string, args string) {
	args = strings.ToLower(strings.TrimSpace(args))
	
	if args != "on" && args != "off" {
		replyMessage(client, v, "❌ *Usage:* `.command on` or `.command off`")
		return
	}

	state := false
	if args == "on" { state = true }

	cleanJID := client.Store.ID.ToNonAD().User

	// 🌟 DYNAMIC QUERY: یہ جس بھی کالم کا نام دو گے (anti_dm, anti_call وغیرہ)، اسے اپڈیٹ کر دے گا
	query := fmt.Sprintf("UPDATE bot_settings SET %s = ? WHERE jid = ?", columnName)
	_, err := settingsDB.Exec(query, state, cleanJID)

	if err != nil {
		react(client, v.Info.Chat, v.Info.ID, "❌")
		replyMessage(client, v, "❌ Database error!")
		return
	}

	react(client, v.Info.Chat, v.Info.ID, "✅")
	
	// خوبصورت ریپلائی بنانے کے لیے (مثلاً "anti_dm" کو "ANTI DM" کر دے گا)
	featureName := strings.ReplaceAll(strings.ToUpper(columnName), "_", " ")
	replyMessage(client, v, fmt.Sprintf("🛡️ *%s* is now *%s*", featureName, strings.ToUpper(args)))
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
