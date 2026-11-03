#!/usr/bin/env bash
# Runs remnantd with the web UI and SYNTHETIC demo data, for trying the UI
# locally. It DROPS the "remnant" schema in the database it is given:
#
#   REMNANT_DEMO_DATABASE_URL=postgres://user@localhost/remnant_demo scripts/dev-demo.sh
#
# The model API is scripts/fake_anthropic.py (no network, no real key). When
# bubblewrap and a user systemd scope are available, the sandboxed worker runs
# real analyzers and an ASan lab harness on the demo snapshot. Everything the
# demo creates is labelled synthetic. REMNANT_DEMO_MODE=personal sets the
# instance up for one person (self-approval) instead of a team. Stop it with
# Ctrl-C.
set -u
: "${REMNANT_DEMO_DATABASE_URL:?set REMNANT_DEMO_DATABASE_URL to a disposable database}"
cd "$(dirname "$0")/.."
D=$(mktemp -d)
PORT=${REMNANT_DEMO_PORT:-8090}
PIDS=()
trap 'kill ${PIDS[@]} 2>/dev/null; rm -rf $D' EXIT

if [ ! -f web/dist/index.html ]; then
  (cd web && npm ci --no-audit --no-fund && npm run build) || exit 1
fi
go build -o $D/remnantd ./cmd/remnantd && go build -o $D/remnant-worker ./cmd/remnant-worker || exit 1

export REMNANT_DATABASE_URL=$REMNANT_DEMO_DATABASE_URL
export REMNANT_MASTER_KEY=$(head -c32 /dev/urandom | base64)
export REMNANT_DATA_DIR=$D/data REMNANT_LISTEN=127.0.0.1:$PORT REMNANT_AUTO_UPDATE=off
if command -v bwrap >/dev/null && systemd-run --user --scope -q true 2>/dev/null; then export REMNANT_WORKER=bwrap; fi
psql "$REMNANT_DATABASE_URL" -qc 'DROP SCHEMA IF EXISTS remnant CASCADE' 2>/dev/null

S=$D/data/snapshots/libdemo-1.2
mkdir -p $S/src $S/test $S/.remnant/lab
cat > $S/src/parser.c <<'C'
/* Synthetic demo code for REMNANT. Not a real project. */
#include <string.h>
#include <stdlib.h>

struct req { int state; char name[8]; char *body; };

enum { STATE_NEW, STATE_PUBLISHED };

static int init_body(struct req *r)
{
	r->body = malloc(64);
	return r->body ? 0 : -1;
}

int parse_header(struct req *r, const char *buf, size_t len)
{
	r->state = STATE_PUBLISHED;
	if (init_body(r) != 0)
		return -1;
	memcpy(r->name, buf, len);
	return 0;
}

void copy_name(char *dst, const char *src, size_t len)
{
	memcpy(dst, src, len);
}
C
cat > $S/test/main.c <<'C'
#include <stddef.h>
struct req { int state; char name[8]; char *body; };
int parse_header(struct req *r, const char *buf, size_t len);
int main(void) { struct req r = {0}; return parse_header(&r, "0123456789abcdef", 16); }
C
echo 'cc -fsanitize=address -g -O0 -o "$TMPDIR/t" src/parser.c test/main.c && "$TMPDIR/t"' > $S/.remnant/lab/asan_test

python3 -I scripts/fake_anthropic.py 58191 & PIDS+=($!)
$D/remnantd migrate 2>/dev/null || exit 1
if [ "${REMNANT_DEMO_MODE:-team}" = personal ]; then
  ADMIN=$($D/remnantd setup -mode personal -username solo -name "REMNANT demo (synthetic)" 2>/dev/null)
  RITA=$ADMIN VIC=$ADMIN
else
  ADMIN=$($D/remnantd setup -mode team -username admin -name "REMNANT demo (synthetic)" 2>/dev/null)
  RITA=$($D/remnantd create-user -username rita -role researcher 2>/dev/null)
  VIC=$($D/remnantd create-user -username vic -role reviewer 2>/dev/null)
fi
$D/remnantd serve 2>$D/serve.log & PIDS+=($!)
for _ in $(seq 50); do curl -sf localhost:$PORT/healthz >/dev/null && break; sleep 0.2; done

B=http://127.0.0.1:$PORT/api/v1
api() { curl -sf -X "$2" -H "Authorization: Bearer $1" -H 'Content-Type: application/json' ${4:+-d "$4"} "$B$3"; }
jid() { python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])'; }

LLM=$(api $ADMIN POST /llm-profiles '{"name":"Demo (fake API)","provider":"anthropic","model":"claude-opus-5-5","base_url":"http://127.0.0.1:58191","api_key":"sk-ant-demo-000000000000"}' | jid)
PROJ=$(api $RITA POST /projects "{\"name\":\"libdemo (synthetic)\",\"description\":\"Synthetic demo project created by scripts/dev-demo.sh.\",\"llm_profile_id\":$LLM,\"policy\":{\"source_mode\":\"read_only\",\"dynamic_mode\":\"local_lab_only\",\"network\":\"none\"}}" | jid)
TGT=$(api $RITA POST /projects/$PROJ/targets '{"kind":"local_snapshot","display_name":"libdemo 1.2 (synthetic)","snapshot_key":"libdemo-1.2","primary_language":"c"}' | jid)
PAT=$(api $RITA POST /patterns '{"source_kind":"historical","title":"Copy length not bounded by destination","vulnerability_class":"memory-safety","cwe_id":"CWE-787","language":"c","source_ref":"synthetic demo pattern","root_cause":"A caller-controlled length is used for a copy into a fixed-size buffer without comparing it to the buffer size.","preconditions":["the length comes from input","the destination is fixed-size"],"code_signatures":["memcpy"],"counter_signatures":["sizeof"]}' | jid)
api $RITA POST /patterns '{"title":"State published before fallible init","vulnerability_class":"resource-lifecycle","language":"c","root_cause":"Shared state becomes visible before an initialization step that can fail, and the failure path does not roll it back.","code_signatures":["STATE_PUBLISHED","init_body"]}' >/dev/null
[ "$VIC" != "$RITA" ] && api $VIC POST /patterns/$PAT/review '{"status":"reviewed"}' >/dev/null
RUN=$(api $RITA POST /runs "{\"project_id\":$PROJ,\"target_ids\":[$TGT],\"pattern_ids\":[$PAT]}" | jid)

echo "waiting for run $RUN to reach review…"
for _ in $(seq 150); do
  st=$(api $RITA GET /runs/$RUN | python3 -c 'import json,sys; print(json.load(sys.stdin)["status"])')
  [ "$st" = review_required ] && break; sleep 0.2
done
HYP=$(api $RITA GET /runs/$RUN/hypotheses | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["id"])')
api $VIC PATCH /hypotheses/$HYP '{"decision":"accepted","reviewer_note":"Plausible: len is never compared to sizeof(r->name)."}' >/dev/null
api $RITA POST /hypotheses/$HYP/lab-approval '{"operation":"lab.asan_test","summary":"Run the ASan harness against parse_header with a 16-byte name."}' >/dev/null
api $RITA POST /runs "{\"project_id\":$PROJ,\"target_ids\":[$TGT],\"pattern_ids\":[$PAT]}" >/dev/null

cat <<EOF

REMNANT demo is running at http://127.0.0.1:$PORT  (synthetic data; mode: ${REMNANT_DEMO_MODE:-team}; worker: ${REMNANT_WORKER:-off})
Sign in with one of these tokens:
  admin       $ADMIN
  researcher  $RITA
  reviewer    $VIC
A hypothesis waits for lab approval: sign in as the reviewer and open Lab approvals.
Press Ctrl-C to stop.
EOF
wait
