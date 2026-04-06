package main

import (
	"context" // 🛠️ FIX: یہ امپورٹ اب لازمی ہو گیا ہے
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// ==========================================
// 🌐 GLOBAL VARIABLES
// ==========================================
var activeClients = make(map[string]*whatsmeow.Client)
var clientsMutex sync.RWMutex
var dbContainer *sqlstore.Container

// ==========================================
// 📂 1. DATABASE INITIALIZATION
// ==========================================
// ==========================================
// 📂 1. DATABASE INITIALIZATION
// ==========================================
func initDB() {
	// 🛠️ FIX: "WARN" کو "ERROR" کر دیا ہے تاکہ کنسول صاف رہے
	dbLog := waLog.Noop

	err := os.MkdirAll("./data", 0755)
	if err != nil {
		log.Fatal("❌ Data directory create error:", err)
	}

	dbContainer, err = sqlstore.New(context.Background(), "sqlite3", "file:./data/sessions.db?_foreign_keys=on", dbLog)
	if err != nil {
		log.Fatal("❌ Database connection error:", err)
	}
	
	log.Println("✅ Database Initialized Successfully!")
}

// ==========================================
// 🔄 2. AUTO-CONNECT ALL SESSIONS
// ==========================================
func RunAllSessions() {
	devices, err := dbContainer.GetAllDevices(context.Background())
	if err != nil {
		log.Println("❌ Error fetching devices:", err)
		return
	}

	for _, device := range devices {
		// 🛠️ FIX: "WARN" کو "ERROR" کر دیا تاکہ Decryption ایررز بوٹ کو سلو نہ کریں
		clientLog := waLog.Stdout("Client", "ERROR", true)
		client := whatsmeow.NewClient(device, clientLog)

		client.AddEventHandler(func(evt interface{}) {
			EventHandler(client, evt)
		})

		err := client.Connect()
		if err != nil {
			log.Printf("❌ Failed to auto-connect session %s: %v", device.ID.User, err)
			continue
		}

		clientsMutex.Lock()
		activeClients[device.ID.User] = client
		clientsMutex.Unlock()

		log.Printf("🟢 Session %s successfully auto-connected!", device.ID.User)

		// ==========================================
		// 🌟 FIX: Goroutine کو لوپ کے اندر رکھا گیا ہے
		// ==========================================
		// ہم client کو ایز اے پیرامیٹر (c) پاس کر رہے ہیں تاکہ میموری مکس نہ ہو
		go func(c *whatsmeow.Client) {
			// 1. بوٹ کنیکٹ ہوتے ہی فوراً ایک بار لسٹ اپڈیٹ کریں
			if c.IsConnected() {
				syncBotContacts(c)
			}
			
			// 2. اس کے بعد ہر 5 گھنٹے کا لوپ شروع کر دیں
			for {
				time.Sleep(5 * time.Hour)
				if c.IsConnected() {
					syncBotContacts(c)
				}
			}
		}(client) // 👈 یہاں سے کرنٹ کلائنٹ اندر پاس ہو رہا ہے
	}
}

// ==========================================
// 📱 3. PAIRING NEW SESSION
// ==========================================
func ConnectNewSession(w http.ResponseWriter, r *http.Request) {
	phone := r.URL.Query().Get("phone")
	if phone == "" {
		http.Error(w, "Phone number required", http.StatusBadRequest)
		return
	}

	deviceStore := dbContainer.NewDevice()
	
	// 🛠️ FIX: "INFO" کو "ERROR" کر دیا تاکہ فالتو لاگز نہ آئیں
	clientLog := waLog.Noop
	client := whatsmeow.NewClient(deviceStore, clientLog)

	client.AddEventHandler(func(evt interface{}) {
		EventHandler(client, evt)
	})

	err := client.Connect()
	if err != nil {
		http.Error(w, "Failed to connect to WhatsApp servers", http.StatusInternalServerError)
		return
	}

	code, err := client.PairPhone(context.Background(), phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		http.Error(w, "Failed to get pairing code", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "%s", code)

	log.Printf("🔗 Pairing code generated for: %s", phone)
}


// ==========================================
// 🚀 4. MAIN ENGINE START
// ==========================================
func main() {
	log.Println("🚀 Starting Silent Nexus Engine...")

	initDB()
	initSettingsDB()
	initGroupDB()
	initContactsDB() // 👈 نیا کانٹیکٹ ٹیبل بنانا نہ بھولنا

	// سیشنز چلائیں (یہ فنکشن اب خود ہر سیشن کے لیے سنک لوپ شروع کرے گا)
	RunAllSessions()

	// ویب سرور کی سیٹنگز
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})
	http.HandleFunc("/pair", ConnectNewSession)

	port := os.Getenv("PORT")
	if port == "" { port = "8080" }

	log.Printf("🌐 Web Server is running on port %s...", port)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Fatal("❌ Web Server Crashed:", err)
	}
}
