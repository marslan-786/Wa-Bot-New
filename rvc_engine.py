import sys
import json
import urllib.parse
import requests
import subprocess
import os
from playwright.sync_api import sync_playwright

# آڈیو کی لمبائی نکالنے کا ہلکا طریقہ (بغیر لبراوسا کے)
def get_audio_duration(file_path):
    try:
        print(f"[INFO] Fetching duration for: {file_path}")
        result = subprocess.run(
            ["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", file_path],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True
        )
        return float(result.stdout.strip())
    except Exception as e:
        print(f"[ERROR] Could not get duration: {str(e)}")
        return 3.0

def convert_voice(input_file, voice_id="7", pitch=16):
    if not os.path.exists(input_file):
        print(f"[ERROR] Input file not found: {input_file}")
        return

    duration = get_audio_duration(input_file)
    
    with sync_playwright() as p:
        print("[PROCESS] Launching a fresh isolated browser...")
        # ہر بار ایک نیا براؤزر لانچ کرنا تاکہ پرانی کوئی بھی میموری باقی نہ رہے
        browser = p.chromium.launch(headless=True)
        
        # 'new_context' کوکیز، لوکل سٹوریج اور انڈیکس ڈی بی کو خود بخود کلیئر رکھتا ہے
        # ہم یوزر ایجنٹ کو بھی تھوڑا رینڈم کر سکتے ہیں تاکہ ویب سائٹ کو لگے کہ ڈیوائس بدل گئی ہے
        context = browser.new_context(
            user_agent="Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36",
            viewport={"width": 1280, "height": 720}
        )
        
        page = context.new_page()
        
        try:
            print("[STEP 1] Navigating to Voice.ai and bypassing Cloudflare...")
            page.goto("https://voice.ai/tools/voice-changer", wait_until="networkidle", timeout=60000)
            
            # ٹوکن نکالنا
            cookies = context.cookies()
            xsrf_token = ""
            for cookie in cookies:
                if cookie['name'] == 'XSRF-TOKEN':
                    xsrf_token = urllib.parse.unquote(cookie['value'])
                    break
            
            if not xsrf_token:
                print("[CRITICAL ERROR] XSRF-TOKEN not found. Cloudflare might have blocked the script.")
                return

            print(f"[SUCCESS] Token Acquired: {xsrf_token[:20]}...")

            # گوگل کلاؤڈ یو آر ایل لینا
            print("[STEP 2] Requesting Google Cloud Upload URL...")
            upload_req = context.request.post(
                "https://voice.ai/api/upload/get-google-url",
                headers={"x-xsrf-token": xsrf_token, "accept": "application/json"},
                data={"file_type": "audio/mpeg", "filename": None}
            )
            
            if upload_req.status != 200:
                print(f"[ERROR] Failed to get upload URL. Status: {upload_req.status}, Response: {upload_req.text()}")
                return

            upload_data = upload_req.json()
            google_url = upload_data["url"]
            file_key = upload_data["fileKey"]
            print(f"[INFO] Upload URL generated. Key: {file_key}")

            # فائل اپلوڈ کرنا
            print("[STEP 3] Uploading audio to Google Storage...")
            with open(input_file, "rb") as f:
                audio_bytes = f.read()
            
            put_resp = requests.put(google_url, data=audio_bytes, headers={"Content-Type": "audio/mpeg"})
            if put_resp.status_code != 200:
                print(f"[ERROR] Audio upload failed. Status: {put_resp.status_code}")
                return
            print("[SUCCESS] Audio uploaded successfully.")

            # وائس چینج کی ریکویسٹ
            print(f"[STEP 4] Sending conversion request (Voice ID: {voice_id})...")
            process_req = context.request.post(
                "https://voice.ai/api/web-tools/queue/store/voice-changer",
                headers={"x-xsrf-token": xsrf_token, "accept": "application/json"},
                data={
                    "path": f"tmp/{file_key}",
                    "original_filename": "wa_audio.mp3",
                    "voice_id": str(voice_id),
                    "pitch": int(pitch),
                    "duration": float(duration)
                }
            )
            
            result_text = process_req.text()
            print(f"[DEBUG] API Full Response: {result_text}")
            
            result = json.loads(result_text)
            if "data" in result and "url" in result["data"]:
                final_url = result["data"]["url"]
                print(f"[COMPLETE] Result Found: {final_url}")
                # یہ لنک Go بوٹ ریڈ کرے گا
                print(f"RESULT_URL:{final_url}")
            else:
                print(f"[ERROR] Conversion failed. Server said: {result.get('message', 'No message')}")
                
        except Exception as e:
            print(f"[EXCEPTION] Something went wrong in Python script: {str(e)}")
        finally:
            print("[INFO] Closing browser and clearing context...")
            context.close()
            browser.close()

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("[ERROR] No input file provided.")
        sys.exit(1)
    
    convert_voice(sys.argv[1])