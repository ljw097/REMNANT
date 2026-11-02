#!/usr/bin/env bash
# End-to-end test of self-update with real binaries. It DROPS the "remnant"
# schema in the target database:
#
#   REMNANT_SMOKE_DATABASE_URL=postgres://user@localhost/scratch scripts/update-e2e.sh
#
# Binaries are built with a throwaway signing key, a local stand-in for the
# GitHub releases API serves the release, and the script checks: update via
# the API with an in-place restart, confirmation of the new version,
# rollback after repeated failed starts, and refusal of tampered or
# foreign-signed releases.
set -u
: "${REMNANT_SMOKE_DATABASE_URL:?set REMNANT_SMOKE_DATABASE_URL to a disposable database}"
cd "$(dirname "$0")/.."
T=$(mktemp -d)
PIDS=()
trap 'kill ${PIDS[@]} 2>/dev/null; rm -rf $T' EXIT

pass=0; fail=0
check() { # name expected actual
  if [ "$2" = "$3" ]; then pass=$((pass+1)); echo "ok   $1"; else fail=$((fail+1)); echo "FAIL $1: want $2 got $3"; fi
}
PLATFORM=$(go env GOOS)/$(go env GOARCH)

go build -o $T/remnant-release ./cmd/remnant-release || exit 1
PUB=$($T/remnant-release keygen -out $T/test.key)
export EXTRA_LDFLAGS="-X github.com/ljw097/remnant/internal/update.trustedKeys=$PUB"

# v0.0.1 is what gets installed; v0.0.2 is published.
DIST=$T/v1 scripts/release-build.sh v0.0.1 $PLATFORM >/dev/null || exit 1
DIST=$T/v2 scripts/release-build.sh v0.0.2 $PLATFORM >/dev/null || exit 1
$T/remnant-release sign -key $T/test.key $T/v2/SHA256SUMS
mkdir -p $T/install
tar xzf $T/v1/*.tar.gz -C $T/install --strip-components=1
INSTALL=$T/install/remnantd

mkdir -p $T/pub
cp $T/v2/* $T/pub/
cat > $T/fake_github.py <<'PY'
import json, os, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
root, port = sys.argv[1], int(sys.argv[2])
class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        if self.path == "/repos/test/remnant/releases/latest":
            assets = [{"name": n, "size": os.path.getsize(os.path.join(root, n)),
                       "browser_download_url": f"http://127.0.0.1:{port}/dl/{n}"} for n in sorted(os.listdir(root))]
            body = json.dumps({"tag_name": "v0.0.2", "html_url": "http://example/rel", "prerelease": False, "assets": assets}).encode()
        elif self.path.startswith("/dl/") and os.path.isfile(os.path.join(root, os.path.basename(self.path))):
            body = open(os.path.join(root, os.path.basename(self.path)), "rb").read()
        else:
            self.send_response(404); self.end_headers(); return
        self.send_response(200); self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
ThreadingHTTPServer(("127.0.0.1", port), H).serve_forever()
PY
python3 -I $T/fake_github.py $T/pub 58091 & PIDS+=($!)

export REMNANT_DATABASE_URL=$REMNANT_SMOKE_DATABASE_URL
export REMNANT_MASTER_KEY=$(head -c32 /dev/urandom | base64)
export REMNANT_DATA_DIR=$T/data REMNANT_LISTEN=127.0.0.1:58090
export REMNANT_AUTO_UPDATE=check REMNANT_UPDATE_API=http://127.0.0.1:58091 REMNANT_UPDATE_REPO=test/remnant
psql "$REMNANT_DATABASE_URL" -qc 'DROP SCHEMA IF EXISTS remnant CASCADE' 2>/dev/null
ADMIN=$($INSTALL create-user -username admin -role admin 2>/dev/null)
B=http://127.0.0.1:58090/api/v1
version() { curl -s -H "Authorization: Bearer $ADMIN" $B/system/version | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['version'], d['update'].get('$1',''))"; }
wait_version() { # want seconds
  for _ in $(seq $(( $2 * 5 ))); do
    v=$(version status 2>/dev/null | cut -d' ' -f1); [ "$v" = "$1" ] && { echo "$v"; return; }; sleep 0.2
  done
  echo "${v:-down}"
}
start() { $INSTALL serve 2>>$T/serve.log & SERVER=$!; PIDS+=($SERVER); }

check "installed version" "v0.0.1 dev" "$($INSTALL version | cut -d' ' -f1) $(go run ./cmd/remnantd version | cut -d' ' -f1)"
start
check "serving v0.0.1" v0.0.1 "$(wait_version v0.0.1 10)"
check "update" 202 "$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Authorization: Bearer $ADMIN" $B/system/update)"
check "serving v0.0.2" v0.0.2 "$(wait_version v0.0.2 30)"
check "same process" "$INSTALL" "$(readlink /proc/$SERVER/exe)"
check "worker updated" v0.0.2 "$($T/install/remnant-worker version | cut -d' ' -f1)"
check "previous kept" v0.0.1 "$($T/install/remnantd.old version | cut -d' ' -f1)"
check "pending confirmation" 1 "$(python3 -c "import json; print(json.load(open('$T/install/.remnantd-update.json'))['attempts'])")"
check "up to date" 409 "$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Authorization: Bearer $ADMIN" $B/system/update)"
for _ in $(seq 80); do [ -e $T/install/.remnantd-update.json ] || break; sleep 1; done
check "confirmed after a healthy minute" absent "$([ -e $T/install/.remnantd-update.json ] && echo present || echo absent)"

# Simulate a version that keeps failing to start: the next start rolls back.
kill -TERM $SERVER; wait $SERVER 2>/dev/null
echo '{"from":"v0.0.1","to":"v0.0.2","attempts":2,"installed_at":"2026-01-01T00:00:00Z"}' > $T/install/.remnantd-update.json
start
check "rolled back" v0.0.1 "$(wait_version v0.0.1 15)"
check "worker rolled back" v0.0.1 "$($T/install/remnant-worker version | cut -d' ' -f1)"
check "marker cleared" absent "$([ -e $T/install/.remnantd-update.json ] && echo present || echo absent)"
check "rollback logged" 1 "$(grep -c 'rolling back' $T/serve.log)"

# A tampered archive is refused and nothing changes.
ARCHIVE=$(cd $T/pub && ls *.tar.gz)
printf 'tampered' >> $T/pub/$ARCHIVE
check "tampered update accepted for checking" 202 "$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Authorization: Bearer $ADMIN" $B/system/update)"
sleep 2
check "tampered archive refused" "v0.0.1 does not match its signed checksum" \
  "$(version last_error | sed -E 's/^(v[0-9.]+) .*(does not match its signed checksum).*/\1 \2/')"
cp $T/v2/$ARCHIVE $T/pub/$ARCHIVE

# A release signed with a key the binary does not trust is refused.
$T/remnant-release keygen -out $T/other.key >/dev/null
$T/remnant-release sign -key $T/other.key $T/pub/SHA256SUMS
check "foreign signature refused" 502 "$(curl -s -o $T/body -w '%{http_code}' -X POST -H "Authorization: Bearer $ADMIN" $B/system/update)"
check "still v0.0.1" v0.0.1 "$(version status | cut -d' ' -f1)"

echo "---- $pass passed, $fail failed"
[ $fail -eq 0 ] || tail -20 $T/serve.log
[ $fail -eq 0 ]
