#!/bin/sh
# Local-mode tests for scripts/deploy.sh (FRIDGE_DEPLOY_LOCAL=1): install,
# backup, verify, automatic rollback, and make rollback, in a temp directory,
# with no SSH and no real systemd. Run with: sh scripts/deploy_test.sh
set -eu

ROOT_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
DEPLOY="$ROOT_DIR/scripts/deploy.sh"
WORK="$(mktemp -d)"
HEALTHZ_PID=""
cleanup() {
	if [ -n "${HEALTHZ_PID:-}" ]; then
		kill "$HEALTHZ_PID" >/dev/null 2>&1 || true
	fi
	rm -rf "$WORK"
}
trap cleanup EXIT

ENV_FILE="$WORK/env.deploy"
: >"$ENV_FILE"
export FRIDGE_ENV_FILE="$ENV_FILE"
export FRIDGE_DEPLOY_LOCAL=1
export FRIDGE_ROOT="$WORK/root"
export FRIDGE_DEPLOY_SERVICE=fridge
export FRIDGE_SWITCH="$ROOT_DIR/scripts/fridge-switch.sh"
export FRIDGE_SKIP_BUILD=1
export FRIDGE_SERVER_BIN="$WORK/dist/server"
export FRIDGE_INVITE_BIN="$WORK/dist/invite"
unset FRIDGE_DEPLOY_HOST FRIDGE_DEPLOY_USER FRIDGE_URL FRIDGE_EXPECTED_SHA GIT_SHA

mkdir -p "$FRIDGE_ROOT/etc/fridge" "$FRIDGE_ROOT/usr/local/bin" \
	"$FRIDGE_ROOT/var/lib/fridge" "$FRIDGE_ROOT/var/backups/fridge" \
	"$WORK/dist" "$WORK/stub" "$WORK/healthz"

cat >"$FRIDGE_ROOT/etc/fridge/fridge.env" <<'ENVFILE'
COOKIE_SECURE=1
TRUST_PROXY=1
FRIDGE_ADDR=127.0.0.1:8082
FRIDGE_DB=/var/lib/fridge/fridge.db
ENVFILE

cat >"$WORK/stub/systemctl" <<'STUB'
#!/bin/sh
set -eu
log="${FRIDGE_TEST_LOG:?}"
live="${FRIDGE_TEST_LIVE:?}"
root="${FRIDGE_ROOT:?}"
action=""
unit=""
for arg in "$@"; do
	case "$arg" in
		--*) continue ;;
		start|stop|restart|status|is-active) action="$arg" ;;
		*) unit="$arg" ;;
	esac
done
unit="${unit%.service}"
printf '%s %s\n' "$action" "$unit" >>"$log"
case "$action $unit" in
	"start fridge-backup"|"restart fridge-backup")
		db="$root/var/lib/fridge/fridge.db"
		dir="$root/var/backups/fridge"
		mkdir -p "$dir"
		if [ -f "$db" ]; then
			ts="$(date -u +%Y%m%dT%H%M%SZ)"
			cp "$db" "$dir/fridge-$ts-$$.db"
		else
			exit 1
		fi
		;;
	"start fridge"|"restart fridge")
		server="$root/usr/local/bin/fridge-server"
		if [ -f "$server" ]; then
			cp "$server" "$live"
		fi
		;;
	*) ;;
esac
STUB
chmod +x "$WORK/stub/systemctl"

PATH="$WORK/stub:$PATH"
export PATH
export FRIDGE_TEST_LOG="$WORK/systemctl.log"
export FRIDGE_TEST_LIVE="$WORK/healthz/commit"
: >"$FRIDGE_TEST_LOG"
: >"$FRIDGE_TEST_LIVE"

cat >"$WORK/healthz/server.py" <<'PYSRC'
import json
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
import os

commit_path = Path(os.environ["FRIDGE_TEST_LIVE"])
port = int(os.environ["FRIDGE_TEST_PORT"])

class H(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path.split("?", 1)[0] != "/healthz":
            self.send_error(404)
            return
        commit = commit_path.read_text().strip() if commit_path.exists() else ""
        body = json.dumps({"status": "ok", "commit": commit}, separators=(",", ":")).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):
        return

HTTPServer(("127.0.0.1", port), H).serve_forever()
PYSRC

FRIDGE_TEST_PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')"
export FRIDGE_TEST_PORT
python3 "$WORK/healthz/server.py" &
HEALTHZ_PID=$!
export FRIDGE_URL="http://127.0.0.1:$FRIDGE_TEST_PORT"

i=0
while [ "$i" -lt 20 ]; do
	if curl -fsS --max-time 1 "$FRIDGE_URL/healthz" >/dev/null 2>&1; then
		break
	fi
	i=$((i + 1))
	sleep 0.1
done

failures=0
pass() { echo "ok - $1"; }
fail() {
	echo "FAIL - $1" >&2
	failures=$((failures + 1))
}
check() {
	name="$1"
	shift
	if "$@"; then pass "$name"; else fail "$name"; fi
}

healthz_has() {
	curl -fsS "$FRIDGE_URL/healthz" | grep -Fq "$1"
}

write_binaries() {
	sha="$1"
	printf '%s\n' "$sha" >"$WORK/dist/server"
	printf 'invite-%s\n' "$sha" >"$WORK/dist/invite"
	chmod +x "$WORK/dist/server" "$WORK/dist/invite"
}

# 1. First deploy, no database.
write_binaries firstsha
if ! FRIDGE_EXPECTED_SHA=firstsha "$DEPLOY" >"$WORK/out.log" 2>&1; then
	cat "$WORK/out.log" >&2
	fail "first deploy without a database"
else
	pass "first deploy without a database"
fi
check "first deploy installed the server" grep -q firstsha "$FRIDGE_ROOT/usr/local/bin/fridge-server"
check "first deploy installed the invite CLI" grep -q invite-firstsha "$FRIDGE_ROOT/usr/local/bin/fridge-invite"
check "first deploy skipped a pre-deploy backup" [ ! -f "$FRIDGE_ROOT/var/backups/fridge/pre-deploy.db" ]
check "healthz serves the first SHA" healthz_has '"commit":"firstsha"'

# 2. Second deploy with a database: backup, .previous, new SHA.
echo "items" >"$FRIDGE_ROOT/var/lib/fridge/fridge.db"
sleep 1
write_binaries secondsha
if ! FRIDGE_EXPECTED_SHA=secondsha "$DEPLOY" >"$WORK/out.log" 2>&1; then
	cat "$WORK/out.log" >&2
	fail "deploy with a database"
else
	pass "deploy with a database"
fi
check "previous server was kept" grep -q firstsha "$FRIDGE_ROOT/usr/local/bin/fridge-server.previous"
check "pre-deploy backup was recorded" [ -f "$FRIDGE_ROOT/var/backups/fridge/pre-deploy.db" ]
check "healthz serves the second SHA" healthz_has '"commit":"secondsha"'
check "backup unit was started" grep -q "start fridge-backup" "$FRIDGE_TEST_LOG"

# 3. Explicit rollback restores the previous successful release and pre-deploy DB.
printf 'newer-data\n' >"$FRIDGE_ROOT/var/lib/fridge/fridge.db"
if ! "$DEPLOY" rollback >"$WORK/out.log" 2>&1; then
	cat "$WORK/out.log" >&2
	fail "explicit rollback"
else
	pass "explicit rollback"
fi
check "rollback restored the previous server" grep -q firstsha "$FRIDGE_ROOT/usr/local/bin/fridge-server"
check "rollback restored the pre-deploy database" grep -q items "$FRIDGE_ROOT/var/lib/fridge/fridge.db"

# 4. A SHA mismatch after a later deploy is rolled back automatically.
sleep 1
write_binaries secondsha
if ! FRIDGE_EXPECTED_SHA=secondsha "$DEPLOY" >"$WORK/out.log" 2>&1; then
	cat "$WORK/out.log" >&2
	fail "redeploy after rollback"
else
	pass "redeploy after rollback"
fi
sleep 1
write_binaries badsha
if FRIDGE_EXPECTED_SHA=expected-other "$DEPLOY" >"$WORK/out.log" 2>&1; then
	fail "deploy with a SHA mismatch should fail"
else
	check "reports an automatic rollback" grep -q "rolling back automatically" "$WORK/out.log"
fi
check "server was restored after a failed deploy" grep -q secondsha "$FRIDGE_ROOT/usr/local/bin/fridge-server"
check "healthz still serves the previous SHA" healthz_has '"commit":"secondsha"'

# 5. Remote mode needs a host, and unsafe keys/paths are rejected.
if FRIDGE_DEPLOY_LOCAL='' "$DEPLOY" verify >"$WORK/out.log" 2>&1; then
	fail "remote mode requires FRIDGE_DEPLOY_HOST"
else
	check "remote mode requires FRIDGE_DEPLOY_HOST" grep -q "FRIDGE_DEPLOY_HOST" "$WORK/out.log"
fi
if FRIDGE_DEPLOY_PATH='/usr/local/bin/fridge server' "$DEPLOY" verify >"$WORK/out.log" 2>&1; then
	fail "rejects unsafe deploy paths"
else
	pass "rejects unsafe deploy paths"
fi
printf 'PATH=/tmp\n' >"$ENV_FILE"
if "$DEPLOY" verify >"$WORK/out.log" 2>&1; then
	fail ".env.deploy only accepts FRIDGE_ keys"
else
	pass ".env.deploy only accepts FRIDGE_ keys"
fi
: >"$ENV_FILE"

# 6. restore-drill against local backups (uses real sqlite3 when present).
if command -v sqlite3 >/dev/null; then
	drill_dir="$WORK/drill-backups"
	mkdir -p "$drill_dir"
	drill_db="$WORK/drill.db"
	sqlite3 "$drill_db" "CREATE TABLE items (id INTEGER); INSERT INTO items VALUES (1), (2);"
	cp "$drill_db" "$drill_dir/fridge-20260101T000000Z.db"
	if "$ROOT_DIR/scripts/restore-drill.sh" "$drill_dir" >"$WORK/drill.log" 2>&1; then
		check "restore-drill reports item rows" grep -q "item rows:      2" "$WORK/drill.log"
	else
		cat "$WORK/drill.log" >&2
		fail "restore-drill"
	fi
else
	echo "sqlite3 not installed; skipping restore-drill test"
fi

if [ "$failures" -gt 0 ]; then
	echo "$failures test(s) failed" >&2
	exit 1
fi
echo "all deploy script tests passed"
