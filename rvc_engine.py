import sys
import json
import urllib.parse
import requests
import subprocess
import os
import random
import time
from playwright.sync_api import sync_playwright

# جدید یوزر ایجنٹس
USER_AGENTS = [
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
    "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:123.0) Gecko/20100101 Firefox/123.0",
    "Mozilla/5.0 (iPhone; CPU iPhone OS 17_3_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.3.1 Mobile/15E148 Safari/604.1"
]

def get_audio_duration(file_path):
    try:
        print(f"--- [DEBUG] Running ffprobe for duration: {file_path}")
        result = subprocess.run(
            ["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", file_path],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True
        )
        duration = float(result.stdout.strip())
        print(f"--- [INFO] Audio Duration: {duration} seconds")
        return duration
    except Exception as e:
        print(f"--- [ERROR] ffprobe failed: {str(e)}")
        return 3.0

def convert_voice(input_file, voice_id="7", pitch=16):
    print("\n" + "="*50)
    print(f"--- [START] New Conversion Thread ---")
    print(f"--- [FILE] {input_file} | [VOICE] {voice_id}")
    
    if not os.path.exists(input_file):
        print(f"--- [CRITICAL ERROR] File not found: {input_file}")
        return

    duration = get_audio_duration(input_file)
    selected_ua = random.choice(USER_AGENTS)
    print(f"--- [USER-AGENT] {selected_ua}")

    with sync_playwright() as p:
        print("--- [STEP 1] Launching Browser...")
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(user_agent=selected_ua, viewport={"width": 1280, "height": 720})
        page = context.new_page()
        
        try:
            # 1. ویب سائٹ وزٹ کرنا
            print(f"--- [STEP 2] Navigating to Voice.ai...")
            page.goto("https://voice.ai/tools/voice-changer", wait_until="networkidle", timeout=60000)
            
            # 2. ٹوکن نکالنا
            print("--- [STEP 3] Extracting XSRF-TOKEN...")
            cookies = context.cookies()
            xsrf_token = ""
            for cookie in cookies:
                if cookie['name'] == 'XSRF-TOKEN':
                    xsrf_token = urllib.parse.unquote(cookie['value'])
                    break
            
            if not xsrf_token:
                print("--- [STOP] XSRF-TOKEN not found. Possible Cloudflare block or site change.")
                return
            print(f"--- [TOKEN FOUND] {xsrf_token[:30]}...")

            # 3. گوگل کلاؤڈ یو آر ایل کی ریکویسٹ
            print("--- [STEP 4] Requesting Google Cloud Upload URL...")
            payload_step4 = {"file_type": "audio/mpeg", "filename": None}
            print(f"--- [REQ PAYLOAD] {payload_step4}")
            
            upload_req = context.request.post(
                "https://voice.ai/api/upload/get-google-url",
                headers={"x-xsrf-token": xsrf_token, "accept": "application/json"},
                data=payload_step4
            )
            
            print(f"--- [RESP STATUS] {upload_req.status}")
            if upload_req.status != 200:
                print(f"--- [STOP] Failed to get URL. Server Response: {upload_req.text()}")
                return

            upload_data = upload_req.json()
            google_url = upload_data["url"]
            file_key = upload_data["fileKey"]
            print(f"--- [UPLOAD DATA] FileKey: {file_key}")

            # 4. فائل اپلوڈ کرنا (Requests library)
            print("--- [STEP 5] Uploading Audio to Google Storage...")
            with open(input_file, "rb") as f:
                audio_bytes = f.read()
            
            put_headers = {"Content-Type": "audio/mpeg", "User-Agent": selected_ua}
            print(f"--- [PUT HEADERS] {put_headers}")
            
            put_resp = requests.put(google_url, data=audio_bytes, headers=put_headers)
            print(f"--- [PUT STATUS] {put_resp.status_code}")
            
            if put_resp.status_code != 200:
                print(f"--- [STOP] Google Storage upload failed. Resp: {put_resp.text}")
                return

            # 5. وائس کنورٹ کرنے کی اصل ریکویسٹ
            print("--- [STEP 6] Sending Conversion Request to API...")
            convert_payload = {
                "path": f"tmp/{file_key}",
                "original_filename": "wa_audio.mp3",
                "voice_id": str(voice_id),
                "pitch": int(pitch),
                "duration": float(duration)
            }
            print(f"--- [CONVERSION REQ] {json.dumps(convert_payload, indent=2)}")

            process_req = context.request.post(
                "https://voice.ai/api/web-tools/queue/store/voice-changer",
                headers={"x-xsrf-token": xsrf_token, "accept": "application/json"},
                data=convert_payload
            )
            
            print(f"--- [API RESP STATUS] {process_req.status}")
            result_text = process_req.text()
            print(f"--- [FULL API RESPONSE] {result_text}")
            
            result = json.loads(result_text)
            if "data" in result and "url" in result["data"]:
                final_link = result["data"]["url"]
                print(f"--- [SUCCESS] Conversion Successful!")
                print(f"RESULT_URL:{final_link}")
            else:
                print(f"--- [ERROR] Conversion did not return a URL. Message: {result.get('message')}")
                
        except Exception as e:
            print(f"--- [EXCEPTION] Error occurred during process: {str(e)}")
        finally:
            print("--- [FINISH] Cleaning up browser and exiting...")
            context.close()
            browser.close()
            print("="*50 + "\n")

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("--- [ERROR] No input file path provided.")
        sys.exit(1)
    
    # آپ چاہیں تو Arguments سے وائس آئی ڈی بھی پاس کر سکتے ہیں
    v_id = sys.argv[2] if len(sys.argv) > 2 else "7"
    convert_voice(sys.argv[1], voice_id=v_id)
