#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

GO_BIN="${GO_BIN:-go}"

info() { printf "  -> %s\n" "$*"; }
ok() { printf "  [ok] %s\n" "$*"; }
fail() { printf "  [x] %s\n" "$*"; exit 1; }

if ! command -v "$GO_BIN" >/dev/null 2>&1; then
  fail "Go is required in PATH (or set GO_BIN) to run release checks."
fi

if ! command -v ruby >/dev/null 2>&1; then
  fail "Ruby is required for YAML validation."
fi

if ! command -v python3 >/dev/null 2>&1; then
  fail "Python 3 is required for the local provider smoke test."
fi

if ! git diff --quiet || ! git diff --cached --quiet || [ -n "$(git ls-files --others --exclude-standard)" ]; then
  fail "release-check requires a clean worktree. Commit or stash changes first."
fi

tmp_root="$(mktemp -d)"
install_dir="$tmp_root/bin"
home_dir="$tmp_root/home"
stub_log="$tmp_root/stub.log"
agent_log="$tmp_root/agent.log"
stub_pid=""

cleanup() {
  if [ -n "$stub_pid" ] && kill -0 "$stub_pid" >/dev/null 2>&1; then
    kill "$stub_pid" >/dev/null 2>&1 || true
    wait "$stub_pid" >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp_root"
}
trap cleanup EXIT

wait_for_stub() {
  local i
  for i in $(seq 1 50); do
    if python3 - <<'PY' >/dev/null 2>&1
import socket
s = socket.socket()
try:
    s.connect(("127.0.0.1", 18080))
    raise SystemExit(0)
except OSError:
    raise SystemExit(1)
finally:
    s.close()
PY
    then
      return 0
    fi
    sleep 0.1
  done
  return 1
}

info "Checking formatting..."
"$GO_BIN" fmt ./... >/dev/null
git diff --exit-code >/dev/null
ok "Formatting is clean"

info "Running go vet..."
"$GO_BIN" vet ./...
ok "go vet passed"

info "Running go test..."
"$GO_BIN" test ./...
ok "go test passed"

info "Building binary..."
make build >/dev/null
ok "Build passed"

info "Checking installer shell syntax..."
bash -n install.sh
ok "install.sh syntax is valid"

info "Validating workflow and GoReleaser YAML..."
ruby -e 'require "yaml"; [".github/workflows/build.yml", ".github/workflows/pr.yml", ".github/workflows/release.yml", ".github/workflows/docker-build.yml", ".goreleaser.yaml"].each { |p| YAML.load_file(p); puts "  yaml ok: #{p}" }'
ok "YAML files parsed successfully"

info "Running installer smoke test..."
INSTALL_DIR="$install_dir" bash ./install.sh --source >/dev/null
"$install_dir/levik" version >/dev/null
ok "Installer smoke test passed"

info "Starting local OpenAI-compatible stub..."
python3 - <<'PY' >"$stub_log" 2>&1 &
from http.server import BaseHTTPRequestHandler, HTTPServer
import json

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path != "/v1/chat/completions":
            self.send_response(404)
            self.end_headers()
            return
        length = int(self.headers.get("Content-Length", "0"))
        self.rfile.read(length)
        body = {
            "choices": [
                {
                    "message": {"content": "stub-ok"},
                    "finish_reason": "stop",
                }
            ],
            "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
        }
        data = json.dumps(body).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, format, *args):
        pass

HTTPServer(("127.0.0.1", 18080), Handler).serve_forever()
PY
stub_pid="$!"

if ! wait_for_stub; then
  fail "Local stub server did not start"
fi
ok "Local stub is reachable"

info "Running end-to-end local-provider smoke test..."
LEVIK_HOME="$home_dir" "$install_dir/levik" onboard --auto --provider vllm --api-base http://127.0.0.1:18080/v1 --model fake-model >/dev/null
LEVIK_HOME="$home_dir" "$install_dir/levik" doctor >/dev/null
LEVIK_HOME="$home_dir" "$install_dir/levik" agent -m "ping" >"$agent_log" 2>&1
grep -q "stub-ok" "$agent_log"
ok "Local-provider smoke test passed"

printf "\nRelease preflight passed.\n"
