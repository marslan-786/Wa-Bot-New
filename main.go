package main

import (
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
// یہ میپ تمام ایکٹو بوٹس (ملٹی ڈیوائس) کا ریکارڈ رکھے گا
var activeClients = make(map[string]*whatsmeow.Client)
var clientsMutex sync.RWMutex

// ڈیٹا بیس کا کنٹینر
var dbContainer *sqlstore.Container

// ==========================================
// 📂 1. DATABASE INITIALIZATION (Railway Volume)
// ==========================================
func initDB() {
	dbLog := waLog.Stdout("Database", "WARN", true)

	// data فولڈر بنائیں (Railway Volume یہاں ماؤنٹ ہوگا)
	err := os.MkdirAll("./data", 0755)
	if err != nil {
		log.Fatal("❌ Data directory create error:", err)
	}

	// SQLite ڈیٹا بیس کنیکٹ کریں
	dbContainer, err = sqlstore.New("sqlite3", "file:./data/sessions.db?_foreign_keys=on", dbLog)
	if err != nil {
		log.Fatal("❌ Database connection error:", err)
	}
	
	log.Println("✅ Database Initialized Successfully!")
}

// ==========================================
// 🔄 2. AUTO-CONNECT ALL SESSIONS
// ==========================================
// یہ فنکشن سرور ری سٹارٹ ہونے پر تمام پرانے سیشنز کو خودکار طریقے سے لائیو کرے گا
func RunAllSessions() {
	devices, err := dbContainer.GetAllDevices()
	if err != nil {
		log.Println("❌ Error fetching devices:", err)
		return
	}

	for _, device := range devices {
		clientLog := waLog.Stdout("Client", "WARN", true)
		client := whatsmeow.NewClient(device, clientLog)

		// 🔗 کمانڈز والی فائل (commands.go) کا ہینڈلر یہاں اٹیچ کریں
		client.AddEventHandler(func(evt interface{}) {
			EventHandler(client, evt)
		})

		err := client.Connect()
		if err != nil {
			log.Printf("❌ Failed to auto-connect session %s: %v", device.ID.User, err)
			continue
		}

		// بوٹ کو میپ میں محفوظ کریں
		clientsMutex.Lock()
		activeClients[device.ID.User] = client
		clientsMutex.Unlock()

		log.Printf("🟢 Session %s successfully auto-connected!", device.ID.User)
	}
}

// ==========================================
// 📱 3. PAIRING NEW SESSION (Web Interface)
// ==========================================
func ConnectNewSession(w http.ResponseWriter, r *http.Request) {
	phone := r.URL.Query().Get("phone")
	if phone == "" {
		http.Error(w, "Phone number required", http.StatusBadRequest)
		return
	}

	deviceStore := dbContainer.NewDevice()
	clientLog := waLog.Stdout("Client", "INFO", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	// 🔗 نیو بوٹ کے ساتھ بھی ہینڈلر اٹیچ کریں تاکہ وہ فوراً کام شروع کر دے
	client.AddEventHandler(func(evt interface{}) {
		EventHandler(client, evt)
	})

	err := client.Connect()
	if err != nil {
		http.Error(w, "Failed to connect to WhatsApp servers", http.StatusInternalServerError)
		return
	}

	// پیئرنگ کوڈ جنریٹ کریں
	code, err := client.PairPhone(phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
	if err != nil {
		http.Error(w, "Failed to get pairing code", http.StatusInternalServerError)
		return
	}

	// کوڈ کو HTML فرنٹ اینڈ پر بھیجیں
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "%s", code)

	log.Printf("🔗 Pairing code generated for: %s", phone)
}

// ==========================================
// 🚀 4. MAIN ENGINE START
// ==========================================
func main() {
	log.Println("🚀 Starting Silent Nexus Engine...")

	// 1. ڈیٹا بیس سیٹ اپ کریں
	initDB()

	// 2. تمام محفوظ شدہ بوٹس کو لائیو کریں
	RunAllSessions()

	// 3. ویب سرور کے روٹس (Routes)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html") // یہ آپ کا VIP ڈیزائن والا پیج ہے
	})
	http.HandleFunc("/pair", ConnectNewSession)

	// 4. پورٹ سیٹ کریں (Railway خودکار طور پر PORT دیتا ہے)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🌐 Web Server is running on port %s...", port)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Fatal("❌ Web Server Crashed:", err)
	}
}
