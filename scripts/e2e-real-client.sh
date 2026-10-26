#!/usr/bin/env bash
# End-to-end check with the real RustDesk client (Linux, headless) against
# real hbbs/hbbr and a locally built RuConnect. Needs the client's GTK
# runtime libraries; set LIBS to a directory holding them if they are not
# installed system-wide.
#
#   scripts/e2e-real-client.sh [workdir]
#
# Verifies: API address derived from the ID server port (port - 2), sysinfo
# and heartbeat from the real client, strategy delivery into the client's
# config, and the /s/<site> multi-server API URL.
set -euo pipefail

CLIENT_VER=${CLIENT_VER:-1.5.0}
SERVER_VER=${SERVER_VER:-1.1.16}
W=${1:-$(mktemp -d)}
REPO=$(cd "$(dirname "$0")/.." && pwd)
mkdir -p "$W"/{run,home/.config/rustdesk}
cd "$W"

[ -x server/amd64/hbbs ] || {
	curl -sSfL -o server.zip "https://github.com/rustdesk/rustdesk-server/releases/download/$SERVER_VER/rustdesk-server-linux-amd64.zip"
	python3 -c "import zipfile; zipfile.ZipFile('server.zip').extractall('server')"
	chmod +x server/amd64/*
}
[ -x client/usr/share/rustdesk/rustdesk ] || {
	curl -sSfL -o client.deb "https://github.com/rustdesk/rustdesk/releases/download/$CLIENT_VER/rustdesk-$CLIENT_VER-x86_64.deb"
	mkdir -p client && (cd client && ar x ../client.deb && tar xf data.tar.*)
}
(cd "$REPO" && go build -o "$W/run/ruconnect" ./cmd/ruconnect)

pids=()
cleanup() { kill "${pids[@]}" 2>/dev/null || true; }
trap cleanup EXIT

cd run
../server/amd64/hbbr -p 31117 -k _ >hbbr.log 2>&1 & pids+=($!)
../server/amd64/hbbs -p 31116 -r 127.0.0.1:31117 -k _ >hbbs.log 2>&1 & pids+=($!)
sleep 2
KEY=$(cat id_ed25519.pub)
rm -f rc.db*
RUCONNECT_LISTEN=127.0.0.1:31114 RUCONNECT_DB=$PWD/rc.db RUCONNECT_ADMIN_PASSWORD=adminpass123 \
	RUCONNECT_ID_SERVER=127.0.0.1:31116 RUCONNECT_KEY_FILE=$PWD/id_ed25519.pub RUCONNECT_LOG_LEVEL=debug \
	./ruconnect >rc.log 2>&1 & pids+=($!)
sleep 1

B=http://127.0.0.1:31114
TOK=$(curl -s $B/api/login -d '{"username":"admin","password":"adminpass123"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["access_token"])')
A="Authorization: Bearer $TOK"
curl -s -H "$A" $B/api/admin/strategies -d '{"name":"E2E","options":{"enable-file-transfer":"N"},"is_default":true}' >/dev/null

CFG=$W/home/.config/rustdesk/RustDesk2.toml
cat >"$CFG" <<TOML
[options]
custom-rendezvous-server = '127.0.0.1:31116'
relay-server = '127.0.0.1:31117'
key = '$KEY'
TOML
export LD_LIBRARY_PATH=${LIBS:+$LIBS:}$W/client/usr/share/rustdesk/lib
HOME=$W/home ../client/usr/share/rustdesk/rustdesk --server >client.log 2>&1 & pids+=($!)

fail() { echo "FAIL: $*"; exit 1; }
for i in $(seq 40); do grep -q "path=/api/sysinfo status=200" rc.log && break; sleep 1; done
grep -q "path=/api/sysinfo status=200" rc.log || fail "no sysinfo from the client (API address derivation)"
for i in $(seq 40); do grep -q "enable-file-transfer = 'N'" "$CFG" && break; sleep 1; done
grep -q "enable-file-transfer = 'N'" "$CFG" || fail "strategy not applied by the client"
ID=$(curl -s -H "$A" $B/api/admin/devices | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"][0]["id"])')
echo "ok: device $ID registered, heartbeat and strategy work"

curl -s -H "$A" $B/api/admin/servers -d "{\"name\":\"Branch\",\"slug\":\"branch\",\"id_server\":\"127.0.0.1:31116\",\"key\":\"$KEY\"}" >/dev/null
kill "${pids[-1]}"; sleep 2
sed -i "s#^key = .*#&\napi-server = '$B/s/branch'#" "$CFG"
HOME=$W/home ../client/usr/share/rustdesk/rustdesk --server >client2.log 2>&1 & pids+=($!)
for i in $(seq 40); do grep -q "path=/s/branch/api/sysinfo status=200" rc.log && break; sleep 1; done
SLUG=$(curl -s -H "$A" $B/api/admin/devices | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"][0]["server_slug"])')
[ "$SLUG" = branch ] || fail "device not moved to the branch site (got $SLUG)"
echo "ok: multi-server API URL works"
echo PASS
