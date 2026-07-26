#!/bin/sh
# Runs via the nginx image's /docker-entrypoint.d hook, before nginx starts.
# Ensures a TLS cert exists so `listen 443 ssl` can come up. If you mount your
# own tls.crt + tls.key into /etc/nginx/certs they are used as-is; otherwise a
# self-signed pair is generated (fine for internal use — the same posture as the
# monitors' InsecureSkipVerify, PRD §4.5).
set -e
CERT_DIR=/etc/nginx/certs
CRT="$CERT_DIR/tls.crt"
KEY="$CERT_DIR/tls.key"

if [ -s "$CRT" ] && [ -s "$KEY" ]; then
    echo "[echomap] using existing TLS cert at $CERT_DIR"
    exit 0
fi

echo "[echomap] no TLS cert found — generating a self-signed pair (365d, CN=echomap)."
echo "[echomap] mount your own tls.crt + tls.key into $CERT_DIR to replace it."
mkdir -p "$CERT_DIR"
openssl req -x509 -nodes -newkey rsa:2048 -days 365 \
    -keyout "$KEY" -out "$CRT" \
    -subj "/CN=echomap" \
    -addext "subjectAltName=DNS:localhost,DNS:echomap,IP:127.0.0.1" >/dev/null 2>&1
chmod 600 "$KEY"
