#!/usr/bin/env sh
# Record the demo GIF (and theme screenshots) against a throwaway SSH
# environment, so no real key names, hosts, or fingerprints end up in the
# published images.
#
#   sh docs/demo-fixture.sh                 # runs vhs docs/demo.tape
#   sh docs/demo-fixture.sh docs/themes.tape
#
# Needs: vhs (with ttyd + ffmpeg), ssh-keygen, ssh-agent, ssh-add, and a
# sshush binary — pass one via SSHUSH_BIN, otherwise `go build` output is used.
set -eu

repo="$(cd "$(dirname "$0")/.." && pwd)"
tape="${1:-docs/demo.tape}"
# Fixed, friendly path — it appears in the recorded status line.
fixture="${TMPDIR:-/tmp}/sshush-demo"
rm -rf "$fixture" && mkdir -p "$fixture"
trap 'kill "${SSH_AGENT_PID:-0}" 2>/dev/null; rm -rf "$fixture"' EXIT

# Binary under demo: SSHUSH_BIN, or a fresh build.
if [ -z "${SSHUSH_BIN:-}" ]; then
  SSHUSH_BIN="$fixture/bin/sshush"
  (cd "$repo" && go build -o "$SSHUSH_BIN" ./cmd/sshush)
fi
mkdir -p "$fixture/bin"
[ -e "$fixture/bin/sshush" ] || ln -s "$SSHUSH_BIN" "$fixture/bin/sshush"

# Throwaway keys with friendly comments (shown in the Keys pane).
sshdir="$fixture/.ssh"
mkdir -p "$sshdir"
ssh-keygen -q -t ed25519 -N '' -C personal -f "$sshdir/id_ed25519"
ssh-keygen -q -t ed25519 -N '' -C work -f "$sshdir/id_work"
ssh-keygen -q -t ecdsa -N '' -C deploys -f "$sshdir/id_deploy"
ssh-keygen -q -t rsa -b 3072 -N '' -C legacy -f "$sshdir/id_legacy"

# Demo hosts (TEST-NET / RFC1918 addresses only).
cat > "$sshdir/config" <<'EOF'
Host github.com
  User git
  IdentityFile ~/.ssh/id_ed25519

Host prod-web-1
  HostName 203.0.113.10
  User deploy
  IdentityFile ~/.ssh/id_work

Host prod-db
  HostName 203.0.113.11
  User deploy
  ProxyJump prod-web-1

Host staging
  HostName 198.51.100.7
  User deploy
  Port 2222
  IdentityFile ~/.ssh/id_deploy

Host home-nas
  HostName 192.168.1.42
  User admin

Host *
  AddKeysToAgent yes
  ServerAliveInterval 60

Match host prod-*
  ForwardAgent yes
EOF
chmod 700 "$sshdir"; chmod 600 "$sshdir"/id_* "$sshdir/config"

# sshush settings: start on tokyonight (the tape's theme switch assumes it),
# no update check notice, id_ed25519 marked default (star + auto-load).
export XDG_CONFIG_HOME="$fixture/xdg"
mkdir -p "$XDG_CONFIG_HOME/sshush"
cat > "$XDG_CONFIG_HOME/sshush/config.toml" <<EOF
default_identities = ["id_ed25519"]
ssh_dir = "$sshdir"
theme = "tokyonight"
check_updates = false
EOF

# Fresh agent with two keys loaded, so the ●/○ badges vary.
eval "$(ssh-agent -s)" > /dev/null
ssh-add -q "$sshdir/id_ed25519" "$sshdir/id_work"

PATH="$fixture/bin:$PATH" SSHUSH_SSH_DIR="$sshdir" vhs "$repo/$tape"
