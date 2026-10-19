#!/usr/bin/env bash
# First-time setup, meant to run ON the Raspberry Pi as the normal user that will run the service
# (NOT with sudo; it calls sudo itself for the two files under /etc):
#   deploy/install.sh --listen 100.x.y.z:8080 [--root ~/src]
# - creates /etc/code-asker.env (random token) unless it already exists
# - installs the systemd unit for THIS user/home/checkout and enables it
# Use --dry-run to print the unit file without touching anything.
set -euo pipefail

APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${ENV_FILE:-/etc/code-asker.env}"
UNIT_DEST="${UNIT_DEST:-/etc/systemd/system/code-asker.service}"
LISTEN="127.0.0.1:8080"
ROOT="$HOME/src"
DRY=0

usage() { sed -n '2,8p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit "${1:-0}"; }
while [ $# -gt 0 ]; do
  case "$1" in
    --listen) LISTEN="${2:?--listen needs HOST:PORT}"; shift 2 ;;
    --root) ROOT="${2:?--root needs a directory}"; shift 2 ;;
    --dry-run) DRY=1; shift ;;
    -h|--help) usage 0 ;;
    *) echo "unknown option: $1" >&2; usage 1 ;;
  esac
done

if [ "$(id -u)" -eq 0 ] && [ "$DRY" -eq 0 ]; then
  echo "Run this as the normal user that will run the service, not as root (it uses sudo where needed)." >&2
  exit 1
fi
RUN_USER="$(id -un)"
RUN_HOME="$HOME"

# PATH for the service: directories of the tools it needs (claude, node) plus the usual places.
SVC_PATH=""
add_path() { case ":$SVC_PATH:" in *":$1:"*) ;; *) SVC_PATH="${SVC_PATH:+$SVC_PATH:}$1" ;; esac; }
for tool in claude node; do
  if p="$(command -v "$tool" 2>/dev/null)"; then add_path "$(dirname "$p")"; fi
done
add_path "$RUN_HOME/.local/bin"; add_path "$RUN_HOME/.npm-global/bin"
add_path /usr/local/bin; add_path /usr/bin; add_path /bin

render_unit() {
  sed -e "s#@USER@#$RUN_USER#g" -e "s#@HOME@#$RUN_HOME#g" -e "s#@APP_DIR@#$APP_DIR#g" -e "s#@PATH@#$SVC_PATH#g" \
    "$APP_DIR/deploy/code-asker.service"
}

if [ "$DRY" -eq 1 ]; then
  render_unit
  exit 0
fi

ROOT_ABS="$(cd "$ROOT" 2>/dev/null && pwd)" || { echo "root directory does not exist: $ROOT (create it and git clone your repositories there, or pass --root)" >&2; exit 1; }
ROOT="$ROOT_ABS"
command -v claude >/dev/null 2>&1 || echo "note: 'claude' not found in PATH; AI chat will not work until it is installed and logged in (see README)." >&2

if sudo test -e "$ENV_FILE"; then
  echo "keeping existing $ENV_FILE (edit it yourself to change root/listen/token)"
else
  TOKEN="$(head -c 24 /dev/urandom | base64 | tr -d '/+=')"
  printf 'CODE_ASKER_ROOT=%s\nCODE_ASKER_TOKEN=%s\nCODE_ASKER_LISTEN=%s\n' "$ROOT" "$TOKEN" "$LISTEN" | sudo tee "$ENV_FILE" >/dev/null
  sudo chmod 600 "$ENV_FILE"
  echo "created $ENV_FILE (root=$ROOT listen=$LISTEN)"
  echo "login token: $TOKEN   (also stored in $ENV_FILE)"
fi

render_unit | sudo tee "$UNIT_DEST" >/dev/null
sudo systemctl daemon-reload
sudo systemctl enable code-asker
echo "installed $UNIT_DEST for user '$RUN_USER' (app dir: $APP_DIR)"
echo "next: $APP_DIR/deploy/deploy.sh"
