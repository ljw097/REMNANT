#!/usr/bin/env bash
# End-to-end smoke test of remnantd. It DROPS the "remnant" schema in the
# target database, so point it only at a disposable PostgreSQL:
#
#   REMNANT_SMOKE_DATABASE_URL=postgres://user@localhost/remnant_smoke scripts/smoke.sh
#
# The in-process orchestrator runs against scripts/fake_anthropic.py, a local
# stand-in for the model API, so no network access or real key is needed.
# When bwrap and a user systemd scope are available, the worker runtime is
# enabled and the lab harness really runs (ASan); otherwise the script checks
# the no-worker path.
set -u
: "${REMNANT_SMOKE_DATABASE_URL:?set REMNANT_SMOKE_DATABASE_URL to a disposable database}"
cd "$(dirname "$0")/.."
SP=$(mktemp -d)
go build -o $SP/remnantd ./cmd/remnantd || exit 1
go build -o $SP/remnant-worker ./cmd/remnant-worker || exit 1
WORKER=off   # SMOKE_WORKER=off forces the no-worker path
if [ "${SMOKE_WORKER:-auto}" != off ] && command -v bwrap >/dev/null && command -v cc >/dev/null &&
   systemd-run --user --scope -q true 2>/dev/null; then
  WORKER=bwrap
fi
export REMNANT_WORKER=$WORKER
echo "worker runtime: $WORKER"

export REMNANT_DATABASE_URL=$REMNANT_SMOKE_DATABASE_URL
export REMNANT_MASTER_KEY=$(head -c32 /dev/urandom | base64)
export REMNANT_DATA_DIR=$SP/data
export REMNANT_LISTEN=127.0.0.1:58080
PSQL=psql_; psql_() { psql "$REMNANT_SMOKE_DATABASE_URL" -tAq "$@"; }
$PSQL -c 'DROP SCHEMA IF EXISTS remnant CASCADE' 2>/dev/null
mkdir -p $SP/data/snapshots/demo/src
mkdir -p $SP/data/snapshots/demo/test $SP/data/snapshots/demo/.remnant/lab
cat > $SP/data/snapshots/demo/src/parser.c <<'C'
#include <string.h>
struct req { char name[8]; };
int parse(struct req *r, const char *buf, size_t len)
{
	memcpy(r->name, buf, len);
	return 0;
}
C
cat > $SP/data/snapshots/demo/test/main.c <<'C'
#include <stddef.h>
struct req { char name[8]; };
int parse(struct req *r, const char *buf, size_t len);
int main(void) { struct req r; return parse(&r, "0123456789abcdef", 16); }
C
cat > $SP/data/snapshots/demo/.remnant/lab/asan_test <<'SH'
cc -fsanitize=address -g -O0 -o "$TMPDIR/t" src/parser.c test/main.c && "$TMPDIR/t"
SH

pass=0; fail=0
check() { # name expected actual
  if [ "$2" = "$3" ]; then pass=$((pass+1)); echo "ok   $1"; else fail=$((fail+1)); echo "FAIL $1: want $2 got $3"; fi
}

$SP/remnantd migrate 2>/dev/null; check "migrate" 0 $?
$SP/remnantd migrate 2>/dev/null; check "migrate idempotent" 0 $?
check "schema_migrations" "$(ls internal/store/migrations/*.sql | wc -l)" "$($PSQL -c 'select count(*) from remnant.schema_migrations')"

ADMIN=$($SP/remnantd create-user -username admin -role admin 2>/dev/null)
RES=$($SP/remnantd create-user -username rita -role researcher 2>/dev/null)
REV=$($SP/remnantd create-user -username vic -role reviewer 2>/dev/null)
check "token prefix" rmt_ "${ADMIN:0:4}"
check "token not stored" 0 "$($PSQL -c "select count(*) from remnant.api_tokens where token_sha256 = '$ADMIN'")"

python3 -I scripts/fake_anthropic.py 58081 & FAKE=$!
$SP/remnantd serve 2>$SP/serve.log & PID=$!
trap 'kill $PID $FAKE 2>/dev/null; rm -rf $SP' EXIT
for _ in $(seq 50); do curl -sf localhost:58080/healthz >/dev/null && break; sleep 0.1; done

B=http://127.0.0.1:58080/api/v1
req() { # token method path [body] -> prints "STATUS BODY"
  local t=$1 m=$2 p=$3; shift 3
  curl -s -o $SP/body -w '%{http_code}' -X "$m" -H "Authorization: Bearer $t" -H 'Content-Type: application/json' ${1:+-d "$1"} "$B$p"
}
j() { python3 -c "import json,sys; print(json.load(open('$SP/body'))$1)"; }

check "healthz" 200 "$(curl -s -o /dev/null -w '%{http_code}' localhost:58080/healthz)"
check "no token" 401 "$(curl -s -o /dev/null -w '%{http_code}' $B/me)"
check "bad token" 401 "$(req rmt_nope GET /me)"
check "me" 200 "$(req $RES GET /me)"; check "me role" researcher "$(j "['role']")"

check "llm profile by researcher" 403 "$(req $RES POST /llm-profiles '{"name":"x","provider":"anthropic","model":"claude-opus-5-5","api_key":"sk-ant-test-0123456789"}')"
check "llm profile by admin" 201 "$(req $ADMIN POST /llm-profiles '{"name":"default","provider":"anthropic","model":"claude-opus-5-5","base_url":"http://127.0.0.1:58081","api_key":"sk-ant-test-0123456789"}')"
check "llm key hint" "…6789" "$(j "['api_key_hint']")"
check "llm no key in response" 0 "$(grep -c sk-ant $SP/body)"
check "llm key sealed at rest" 0 "$($PSQL -c "select count(*) from remnant.llm_profiles where position('sk-ant' in encode(api_key_sealed,'escape')) > 0")"
LLM=$(j "['id']")

check "project bad policy" 400 "$(req $RES POST /projects '{"name":"p","policy":{"source_mode":"read_only","dynamic_mode":"local_lab_only","network":"bridge"}}')"
check "project create" 201 "$(req $RES POST /projects "{\"name\":\"demo\",\"llm_profile_id\":$LLM,\"policy\":{\"source_mode\":\"read_only\",\"dynamic_mode\":\"local_lab_only\",\"network\":\"none\"}}")"
PROJ=$(j "['id']")
check "project unknown field" 400 "$(req $RES POST /projects '{"name":"p","evil":1}')"

check "git target not allowlisted" 403 "$(req $RES POST /projects/$PROJ/targets '{"kind":"git_repository","display_name":"r","source_uri":"https://github.com/a/b","snapshot_key":"r1"}')"
check "snapshot path traversal" 400 "$(req $RES POST /projects/$PROJ/targets '{"kind":"local_snapshot","display_name":"x","snapshot_key":"../../etc"}')"
check "missing snapshot" 422 "$(req $RES POST /projects/$PROJ/targets '{"kind":"local_snapshot","display_name":"x","snapshot_key":"nope"}')"
check "local snapshot" 201 "$(req $RES POST /projects/$PROJ/targets '{"kind":"local_snapshot","display_name":"demo","snapshot_key":"demo","primary_language":"c"}')"
TGT=$(j "['id']"); check "target ready" ready "$(j "['status']")"; check "target revision" sha256: "$(j "['revision'][:7]")"
check "duplicate target" 409 "$(req $RES POST /projects/$PROJ/targets '{"kind":"local_snapshot","display_name":"demo","snapshot_key":"demo"}')"

check "pattern create" 201 "$(req $RES POST /patterns '{"title":"Cleanup skipped on error path","vulnerability_class":"resource-lifecycle","root_cause":"length used for copy without bound","language":"c","code_signatures":["memcpy"]}')"
PAT=$(j "['id']")
check "pattern review by researcher" 403 "$(req $RES POST /patterns/$PAT/review '{"status":"reviewed"}')"
check "pattern validate w/o finding" 409 "$(req $REV POST /patterns/$PAT/review '{"status":"validated"}')"
check "pattern review" 200 "$(req $REV POST /patterns/$PAT/review '{"status":"reviewed"}')"
check "pattern search" 200 "$(req $RES GET '/patterns?q=cleanup')"; check "pattern found" 1 "$(j ".__len__()")"

check "run bad limits" 400 "$(req $RES POST /runs "{\"project_id\":$PROJ,\"target_ids\":[$TGT],\"limits\":{\"max_workers\":9}}")"
check "run foreign target" 422 "$(req $RES POST /runs "{\"project_id\":$PROJ,\"target_ids\":[999]}")"
check "run create" 202 "$(req $RES POST /runs "{\"project_id\":$PROJ,\"target_ids\":[$TGT,$TGT],\"pattern_ids\":[$PAT]}")"
RUN=$(j "['id']"); check "run queued" queued "$(j "['status']")"; check "run pinned" 1 "$(j "['targets'].__len__()")"

wait_status() { # run status [seconds]
  for _ in $(seq $((${3:-30} * 5))); do
    [ "$($PSQL -c "select status from remnant.runs where id=$1")" = "$2" ] && { echo "$2"; return; }
    sleep 0.2
  done
  $PSQL -c "select status || ': ' || error_summary from remnant.runs where id=$1"
}

# The orchestrator picks the run up and drives it to review.
check "orchestrated to review" review_required "$(wait_status $RUN review_required)"
check "model calls" "{'researcher': 1, 'skeptic': 1}" "$(curl -s localhost:58081/calls | python3 -c 'import json,sys; print(json.load(sys.stdin))')"
check "run detail" 200 "$(req $RES GET /runs/$RUN)"; check "tokens recorded" 2000/400 "$(j "['input_tokens']")/$(j "['output_tokens']")"
check "hypotheses" 200 "$(req $RES GET /runs/$RUN/hypotheses)"; check "one hypothesis" 1 "$(j ".__len__()")"
HYP=$(j "[0]['id']")
check "hypothesis in triage" triage/supports/high "$(j "[0]['status']")/$(j "[0]['skeptic_verdict']")/$(j "[0]['triage_confidence']")"
check "static skipped event" "$([ $WORKER = bwrap ] && echo 0 || echo 1)" "$($PSQL -c "select count(*) from remnant.events where run_id=$RUN and event_type='stage.skipped'")"

check "hypothesis detail" 200 "$(req $RES GET /hypotheses/$HYP)"
SRC=$(python3 -c "import json; e=[e for e in json.load(open('$SP/body'))['evidence'] if e['evidence_kind']=='source_location'][0]; print(f\"{e['file_path']}:{e['line_start']}-{e['line_end']} {e['symbol']}\")")
check "source evidence" "src/parser.c:3-7 parse" "$SRC"
check "lab before accept" 409 "$(req $RES POST /hypotheses/$HYP/lab-approval '{"operation":"lab.asan_test","summary":"asan unit test"}')"
check "triage by researcher" 403 "$(req $RES PATCH /hypotheses/$HYP '{"decision":"accepted"}')"
check "triage" 200 "$(req $REV PATCH /hypotheses/$HYP '{"decision":"accepted","reviewer_note":"plausible"}')"
check "re-triage" 409 "$(req $REV PATCH /hypotheses/$HYP '{"decision":"rejected"}')"
check "lab without operation" 400 "$(req $RES POST /hypotheses/$HYP/lab-approval '{"summary":"asan unit test"}')"
check "lab static operation" 400 "$(req $RES POST /hypotheses/$HYP/lab-approval '{"operation":"static.semgrep","summary":"x"}')"
check "lab request" 201 "$(req $RES POST /hypotheses/$HYP/lab-approval '{"operation":"lab.asan_test","summary":"asan unit test"}')"
APR=$(j "['id']"); check "approval pending" pending "$(j "['decision']")"; check "scope has operation" lab.asan_test "$(j "['scope']['operation']")"
check "second pending request" 409 "$(req $RES POST /hypotheses/$HYP/lab-approval '{"operation":"lab.asan_test","summary":"again"}')"
check "approve by researcher role" 403 "$(req $RES POST /approvals/$APR '{"decision":"approved"}')"

# Change the target out from under the approval: it must go stale.
$PSQL -c "update remnant.targets set revision='sha256:changed' where id=$TGT"
check "stale approval" 409 "$(req $REV POST /approvals/$APR '{"decision":"approved"}')"
check "stale marked expired" expired "$($PSQL -c "select decision from remnant.approvals where id=$APR")"
$PSQL -c "update remnant.targets t set revision=rt.revision from remnant.run_targets rt where t.id=$TGT and rt.target_id=t.id and rt.run_id=$RUN"

check "lab request again" 201 "$(req $ADMIN POST /hypotheses/$HYP/lab-approval '{"operation":"lab.asan_test","summary":"asan unit test"}')"
APR=$(j "['id']")
check "self approval" 403 "$(req $ADMIN POST /approvals/$APR '{"decision":"approved"}')"
check "approve" 200 "$(req $REV POST /approvals/$APR '{"decision":"approved"}')"
check "decide twice" 409 "$(req $REV POST /approvals/$APR '{"decision":"denied"}')"

check "lab phase back to review" review_required "$(wait_status $RUN review_required)"
check "experiments" 200 "$(req $RES GET /runs/$RUN/experiments)"
if [ "$WORKER" = bwrap ]; then
  # The worker ran the built-in analyzer during static validation and the
  # approved ASan harness in the lab phase, both inside the sandbox.
  check "static experiment" "static.patterns finding" "$(j "[0]['operation']") $(j "[0]['status']")"
  check "lab experiment" "lab.asan_test finding" "$(j "[1]['operation']") $(j "[1]['status']")"
  check "sanitizer observation" "src/parser.c" "$($PSQL -c "select file_path from remnant.evidence where run_id=$RUN and evidence_kind='runtime_observation' and metadata->>'rule_id'='AddressSanitizer' limit 1")"
  check "lab log imported" 1 "$($PSQL -c "select count(*) from remnant.artifacts where run_id=$RUN and artifact_kind='test_log' and name='lab.log'")"
  check "hypothesis awaiting verdict" validation_pending "$($PSQL -c "select status from remnant.hypotheses where id=$HYP")"
  check "validated" 200 "$(req $REV PATCH /hypotheses/$HYP '{"decision":"validated","reviewer_note":"ASan reproduces the overflow"}')"
  check "pattern promoted" 200 "$(req $REV POST /patterns/$PAT/review '{"status":"validated"}')"
else
  # No worker runtime: the lab experiment is recorded as not run and the
  # hypothesis is handed back.
  check "lab experiment not run" "lab.asan_test failed" "$(j "[0]['operation']") $(j "[0]['status']")"
  check "hypothesis handed back" accepted "$($PSQL -c "select status from remnant.hypotheses where id=$HYP")"
  check "validated without lab finding" 409 "$(req $REV PATCH /hypotheses/$HYP '{"decision":"validated"}')"
fi

check "finalize by admin-less researcher" 202 "$(req $RES POST /runs/$RUN/finalize)"
check "run completed" completed "$(wait_status $RUN completed)"
check "finalize twice" 409 "$(req $RES POST /runs/$RUN/finalize)"
check "artifacts" 200 "$(req $RES GET /runs/$RUN/artifacts)"
check "index + 2 reports" "source_index,report,report" "$(python3 -c "import json; print(','.join(a['artifact_kind'] for a in json.load(open('$SP/body')) if a['artifact_kind'] in ('source_index','report')))")"
check "storage key hidden" 0 "$(grep -c storage_key $SP/body)"
MD=$(python3 -c "import json; print([a for a in json.load(open('$SP/body')) if a['name'].endswith('.md')][0]['id'])")
check "report download" 200 "$(curl -s -D $SP/hdr -o $SP/report.md -w '%{http_code}' -H "Authorization: Bearer $RES" $B/runs/$RUN/artifacts/$MD)"
check "report digest header" "$(sha256sum < $SP/report.md | cut -d' ' -f1)" "$(grep -i '^x-content-sha256' $SP/hdr | tr -d '\r' | cut -d' ' -f2)"
check "report content" 1 "$(grep -c '^## H'$HYP' — Length not checked before copy' $SP/report.md)"
check "artifact of other run" 404 "$(req $RES GET /runs/999/artifacts/$MD)"

check "cancel completed" 409 "$(req $RES POST /runs/$RUN/cancel)"
EV=$(timeout 5 curl -sN -H "Authorization: Bearer $RES" $B/runs/$RUN/events)
check "sse ends" 1 "$(grep -c '^event: end' <<<"$EV")"
check "sse has queued" 1 "$(grep -c '^event: run.queued' <<<"$EV")"
check "sse has report" 1 "$(grep -c '^event: report.generated' <<<"$EV")"
LAST=$(grep '^id:' <<<"$EV" | tail -1 | cut -d' ' -f2)
EV2=$(timeout 5 curl -sN -H "Authorization: Bearer $RES" -H "Last-Event-ID: $LAST" $B/runs/$RUN/events)
check "sse resume skips seen" 0 "$(grep -c '^id:' <<<"$EV2")"

check "pause completed run" 409 "$(req $RES POST /runs/$RUN/pause)"
check "pausable run" 202 "$(req $RES POST /runs "{\"project_id\":$PROJ,\"target_ids\":[$TGT],\"pattern_ids\":[$PAT]}")"
RUN3=$(j "['id']")
check "pause" 202 "$(req $RES POST /runs/$RUN3/pause)"
check "paused" paused "$(wait_status $RUN3 paused 5)"
check "pause twice" 409 "$(req $RES POST /runs/$RUN3/pause)"
sleep 3   # an orchestrator poll passes; a paused run is not claimed
check "still paused" paused "$($PSQL -c "select status from remnant.runs where id=$RUN3")"
check "resume" 202 "$(req $RES POST /runs/$RUN3/resume)"
check "resumed run finishes" review_required "$(wait_status $RUN3 review_required)"
check "resume twice" 409 "$(req $RES POST /runs/$RUN3/resume)"

check "second run" 202 "$(req $RES POST /runs "{\"project_id\":$PROJ,\"target_ids\":[$TGT]}")"
RUN2=$(j "['id']")
check "cancel" 202 "$(req $RES POST /runs/$RUN2/cancel)"
check "cancelled" cancelled "$(wait_status $RUN2 cancelled 5)"
check "cancel twice" 409 "$(req $RES POST /runs/$RUN2/cancel)"
check "run 404" 404 "$(req $RES GET /runs/99999)"

# Web UI, first-run setup, sign-in and instance policies.
U=http://127.0.0.1:58080
check "ui index" 200 "$(curl -s -D $SP/hdr -o $SP/index.html -w '%{http_code}' $U/)"
check "ui csp" 1 "$(grep -ci "^content-security-policy: default-src 'none'" $SP/hdr)"
check "ui history fallback" 200 "$(curl -s -o /dev/null -w '%{http_code}' $U/runs/$RUN)"
check "ui post rejected" 405 "$(curl -s -o /dev/null -w '%{http_code}' -X POST $U/)"
check "unknown api path" 401 "$(curl -s -o /dev/null -w '%{http_code}' $B/nope)"
check "setup not required" 200 "$(curl -s -o $SP/body -w '%{http_code}' $B/auth/status)"; check "setup flag" False "$(j "['setup_required']")"
check "setup after users" 409 "$(curl -s -o $SP/body -w '%{http_code}' -H 'Content-Type: application/json' -H 'X-Remnant-Request: 1' -d '{"code":"AAAA-BBBB-CCCC","username":"mallory","password":"mallory-passphrase","policy":{"profile":"team","session_hours":168}}' $B/setup)"
check "branding public" 200 "$(curl -s -o /dev/null -w '%{http_code}' $B/branding)"
check "branding by researcher" 403 "$(req $RES PUT /branding '{"name":"x"}')"
check "preferences" 200 "$(req $RES PUT /me/preferences '{"lang":"ko"}')"
check "preferences read back" ko "$(req $RES GET /me/preferences >/dev/null; j "['lang']")"
check "summary" 200 "$(req $RES GET /summary)"
check "approvals list" 200 "$(req $REV GET '/approvals?decision=approved')"
check "users list" 200 "$(req $RES GET /users)"; check "users listed" 3 "$(j ".__len__()")"
check "default policy" team "$(req $RES GET /instance >/dev/null; j "['policy']['profile']")"
check "policy by researcher" 403 "$(req $RES PUT /instance '{"policy":{"profile":"personal","self_approval":true,"self_review":true,"session_hours":720}}')"
check "host guard" 421 "$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: evil.example' $U/)"

# Password sign-in with a session cookie.
JAR=$SP/jar
login() { # user password [no-header] -> status; cookie in $JAR
  local h='X-Remnant-Request: 1'; [ -n "${3:-}" ] && h='X-Nothing: 1'
  curl -s -c $JAR -b $JAR -o $SP/body -w '%{http_code}' -H 'Content-Type: application/json' -H "$h" \
    -d "{\"username\":\"$1\",\"password\":\"$2\"}" $B/auth/login
}
creq() { # method path [body] with the cookie and the same-origin header
  local m=$1 p=$2; shift 2
  curl -s -b $JAR -o $SP/body -w '%{http_code}' -X "$m" -H 'X-Remnant-Request: 1' -H 'Content-Type: application/json' ${1:+-d "$1"} "$B$p"
}
echo "rita's passphrase" | $SP/remnantd reset-password -username rita -password-stdin 2>/dev/null
check "login without same-origin header" 403 "$(login rita "rita's passphrase" no-header)"
check "login wrong password" 401 "$(login rita "not her passphrase")"
check "login unknown user" 401 "$(login nobody "rita's passphrase")"
check "login" 200 "$(login rita "rita's passphrase")"; check "login role" researcher "$(j "['role']")"
check "session cookie httponly" 1 "$(grep -c '^#HttpOnly_127.0.0.1.*remnant_session' $JAR)"
check "session not stored" 0 "$($PSQL -c "select count(*) from remnant.sessions where token_sha256 like 'rms_%'")"
check "cookie me" 200 "$(creq GET /me)"
check "cookie write without header" 403 "$(curl -s -b $JAR -o /dev/null -w '%{http_code}' -X PUT -H 'Content-Type: application/json' -d '{}' $B/me/preferences)"
check "cookie write cross-site" 403 "$(curl -s -b $JAR -o /dev/null -w '%{http_code}' -X PUT -H 'X-Remnant-Request: 1' -H 'Origin: https://evil.example' -H 'Content-Type: application/json' -d '{}' $B/me/preferences)"
check "cookie write" 200 "$(creq PUT /me/preferences '{"lang":"en"}')"
check "change password wrong current" 403 "$(creq PUT /me/password '{"current_password":"nope nope nope","new_password":"ritas new passphrase"}')"
check "change password weak" 400 "$(creq PUT /me/password "{\"current_password\":\"rita's passphrase\",\"new_password\":\"short\"}")"
check "change password" 204 "$(creq PUT /me/password "{\"current_password\":\"rita's passphrase\",\"new_password\":\"rita's new passphrase\"}")"
check "session kept after change" 200 "$(creq GET /me)"
check "logout" 204 "$(creq POST /auth/logout)"
check "session ended" 401 "$(curl -s -b $JAR -o /dev/null -w '%{http_code}' $B/me)"
check "old password refused" 401 "$(login rita "rita's passphrase")"

# Temporary passwords: an administrator creates or resets; the user must change it.
check "user with temporary password" 201 "$(req $ADMIN POST /users '{"username":"tina","role":"reviewer"}')"
TMP=$(j "['temporary_password']")
check "temporary password length" 16 "${#TMP}"
check "temporary login" 200 "$(login tina "$TMP")"; check "must change" True "$(j "['must_change_password']")"
check "gated until changed" 403 "$(creq GET /runs)"; check "gate code" password_change_required "$(j "['code']")"
check "change temporary" 204 "$(creq PUT /me/password "{\"current_password\":\"$TMP\",\"new_password\":\"tina's own passphrase\"}")"
check "ungated" 200 "$(creq GET /runs)"
check "admin reset" 200 "$(req $ADMIN POST /users/$($PSQL -c "select id from remnant.users where username='tina'")/password)"
check "reset ends sessions" 401 "$(curl -s -b $JAR -o /dev/null -w '%{http_code}' $B/me)"

# Account requests follow the policy; the managed profile takes them.
check "request refused in team" 403 "$(curl -s -o /dev/null -w '%{http_code}' -H 'X-Remnant-Request: 1' -H 'Content-Type: application/json' -d '{"username":"uma","password":"uma passphrase"}' $B/auth/request-account)"
check "managed policy" 200 "$(req $ADMIN PUT /instance '{"policy":{"profile":"managed","admin_lab_approval":true,"account_requests":true,"session_hours":12}}')"
check "managed profile" managed "$(j "['policy']['profile']")"
check "account request" 202 "$(curl -s -o /dev/null -w '%{http_code}' -H 'X-Remnant-Request: 1' -H 'Content-Type: application/json' -d '{"username":"uma","password":"uma passphrase"}' $B/auth/request-account)"
check "pending sign-in" 403 "$(login uma "uma passphrase")"; check "pending code" pending "$(j "['code']")"
UMA=$($PSQL -c "select id from remnant.users where username='uma'")
check "approve request" 200 "$(req $ADMIN PATCH /users/$UMA '{"approve":true}')"
check "approved sign-in" 200 "$(login uma "uma passphrase")"
check "delete active account" 409 "$(req $ADMIN DELETE /users/$UMA)"

# Self-review follows the policy.
check "own pattern" 201 "$(req $ADMIN POST /patterns '{"title":"Own pattern","vulnerability_class":"memory-safety","root_cause":"self review check","language":"c"}')"
OWN=$(j "['id']")
check "self review refused" 403 "$(req $ADMIN POST /patterns/$OWN/review '{"status":"reviewed"}')"
check "personal policy" 200 "$(req $ADMIN PUT /instance '{"policy":{"profile":"personal","self_approval":true,"self_review":true,"session_hours":720}}')"
check "personal self review" 200 "$(req $ADMIN POST /patterns/$OWN/review '{"status":"reviewed"}')"
check "self review recorded" t "$($PSQL -c "select self_reviewed from remnant.patterns where id=$OWN")"
check "back to team" 200 "$(req $ADMIN PUT /instance '{"policy":{"profile":"team","session_hours":168}}')"

# Model profiles, agents and expert settings.
check "profile test" True "$(req $ADMIN POST /llm-profiles/$LLM/test >/dev/null; j "['ok']")"
check "profile test by researcher" 403 "$(req $RES POST /llm-profiles/$LLM/test)"
check "unsaved bad key" False "$(req $ADMIN POST /llm-profiles/test '{"model":"claude-opus-5-5","api_key":"wrong-key","base_url":"http://127.0.0.1:58081"}' >/dev/null; j "['ok']")"
check "unsaved unknown model" 'model "gpt-x" was not found (404)' "$(req $ADMIN POST /llm-profiles/test '{"model":"gpt-x","api_key":"sk-ant-test-0123456789","base_url":"http://127.0.0.1:58081"}' >/dev/null; j "['error']")"
check "profile options" 200 "$(req $ADMIN PATCH /llm-profiles/$LLM '{"options":{"thinking":"adaptive","timeout_seconds":120,"max_retries":1}}')"
check "profile options saved" adaptive/120 "$(j "['options']['thinking']")/$(j "['options']['timeout_seconds']")"
check "profile bad options" 400 "$(req $ADMIN PATCH /llm-profiles/$LLM '{"options":{"thinking":"always"}}')"
check "profile key rotation" 200 "$(req $ADMIN PATCH /llm-profiles/$LLM '{"api_key":"sk-ant-rotated-99999999"}')"; check "rotated hint" "…9999" "$(j "['api_key_hint']")"
check "rotated key works" True "$(req $ADMIN POST /llm-profiles/$LLM/test >/dev/null; j "['ok']")"
check "delete profile in use" 409 "$(req $ADMIN DELETE /llm-profiles/$LLM)"
check "agents" 200 "$(req $RES GET /agents)"; check "three agents" 3 "$(j ".__len__()")"
check "agent by researcher" 403 "$(req $RES PATCH /agents/skeptic '{"enabled":false}')"
check "agent instructions" 200 "$(req $ADMIN PATCH /agents/hypothesis_researcher '{"config":{"instructions":"Prefer length arithmetic.","max_call_tokens":4096}}')"
check "agent bad cap" 400 "$(req $ADMIN PATCH /agents/skeptic '{"config":{"max_call_tokens":10}}')"
check "unknown agent" 404 "$(req $ADMIN PATCH /agents/reporter '{"enabled":false}')"
check "research settings" 200 "$(req $RES GET /settings/research)"; check "default ttl" 24 "$(j "['settings']['approval_ttl_hours']")"
SET=$(python3 -c "import json; d=json.load(open('$SP/body'))['settings']; print(json.dumps(d))")
mut() { python3 -c 'import json,sys; d=json.loads(sys.argv[1]); exec(sys.argv[2]); print(json.dumps(d))' "$SET" "$1"; }
SET=$(mut "d['approval_ttl_hours']=48; d['run_defaults']['max_tokens']=300000")
check "research settings by researcher" 403 "$(req $RES PUT /settings/research "$SET")"
check "research settings save" 200 "$(req $ADMIN PUT /settings/research "$SET")"
check "bad research settings" 422 "$(req $ADMIN PUT /settings/research "$(mut "d['max_candidates']=0")")"
check "unknown analyzer" 400 "$(req $ADMIN PUT /settings/research "$(mut "d['disabled_static_operations']=['static.magic']")")"
check "run uses new defaults" 202 "$(req $RES POST /runs "{\"project_id\":$PROJ,\"target_ids\":[$TGT]}")"; check "default tokens applied" 300000 "$(j "['limits']['max_tokens']")"
RUN5=$(j "['id']"); req $RES POST /runs/$RUN5/cancel >/dev/null
check "system config" 200 "$(req $ADMIN GET /system/config)"; check "key source" environment "$(j "['master_key_source']")"
check "no secrets in config" 0 "$(grep -c "$REMNANT_MASTER_KEY" $SP/body)"
check "system config by researcher" 403 "$(req $RES GET /system/config)"

# Repository intake. The smoke server allows no remote hosts, so intakes are
# refused with the reason; the end-to-end path is covered by the Go tests.
check "intake config" 200 "$(req $RES GET /intakes/config)"; check "no repo hosts" "[]" "$(j "['repo_hosts']")"
check "intake refused without hosts" 403 "$(req $RES POST /intakes '{"url":"github.com/org/repo"}')"; check "intake reason" source_policy "$(j "['code']")"
check "intake by reviewer" 403 "$(req $REV POST /intakes '{"url":"github.com/org/repo"}')"
check "intakes list" 200 "$(req $RES GET /intakes)"; check "no intakes" 0 "$(j ".__len__()")"
check "historian agent" 200 "$(req $ADMIN PATCH /agents/historian '{"config":{"instructions":"Ignore test-only commits."}}')"

check "server logged no errors" 0 "$(grep -c '"level":"ERROR"' $SP/serve.log)"

# Crash recovery: kill remnantd while a run is in hypothesis_generation,
# restart it, and let the reaper requeue the run once its 30 s lease expires.
curl -s -X POST 'localhost:58081/delay?s=4' >/dev/null
check "crash run" 202 "$(req $RES POST /runs "{\"project_id\":$PROJ,\"target_ids\":[$TGT],\"pattern_ids\":[$PAT]}")"
RUN4=$(j "['id']")
check "crash run generating" hypothesis_generation "$(wait_status $RUN4 hypothesis_generation 15)"
kill -9 $PID; wait $PID 2>/dev/null
curl -s -X POST 'localhost:58081/delay?s=0' >/dev/null
$SP/remnantd serve 2>>$SP/serve.log & PID=$!
check "crash run recovered" review_required "$(wait_status $RUN4 review_required 60)"
check "recovery recorded" "1 hypothesis_generation" "$($PSQL -c "select attempts || ' ' || (select payload->>'resume_stage' from remnant.events where run_id=$RUN4 and event_type='run.resumed' limit 1) from remnant.runs where id=$RUN4")"
check "no duplicate candidates" 1 "$($PSQL -c "select count(*) from remnant.evidence where run_id=$RUN4 and hypothesis_id is null and metadata ? 'candidate_key'")"
echo "---- $pass passed, $fail failed"
[ $fail -eq 0 ] || grep '"level":"ERROR"' $SP/serve.log
[ $fail -eq 0 ]
