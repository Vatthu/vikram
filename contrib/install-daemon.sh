#!/usr/bin/env bash
# Install Vikram as a host-native macOS launchd service.
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: contrib/install-daemon.sh [--no-load]

Environment:
  INSTALL_DIR          Binary install directory. Default: $HOME/.local/bin
  VIKRAM_HOME           Runtime home. Default: $HOME/.vikram
  VIKRAM_CONSOLE_ADDR   Console bind address. Default: 127.0.0.1:8787
  VIKRAM_DASHBOARD_ADDR Dashboard bind address. Default: 127.0.0.1:8788
  VIKRAM_CONSOLE_API_KEY Optional console API key. Generated if absent.

Options:
  --no-load            Install files but do not bootstrap launchd.
USAGE
}

NO_LOAD=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-load)
      NO_LOAD=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "This installer targets macOS launchd. Set up the service manually on $(uname -s)." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
VIKRAM_HOME="${VIKRAM_HOME:-$HOME/.vikram}"
VIKRAM_CONSOLE_ADDR="${VIKRAM_CONSOLE_ADDR:-127.0.0.1:8787}"
VIKRAM_DASHBOARD_ADDR="${VIKRAM_DASHBOARD_ADDR:-127.0.0.1:8788}"

RUN_DIR="$VIKRAM_HOME/run"
LOG_DIR="$VIKRAM_HOME/logs"
SECRETS_DIR="$VIKRAM_HOME/secrets"
WRAPPER_DIR="$VIKRAM_HOME/bin"
CONSOLE_KEY_FILE="$SECRETS_DIR/console-api-key"
HOST_SOCKET="$RUN_DIR/vikramd.sock"
ORCHESTRATOR_SOCKET="$RUN_DIR/vikram-orchestrator.sock"
PLIST_NAME="com.vikram.team.plist"
PLIST_PATH="$HOME/Library/LaunchAgents/$PLIST_NAME"
WRAPPER_PATH="$WRAPPER_DIR/vikram-gateway-wrapper.sh"

generate_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
    return
  fi
  uuidgen | tr '[:upper:]' '[:lower:]' | tr -d '-'
}

escape_sed_replacement() {
  printf '%s' "$1" | sed 's/[\\&|]/\\&/g'
}

shell_quote() {
  printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

echo "=== Vikram Daemon Install ==="
echo "Project:    $PROJECT_ROOT"
echo "Install:    $INSTALL_DIR/vikram"
echo "Vikram home: $VIKRAM_HOME"
echo "Console:    http://$VIKRAM_CONSOLE_ADDR"

umask 077
install -d -m 755 "$INSTALL_DIR"
install -d -m 700 "$VIKRAM_HOME" "$RUN_DIR" "$LOG_DIR" "$SECRETS_DIR" "$WRAPPER_DIR"
install -d -m 700 \
  "$VIKRAM_HOME/db" \
  "$VIKRAM_HOME/tasks" \
  "$VIKRAM_HOME/artifacts" \
  "$VIKRAM_HOME/workspaces" \
  "$VIKRAM_HOME/workspace"

if [[ -n "${VIKRAM_CONSOLE_API_KEY:-}" ]]; then
  printf '%s\n' "$VIKRAM_CONSOLE_API_KEY" > "$CONSOLE_KEY_FILE"
elif [[ ! -s "$CONSOLE_KEY_FILE" ]]; then
  generate_secret > "$CONSOLE_KEY_FILE"
fi
chmod 600 "$CONSOLE_KEY_FILE"

echo ""
echo "Building vikram..."
make -C "$PROJECT_ROOT" build
install -m 755 "$PROJECT_ROOT/build/vikram" "$INSTALL_DIR/vikram"
echo "Installed binary to $INSTALL_DIR/vikram"

cat > "$WRAPPER_PATH" <<EOF
#!/usr/bin/env bash
set -euo pipefail
export VIKRAM_HOME=$(shell_quote "$VIKRAM_HOME")
export VIKRAM_HOST_SOCKET=$(shell_quote "$HOST_SOCKET")
export VIKRAM_ORCHESTRATOR_SOCKET=$(shell_quote "$ORCHESTRATOR_SOCKET")
export VIKRAM_CONSOLE_ENABLED="1"
export VIKRAM_CONSOLE_ADDR=$(shell_quote "$VIKRAM_CONSOLE_ADDR")
export VIKRAM_DASHBOARD_ADDR=$(shell_quote "$VIKRAM_DASHBOARD_ADDR")
if [[ -r "\$VIKRAM_HOME/secrets/console-api-key" ]]; then
  export VIKRAM_CONSOLE_API_KEY="\$(cat "\$VIKRAM_HOME/secrets/console-api-key")"
fi
exec $(shell_quote "$INSTALL_DIR/vikram") gateway
EOF
chmod 700 "$WRAPPER_PATH"
echo "Installed wrapper to $WRAPPER_PATH"

install -d -m 755 "$HOME/Library/LaunchAgents"
TMP_PLIST="$(mktemp)"
sed \
  -e "s|__VIKRAM_WRAPPER__|$(escape_sed_replacement "$WRAPPER_PATH")|g" \
  -e "s|__VIKRAM_HOME__|$(escape_sed_replacement "$VIKRAM_HOME")|g" \
  "$PROJECT_ROOT/contrib/$PLIST_NAME" > "$TMP_PLIST"
install -m 600 "$TMP_PLIST" "$PLIST_PATH"
rm -f "$TMP_PLIST"
echo "Installed LaunchAgent to $PLIST_PATH"

if [[ "$NO_LOAD" == "1" ]]; then
  echo "Skipped launchd bootstrap because --no-load was provided."
else
  launchctl bootout "gui/$(id -u)" "$PLIST_PATH" >/dev/null 2>&1 || true
  launchctl bootstrap "gui/$(id -u)" "$PLIST_PATH"
  launchctl kickstart -k "gui/$(id -u)/com.vikram.team"
  echo "LaunchAgent bootstrapped."
fi

echo ""
echo "=== Vikram daemon install complete ==="
echo "Logs:        $LOG_DIR/gateway.log"
echo "Errors:      $LOG_DIR/gateway.err"
echo "Console key: $CONSOLE_KEY_FILE"
echo "Status:      launchctl print gui/$(id -u)/com.vikram.team"
echo "Stop:        launchctl bootout gui/$(id -u) $PLIST_PATH"
