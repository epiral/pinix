# Pinix all-in-one Docker image
# One token, one command, everything runs.
#
# Usage:
#   docker run -d --shm-size=2g pinixai/pinix <hub-token>
#   docker run -d --shm-size=2g -p 9000:9000 pinixai/pinix <hub-token>
#
# What's inside:
#   pinix/pinixd    Pinix CLI + daemon
#   bun             Clip runtime
#   node/npm        bb-browser daemon runtime
#   bb-browser      Browser automation + WebRTC stream
#   bb-viewer       Video stream encoder (VP8)
#   Chrome          Full browser (headed on Xvfb)
#   Xvfb            Virtual display

FROM node:22-bookworm-slim

# China mirrors for faster builds
RUN sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list.d/debian.sources 2>/dev/null; \
    sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list 2>/dev/null; \
    true

# Chrome + Xvfb + runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl unzip fonts-noto-cjk fonts-noto-color-emoji \
    libnss3 libxss1 libasound2 libatk-bridge2.0-0 libgtk-3-0 libdrm2 \
    libgbm1 libx11-xcb1 libxcomposite1 libxdamage1 libxrandr2 \
    libpango-1.0-0 libcairo2 libcups2 libdbus-1-3 libexpat1 \
    libxext6 libxfixes3 libxkbcommon0 libatspi2.0-0 \
    xvfb procps \
    && rm -rf /var/lib/apt/lists/*

# Install pinix binary
ARG PINIX_URL="https://pinix-blobs-1251447449.cos.ap-beijing.myqcloud.com/releases/latest/pinix-linux-amd64"
RUN curl -fsSL "$PINIX_URL" -o /usr/local/bin/pinix && chmod +x /usr/local/bin/pinix

# Install Bun
RUN curl -fsSL https://bun.sh/install | bash && \
    ln -sf /root/.bun/bin/bun /usr/local/bin/bun

# Install bb-browser daemon (via npm)
RUN npm install -g bb-browser --registry=https://registry.npmmirror.com 2>/dev/null

# Pre-install bb-viewer (static-linked binary)
ARG BB_VIEWER_URL="https://pinix-blobs-1251447449.cos.ap-beijing.myqcloud.com/releases/bb-viewer/latest/bb-viewer-linux-amd64"
RUN mkdir -p /root/.bb-browser/bin && \
    curl -fsSL "$BB_VIEWER_URL" -o /root/.bb-browser/bin/bb-viewer && \
    chmod +x /root/.bb-browser/bin/bb-viewer

# Pre-install Chrome for Testing
ARG CHROME_URL="https://pinix-blobs-1251447449.cos.ap-beijing.myqcloud.com/releases/chrome/chrome-linux64.zip"
RUN mkdir -p /root/.bb-browser/browser && \
    curl -fsSL "$CHROME_URL" -o /tmp/chrome.zip && \
    unzip -q /tmp/chrome.zip -d /root/.bb-browser/browser && \
    chmod 755 /root/.bb-browser/browser/chrome-linux64/chrome && \
    echo "149.0.7827.22" > /root/.bb-browser/browser/version && \
    rm /tmp/chrome.zip

ENV DISPLAY=:99

EXPOSE 9000

COPY <<'ENTRYPOINT' /entrypoint.sh
#!/bin/sh
set -e

TOKEN="$1"
if [ -z "$TOKEN" ]; then
  echo "Usage: docker run --shm-size=2g pinixai/pinix <hub-token>"
  echo ""
  echo "Get your token at https://console.pinixai.com/settings"
  exit 1
fi

# Start Xvfb
Xvfb :99 -screen 0 1920x1080x24 -ac +render -noreset &
for i in 1 2 3 4 5 6 7 8 9 10; do
  xdpyinfo -display :99 >/dev/null 2>&1 && break
  sleep 0.5
done

# Start pinixd (it will auto-start bb-browser daemon)
pinix start
pinix login --token "$TOKEN"

echo ""
echo "Pinix is running."
echo "  Hub:     https://hub.pinixai.com"
echo "  Console: http://localhost:9000"
echo ""

# Keep container alive, follow pinixd logs
tail -f /root/.pinix/logs/pinixd.log 2>/dev/null || sleep infinity
ENTRYPOINT
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
