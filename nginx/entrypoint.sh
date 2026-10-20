#!/bin/sh
# Generates a self-signed certificate on first start if none is mounted, then
# runs nginx in the foreground. Mount real certificates at /etc/nginx/certs to
# use them instead (fullchain.pem + privkey.pem).
set -e

CERT_DIR=/etc/nginx/certs
mkdir -p "$CERT_DIR"

if [ ! -f "$CERT_DIR/fullchain.pem" ] || [ ! -f "$CERT_DIR/privkey.pem" ]; then
  echo "nginx: generating self-signed certificate for CN=${SERVER_NAME:-localhost}"
  openssl req -x509 -nodes -newkey rsa:2048 -days 365 \
    -keyout "$CERT_DIR/privkey.pem" \
    -out "$CERT_DIR/fullchain.pem" \
    -subj "/CN=${SERVER_NAME:-localhost}"
  chmod 600 "$CERT_DIR/privkey.pem"
fi

exec nginx -g 'daemon off;'
