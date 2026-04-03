# ═══════════════════════════════════════════════════════════
# 1. Stage: Go Builder (Fast Compilation)
# ═══════════════════════════════════════════════════════════
FROM golang:1.25-bookworm AS go-builder

RUN apt-get update && apt-get install -y gcc libc6-dev git libsqlite3-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY . .

RUN rm -f go.mod go.sum || true
RUN go mod init bot && \
    go get go.mau.fi/whatsmeow@latest && \
    go get github.com/mattn/go-sqlite3@latest && \
    go get google.golang.org/protobuf/proto@latest && \
    go mod tidy

RUN CGO_ENABLED=1 GOOS=linux go build -v -ldflags="-s -w" -o bot .

# ═══════════════════════════════════════════════════════════
# 2. Stage: Final Runtime (1TB RAM Edition)
# ═══════════════════════════════════════════════════════════
FROM python:3.10-slim-bookworm

ENV PYTHONUNBUFFERED=1

# 🛠️ سسٹم لائبریریز اور Chromium کے لیے ضروری چیزیں
RUN apt-get update && apt-get install -y \
    ffmpeg curl sqlite3 libsqlite3-0 ca-certificates python3-pip \
    && rm -rf /var/lib/apt/lists/*

# 🚀 yt-dlp انسٹالیشن
RUN curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp \
    && chmod a+rx /usr/local/bin/yt-dlp

# 🐍 Python Packages (Playwright واپس آ گیا ہے)
RUN pip3 install --no-cache-dir requests playwright

# 🌍 Playwright Browsers انسٹالیشن (یہ ہیوی ہے لیکن آپ کے پاس ریم کی کمی نہیں)
RUN playwright install --with-deps chromium

WORKDIR /app

# 🚀 بلڈر سے فائلیں کاپی کریں
COPY --from=go-builder /app/bot ./bot
COPY tiktok_search.py ./tiktok_search.py
COPY index.html ./index.html

RUN mkdir -p data

ENV PORT=8080
EXPOSE 8080

CMD ["/app/bot"]
