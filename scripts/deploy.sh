#!/bin/sh
# Deploy the fridge server and invite CLI to the VPS. See docs/DEPLOY.md.
#
#   scripts/deploy.sh [deploy]   build (unless skipped), upload, restart, verify
#   scripts/deploy.sh rollback   restore the previous binary and pre-deploy DB
#   scripts/deploy.sh verify     check public /healthz without deploying
#
# Settings come from the environment or from .env.deploy (KEY=value lines;
# already-set values win). See .env.deploy.example.
set -eu

ACTION="${1:-deploy}"
ROOT_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT_DIR"

die() {
	echo "deploy: $*" >&2
	exit 1
}

ENV_FILE="${FRIDGE_ENV_FILE:-$ROOT_DIR/.env.deploy}"
if [ -f "$ENV_FILE" ]; then
	while IFS= read -r line || [ -n "$line" ]; do
		case "$line" in '' | '#'*) continue ;; esac
		key="${line%%=*}"
		value="${line#*=}"
		case "$key" in
			FRIDGE_*) ;;
			*) die "$ENV_FILE: expected FRIDGE_NAME=value, got: $line" ;;
		esac
		case "$key" in *[!A-Z_]*) die "$ENV_FILE: bad name: $key" ;; esac
		eval "already=\${$key-}" # $key is checked above: capitals and underscores only
		if [ -z "$already" ]; then
			export "$key=$value"
		fi
	done <"$ENV_FILE"
fi

LOCAL="${FRIDGE_DEPLOY_LOCAL:-}"
HOST="${FRIDGE_DEPLOY_HOST:-}"
SSH_USER="${FRIDGE_DEPLOY_USER:-}"
SERVICE="${FRIDGE_DEPLOY_SERVICE:-fridge}"
BIN_DIR="${FRIDGE_DEPLOY_PATH:-/usr/local/bin}"
URL="${FRIDGE_URL:-}"
URL="${URL%/}"
EXPECTED_SHA="${FRIDGE_EXPECTED_SHA:-}"

case "$SERVICE" in
	fridge | fridge-preview) ;;
	*) die "FRIDGE_DEPLOY_SERVICE must be fridge or fridge-preview" ;;
esac
# Deployed units always install under /usr/local/bin (see fridge-switch.sh).
# FRIDGE_DEPLOY_PATH is accepted so CI can set the same DEPLOY_PATH variable
# used by the other sites; it must be /usr/local/bin.
case "$BIN_DIR" in
	/usr/local/bin) ;;
	/*)
		# Still allow a custom absolute path in local tests, but reject junk.
		case "$BIN_DIR" in
			*[!A-Za-z0-9._/-]* | / | */) die "FRIDGE_DEPLOY_PATH may only use letters, digits and ._/- and must not end in /" ;;
		esac
		;;
	*) die "FRIDGE_DEPLOY_PATH must be absolute: $BIN_DIR" ;;
esac

SWITCH="${FRIDGE_SWITCH:-$ROOT_DIR/scripts/fridge-switch.sh}"

if [ "$LOCAL" = "1" ]; then
	WHERE="local ${FRIDGE_ROOT:-/}"
	on_switch() {
		FRIDGE_DEPLOY_LOCAL=1 "$SWITCH" "$@"
	}
else
	[ -n "$HOST" ] || die "set FRIDGE_DEPLOY_HOST (an SSH host or ~/.ssh/config alias) in .env.deploy"
	case "$HOST$SSH_USER" in *[!A-Za-z0-9._@-]*) die "FRIDGE_DEPLOY_HOST/USER have unexpected characters" ;; esac
	if [ -n "$SSH_USER" ]; then
		REMOTE="$SSH_USER@$HOST"
	else
		REMOTE="$HOST"
	fi
	WHERE="$REMOTE"
	on_switch() {
		# Arguments are restricted to known service names and uploaded temp paths.
		# shellcheck disable=SC2029
		ssh "$REMOTE" sudo -n /usr/local/sbin/fridge-switch "$@"
	}
fi

healthz_url() {
	if [ -n "$URL" ]; then
		printf '%s/healthz\n' "$URL"
	fi
}

check_healthz() {
	endpoint="$(healthz_url)"
	if [ -z "$endpoint" ]; then
		echo "FRIDGE_URL not set; skipping the live check"
		return 0
	fi
	echo "checking $endpoint"
	if ! body="$(curl -fsS --retry 10 --retry-connrefused --retry-delay 1 --retry-max-time 30 --max-time 5 "$endpoint")"; then
		echo "health check did not answer" >&2
		return 1
	fi
	if ! printf '%s\n' "$body" | grep -Fq '"status":"ok"'; then
		echo "health check is not ok: $body" >&2
		return 1
	fi
	if [ -n "$EXPECTED_SHA" ]; then
		if ! printf '%s\n' "$body" | grep -Fq "\"commit\":\"$EXPECTED_SHA\""; then
			echo "live commit does not match $EXPECTED_SHA: $body" >&2
			return 1
		fi
	fi
	echo "health check looks right"
}

verify_healthz() {
	check_healthz || die "health verification failed"
}

case "$ACTION" in
deploy) ;;
rollback)
	echo "rolling back $SERVICE on $WHERE"
	on_switch rollback "$SERVICE"
	verify_healthz
	echo "rollback verified"
	exit 0
	;;
verify)
	verify_healthz
	exit 0
	;;
*) die "usage: $0 [deploy|rollback|verify]" ;;
esac

GIT_SHA="${GIT_SHA:-$(git rev-parse HEAD 2>/dev/null || echo unknown)}"
if [ -n "$EXPECTED_SHA" ] && [ "$GIT_SHA" != "$EXPECTED_SHA" ] && [ "${FRIDGE_SKIP_BUILD:-}" != "1" ]; then
	die "checkout does not match the tested commit"
fi
if [ -n "$EXPECTED_SHA" ]; then
	GIT_SHA="$EXPECTED_SHA"
fi

SERVER_BIN="${FRIDGE_SERVER_BIN:-$ROOT_DIR/dist/fridge-server-linux-amd64}"
INVITE_BIN="${FRIDGE_INVITE_BIN:-$ROOT_DIR/dist/fridge-invite-linux-amd64}"

if [ "${FRIDGE_SKIP_BUILD:-}" = "1" ]; then
	echo "using existing binaries (FRIDGE_SKIP_BUILD=1)"
else
	echo "building linux/amd64 server and invite CLI"
	make -C "$ROOT_DIR" build-server build-invite-linux GIT_SHA="$GIT_SHA"
fi
[ -f "$SERVER_BIN" ] || die "server binary is missing: $SERVER_BIN"
[ -f "$INVITE_BIN" ] || die "invite binary is missing: $INVITE_BIN"

REMOTE_TMP=""
cleanup() {
	if [ -n "$REMOTE_TMP" ] && [ "$LOCAL" != "1" ]; then
		# shellcheck disable=SC2029
		ssh "$REMOTE" "rm -rf '$REMOTE_TMP'" >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT HUP INT TERM

echo "deploying $GIT_SHA to $WHERE ($SERVICE)"
if [ "$LOCAL" = "1" ]; then
	on_switch install "$SERVICE" "$SERVER_BIN" "$INVITE_BIN"
else
	REMOTE_TMP="/tmp/fridge-deploy.$$"
	# REMOTE_TMP is generated locally and intentionally expanded here.
		# shellcheck disable=SC2029
		ssh "$REMOTE" "mkdir -p '$REMOTE_TMP'"
	scp "$SERVER_BIN" "$REMOTE:$REMOTE_TMP/server"
	scp "$INVITE_BIN" "$REMOTE:$REMOTE_TMP/invite"
	on_switch install "$SERVICE" "$REMOTE_TMP/server" "$REMOTE_TMP/invite"
fi

if ! check_healthz; then
	echo "verification failed; rolling back automatically" >&2
	on_switch rollback "$SERVICE" || true
	die "deploy failed; rolled back"
fi
echo "deployed $GIT_SHA"
