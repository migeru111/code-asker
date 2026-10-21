#!/usr/bin/env bash
# One-command update, meant to run ON the Raspberry Pi:
#   git pull -> build -> swap binary -> restart -> health check (rollback on failure).
# The old binary is kept and the service is NOT restarted if the build fails.
set -euo pipefail

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVICE="${SERVICE:-code-asker}"
ENV_FILE="${ENV_FILE:-/etc/code-asker.env}"
BIN="$APP_DIR/bin/code-asker"
NEW="$BIN.new"
OLD="$BIN.old"

SUDO=""
if [ "$(id -u)" -ne 0 ]; then SUDO="sudo"; fi

# Derive the health-check URL from CODE_ASKER_LISTEN (default 127.0.0.1:8421).
# The env file is root-only (chmod 600), so fall back to sudo when we cannot read it ourselves;
# guessing 127.0.0.1 would fail whenever the service listens on a specific LAN/Tailscale IP.
LISTEN="${CODE_ASKER_LISTEN:-}"
if [ -z "$LISTEN" ] && [ -e "$ENV_FILE" ]; then
  if [ -r "$ENV_FILE" ]; then ENV_TEXT="$(cat "$ENV_FILE")"; else ENV_TEXT="$($SUDO cat "$ENV_FILE" 2>/dev/null || true)"; fi
  LISTEN="$(printf '%s\n' "$ENV_TEXT" | sed -n 's/^CODE_ASKER_LISTEN=//p' | tail -n1 | tr -d '"'"'"' ')"
  if [ -z "$LISTEN" ]; then echo "note: could not read CODE_ASKER_LISTEN from $ENV_FILE; checking 127.0.0.1:8421 (set HEALTH_URL to override)" >&2; fi
fi
LISTEN="${LISTEN:-127.0.0.1:8421}"
HOST="${LISTEN%:*}"; PORT="${LISTEN##*:}"
case "$HOST" in ""|"0.0.0.0"|"[::]"|"::") HOST="127.0.0.1" ;; esac
HEALTH_URL="${HEALTH_URL:-http://$HOST:$PORT/healthz}"

cd "$APP_DIR"
echo "==> git pull"
git pull --ff-only

VERSION="$(git describe --always --dirty --tags 2>/dev/null || git rev-parse --short HEAD)"
echo "==> build $VERSION"
mkdir -p bin
if ! go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$NEW" ./cmd/code-asker; then
  rm -f "$NEW"
  echo "!! build failed: keeping the current binary, service not restarted" >&2
  exit 1
fi

[ -f "$BIN" ] && cp -p "$BIN" "$OLD"
mv -f "$NEW" "$BIN"

echo "==> restart $SERVICE"
$SUDO systemctl restart "$SERVICE"

echo "==> health check $HEALTH_URL"
for _ in $(seq 1 15); do
  if curl -fsS --max-time 2 "$HEALTH_URL" >/dev/null 2>&1; then
    echo "OK: $VERSION is running"
    exit 0
  fi
  sleep 1
done

echo "!! health check failed" >&2
if [ -f "$OLD" ]; then
  echo "!! rolling back to the previous binary" >&2
  mv -f "$OLD" "$BIN"
  $SUDO systemctl restart "$SERVICE" || true
fi
$SUDO journalctl -u "$SERVICE" -n 20 --no-pager >&2 || true
exit 1
