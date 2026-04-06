import sys
import json
import urllib.parse
import requests
import subprocess
import os
import random
import time
from playwright.sync_api import sync_playwright

# فائل جہاں سیشن محفوظ ہوں گے
SESSION_FILE = "proxy_sessions.json"

PROXY_LIST = [
    "31.59.20.176:6754:wwwsyxzg:582ygxexguhx",
    "23.95.150.145:6114:wwwsyxzg:582ygxexguhx",
    "198.23.239.134:6540:wwwsyxzg:582ygxexguhx",
    "45.38.107.97:6014:wwwsyxzg:582ygxexguhx",
    "107.172.163.27:6543:wwwsyxzg:582ygxexguhx",
    "198.105.121.200:6462:wwwsyxzg:582ygxexguhx",
    "216.10.27.159:6837:wwwsyxzg:582ygxexguhx",
    "142.111.67.146:5611:wwwsyxzg:582ygxexguhx",
    "191.96.254.138:6185:wwwsyxzg:582ygxexguhx",
    "31.58.9.4:6077:wwwsyxzg:582ygxexguhx"
]

def load_sessions():
    if os.path.exists(SESSION_FILE):
        with open(SESSION_FILE, "r") as f:
            return json.load(f)
    return {}

def save_sessions(sessions):
    with open(SESSION_FILE, "w") as f:
        json.dump(sessions, f, indent=4)

def get_audio_duration(file_path):
    try:
        result = subprocess.run(
            ["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", file_path],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True
        )
        return float(result.stdout.strip())
    except:
        return 3.0

def convert_voice(input_file, voice_id="7", pitch=12):
    print("\n" + "="*50)
    sessions = load_sessions()
    
    # ایک ایسی پروکسی ڈھونڈنا جس کے 5 سے کم استعمال ہوں
    active_proxy = None
    for p in PROXY_LIST:
        if p in sessions and sessions[p]['count'] < 5:
            active_proxy = p
            print(f"--- [REUSING SESSION] Proxy: {p.split(':')[0]} | Count: {sessions[p]['count']}/5")
            break
    
    # اگر کوئی پرانی پروکسی نہیں ملی تو نئی سلیکٹ کریں
    if not active_proxy:
        active_proxy = random.choice(PROXY_LIST)
        sessions[active_proxy] = {"count": 0, "token": "", "cookies": []}
        print(f"--- [NEW SESSION] Starting fresh with Proxy: {active_proxy.split(':')[0]}")

    p_ip, p_port, p_user, p_pass = active_proxy.split(':')
    playwright_proxy = {"server": f"http://{p_ip}:{p_port}", "username": p_user, "password": p_pass}
    requests_proxies = {"http": f"http://{p_user}:{p_pass}@{p_ip}:{p_port}", "https": f"http://{p_user}:{p_pass}@{p_ip}:{p_port}"}

    duration = get_audio_duration(input_file)

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        
        # اگر پرانے کوکیز موجود ہیں تو وہ لوڈ کریں
        context = browser.new_context(
            proxy=playwright_proxy,
            user_agent="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
        )
        
        if sessions[active_proxy]['cookies']:
            context.add_cookies(sessions[active_proxy]['cookies'])

        page = context.new_page()
        success = False

        try:
            # اگر ٹوکن نہیں ہے تو ویب سائٹ پر جائیں، ورنہ ڈائریکٹ API ہٹ کریں
            xsrf_token = sessions[active_proxy]['token']
            
            if not xsrf_token:
                print("--- [STEP] Fetching fresh XSRF-TOKEN...")
                page.goto("https://voice.ai/tools/voice-changer", wait_until="networkidle", timeout=60000)
                for cookie in context.cookies():
                    if cookie['name'] == 'XSRF-TOKEN':
                        xsrf_token = urllib.parse.unquote(cookie['value'])
                        break
            
            if not xsrf_token:
                print("--- [ERROR] Token Extraction Failed.")
                # فیل ہونے پر سیشن ری سیٹ کریں
                del sessions[active_proxy]
                save_sessions(sessions)
                return

            # API Call: Get Google URL
            upload_req = context.request.post(
                "https://voice.ai/api/upload/get-google-url",
                headers={"x-xsrf-token": xsrf_token, "accept": "application/json"},
                data={"file_type": "audio/mpeg", "filename": None}
            )
            
            if upload_req.status != 200:
                print(f"--- [FAILED] Session expired or Proxy blocked. Resetting...")
                del sessions[active_proxy]
                save_sessions(sessions)
                return

            upload_data = upload_req.json()
            google_url, file_key = upload_data["url"], upload_data["fileKey"]

            # Step: Upload to Google
            with open(input_file, "rb") as f:
                put_resp = requests.put(google_url, data=f, proxies=requests_proxies, timeout=60)
            
            if put_resp.status_code == 200:
                # Step: Final Conversion
                process_req = context.request.post(
                    "https://voice.ai/api/web-tools/queue/store/voice-changer",
                    headers={"x-xsrf-token": xsrf_token, "accept": "application/json"},
                    data={"path": f"tmp/{file_key}", "original_filename": "in.mp3", "voice_id": str(voice_id), "pitch": int(pitch), "duration": float(duration)}
                )
                
                result = process_req.json()
                if "data" in result and "url" in result["data"]:
                    print(f"RESULT_URL:{result['data']['url']}")
                    success = True
                    # سیشن اپ ڈیٹ کریں
                    sessions[active_proxy]['count'] += 1
                    sessions[active_proxy]['token'] = xsrf_token
                    sessions[active_proxy]['cookies'] = context.cookies()
                    
                    # اگر 5 پورے ہو گئے تو سیشن ختم
                    if sessions[active_proxy]['count'] >= 5:
                        print("--- [LIMIT REACHED] 5 uses completed. Clearing session.")
                        del sessions[active_proxy]
                else:
                    print(f"--- [API ERROR] {result.get('message')}")

        except Exception as e:
            print(f"--- [EXCEPTION] {str(e)}")
            # کسی بھی بڑے ایرر پر سیشن ختم کر دیں تاکہ اگلی بار نیا بنے
            if active_proxy in sessions: del sessions[active_proxy]

        finally:
            save_sessions(sessions)
            context.close()
            browser.close()
            print("="*50 + "\n")

if __name__ == "__main__":
    if len(sys.argv) < 2: sys.exit(1)
    v_id = sys.argv[2] if len(sys.argv) > 2 else "7"
    convert_voice(sys.argv[1], voice_id=v_id)
