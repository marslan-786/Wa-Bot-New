import sys
import json
import urllib.parse
import requests
import subprocess
import os
import random
from playwright.sync_api import sync_playwright

# 10 مختلف یوزر ایجنٹس کی لسٹ
USER_AGENTS = [
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
    "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:123.0) Gecko/20100101 Firefox/123.0",
    "Mozilla/5.0 (AppleChromium; Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2.1 Safari/605.1.15",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Edge/120.0.0.0",
    "Mozilla/5.0 (iPhone; CPU iPhone OS 17_3_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.3.1 Mobile/15E148 Safari/604.1",
    "Mozilla/5.0 (Windows NT 11.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_2_1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
    "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:122.0) Gecko/20100101 Firefox/122.0"
]

def get_audio_duration(file_path):
    try:
        result = subprocess.run(
            ["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", file_path],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True
        )
        return float(result.stdout.strip())
    except:
        return 3.0

def convert_voice(input_file, voice_id="7", pitch=16):
    if not os.path.exists(input_file):
        print(f"[ERROR] Input file not found: {input_file}")
        return

    duration = get_audio_duration(input_file)
    
    # ہر ریکویسٹ کے لیے رینڈم یوزر ایجنٹ سلیکٹ کرنا
    selected_ua = random.choice(USER_AGENTS)

    with sync_playwright() as p:
        # ہر بار بالکل فریش براؤزر لانچ ہوگا (No shared Cache/Profile)
        browser = p.chromium.launch(headless=True)
        
        # 'new_context' کوکیز اور سیشن کو مکمل الگ رکھتا ہے
        # ہم نے یہاں Proxy کا آپشن بھی رکھا ہے اگر آپ بعد میں ڈالنا چاہیں
        context = browser.new_context(
            user_agent=selected_ua,
            viewport={"width": 1280, "height": 720},
            ignore_https_errors=True
        )
        
        page = context.new_page()
        
        try:
            print(f"[PROCESS] New Thread Initialized with UA: {selected_ua[:40]}...")
            page.goto("https://voice.ai/tools/voice-changer", wait_until="networkidle", timeout=60000)
            
            # ٹوکن نکالنا (ہر بار نیا ہوگا کیونکہ کانٹیکسٹ نیا ہے)
            cookies = context.cookies()
            xsrf_token = ""
            for cookie in cookies:
                if cookie['name'] == 'XSRF-TOKEN':
                    xsrf_token = urllib.parse.unquote(cookie['value'])
                    break
            
            if not xsrf_token:
                print("[CRITICAL] Token not found.")
                return

            # کلاؤڈ یو آر ایل ریکویسٹ
            upload_req = context.request.post(
                "https://voice.ai/api/upload/get-google-url",
                headers={"x-xsrf-token": xsrf_token, "accept": "application/json"},
                data={"file_type": "audio/mpeg", "filename": None}
            )
            
            if upload_req.status != 200: return

            upload_data = upload_req.json()
            google_url = upload_data["url"]
            file_key = upload_data["fileKey"]

            # فائل اپلوڈ کرنا (Requests کے ذریعے الگ سیشن میں)
            with open(input_file, "rb") as f:
                audio_bytes = f.read()
            
            # یہاں ہم رینڈم یوزر ایجنٹ ہی استعمال کر رہے ہیں تاکہ مطابقت رہے
            put_resp = requests.put(google_url, data=audio_bytes, headers={"Content-Type": "audio/mpeg", "User-Agent": selected_ua})
            
            if put_resp.status_code != 200: return

            # وائس چینج کی ریکویسٹ
            process_req = context.request.post(
                "https://voice.ai/api/web-tools/queue/store/voice-changer",
                headers={"x-xsrf-token": xsrf_token, "accept": "application/json"},
                data={
                    "path": f"tmp/{file_key}",
                    "original_filename": "input_audio.mp3",
                    "voice_id": str(voice_id),
                    "pitch": int(pitch),
                    "duration": float(duration)
                }
            )
            
            result = process_req.json()
            if "data" in result and "url" in result["data"]:
                print(f"RESULT_URL:{result['data']['url']}")
            else:
                print(f"[ERROR] API Error: {result.get('message')}")
                
        except Exception as e:
            print(f"[EXCEPTION] {str(e)}")
        finally:
            # سب کچھ کلوز کرنا تاکہ میموری ریلیز ہو جائے
            context.close()
            browser.close()

if __name__ == "__main__":
    if len(sys.argv) < 2:
        sys.exit(1)
    convert_voice(sys.argv[1])
