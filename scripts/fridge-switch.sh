#!/bin/sh
# Root helper used by deploy.sh (via a narrow sudoers rule) to install or
# roll back one fridge unit. The only systemd units it may start, stop, or
# restart are that app unit and its oneshot backup unit.
#
# usage: fridge-switch.sh install|rollback fridge|fridge-preview [server_bin] [invite_bin]
set -eu

die() {
	echo "fridge-switch: $*" >&2
	exit 1
}

prefix_path() {
	path="$1"
	if [ -n "${FRIDGE_ROOT:-}" ]; then
		case "$path" in
			/*) path="${FRIDGE_ROOT}$path" ;;
		esac
	fi
	printf '%s\n' "$path"
}

ACTION="${1:-}"
SERVICE="${2:-}"
case "$ACTION" in
	install | rollback) ;;
	*) die "usage: $0 install|rollback fridge|fridge-preview [server_bin] [invite_bin]" ;;
esac
case "$SERVICE" in
	fridge)
		ENV_FILE="$(prefix_path /etc/fridge/fridge.env)"
		TARGET="$(prefix_path /usr/local/bin/fridge-server)"
		INVITE_TARGET="$(prefix_path /usr/local/bin/fridge-invite)"
		BACKUP_UNIT="fridge-backup.service"
		DEFAULT_DB="$(prefix_path /var/lib/fridge/fridge.db)"
		DEFAULT_BACKUP_DIR="$(prefix_path /var/backups/fridge)"
		DATA_DIR="$(prefix_path /var/lib/fridge)"
		;;
	fridge-preview)
		ENV_FILE="$(prefix_path /etc/fridge/fridge-preview.env)"
		TARGET="$(prefix_path /usr/local/bin/fridge-preview-server)"
		INVITE_TARGET="$(prefix_path /usr/local/bin/fridge-preview-invite)"
		BACKUP_UNIT="fridge-preview-backup.service"
		DEFAULT_DB="$(prefix_path /var/lib/fridge-preview/fridge.db)"
		DEFAULT_BACKUP_DIR="$(prefix_path /var/backups/fridge-preview)"
		DATA_DIR="$(prefix_path /var/lib/fridge-preview)"
		;;
	*) die "service must be fridge or fridge-preview" ;;
esac

as_root() {
	if [ "$(id -u)" -eq 0 ] || [ "${FRIDGE_DEPLOY_LOCAL:-}" = "1" ]; then
		"$@"
	else
		sudo -n "$@"
	fi
}

# install(mode, src, dest) skips root:root ownership in local tests.
install_as_root() {
	mode="$1"
	src="$2"
	dest="$3"
	if [ "${FRIDGE_DEPLOY_LOCAL:-}" = "1" ]; then
		as_root install -m "$mode" "$src" "$dest"
	else
		as_root install -o root -g root -m "$mode" "$src" "$dest"
	fi
}

read_config() {
	if [ ! -r "$ENV_FILE" ]; then
		die "missing environment file: $ENV_FILE"
	fi
	# shellcheck disable=SC1090
	DB_PATH="$(set -a && . "$ENV_FILE" && printf '%s\n' "${FRIDGE_DB:-}")"
	# shellcheck disable=SC1090
	BACKUP_DIR="$(set -a && . "$ENV_FILE" && printf '%s\n' "${FRIDGE_BACKUP_DIR:-}")"
	if [ -z "$DB_PATH" ]; then
		DB_PATH="$DEFAULT_DB"
	else
		DB_PATH="$(prefix_path "$DB_PATH")"
	fi
	if [ -z "$BACKUP_DIR" ]; then
		BACKUP_DIR="$DEFAULT_BACKUP_DIR"
	else
		BACKUP_DIR="$(prefix_path "$BACKUP_DIR")"
	fi
}

latest_backup() {
	latest=
	for file in "$BACKUP_DIR"/fridge-*.db; do
		if [ -f "$file" ]; then
			latest="$file"
		fi
	done
	printf '%s\n' "$latest"
}

backup_database() {
	if ! as_root test -f "$DB_PATH"; then
		echo "database does not exist yet; skipping backup"
		as_root rm -f "$BACKUP_DIR/pre-deploy.db"
		return 0
	fi
	echo "creating database backup before deploy"
	before="$(latest_backup)"
	as_root systemctl start "$BACKUP_UNIT"
	backup="$(latest_backup)"
	if [ -z "$backup" ] || [ "$backup" = "$before" ]; then
		die "backup unit did not create a new backup in $BACKUP_DIR"
	fi
	echo "recording $backup as the pre-deploy backup"
	install_as_root 0640 "$backup" "$BACKUP_DIR/pre-deploy.db.new"
	as_root mv "$BACKUP_DIR/pre-deploy.db.new" "$BACKUP_DIR/pre-deploy.db"
}

restore_pre_deploy_db() {
	if ! as_root test -f "$BACKUP_DIR/pre-deploy.db"; then
		return 0
	fi
	echo "restoring pre-deploy database"
	as_root install -d -m 0750 "$DATA_DIR"
	for file in "$DB_PATH" "$DB_PATH-wal" "$DB_PATH-shm"; do
		if as_root test -e "$file"; then
			as_root mv "$file" "$file.rolled-back"
		fi
	done
	if [ "${FRIDGE_DEPLOY_LOCAL:-}" = "1" ]; then
		as_root install -m 0640 "$BACKUP_DIR/pre-deploy.db" "$DB_PATH"
	else
		as_root install -o fridge -g fridge -m 0640 "$BACKUP_DIR/pre-deploy.db" "$DB_PATH"
	fi
}

install_binaries() {
	server_src="${1:-}"
	invite_src="${2:-}"
	[ -n "$server_src" ] && [ -f "$server_src" ] || die "server binary is missing: $server_src"
	install_as_root 0755 "$server_src" "$TARGET.new"
	if [ -n "$invite_src" ]; then
		[ -f "$invite_src" ] || die "invite binary is missing: $invite_src"
		install_as_root 0755 "$invite_src" "$INVITE_TARGET.new"
	fi
	if [ -x "$TARGET" ]; then
		echo "preserving previous binary at $TARGET.previous"
		as_root cp "$TARGET" "$TARGET.previous"
	fi
	if [ -n "$invite_src" ] && [ -x "$INVITE_TARGET" ]; then
		as_root cp "$INVITE_TARGET" "$INVITE_TARGET.previous"
	fi
	as_root mv "$TARGET.new" "$TARGET"
	if [ -n "$invite_src" ]; then
		as_root mv "$INVITE_TARGET.new" "$INVITE_TARGET"
	fi
}

case "$ACTION" in
install)
	read_config
	server_src="${3:-}"
	invite_src="${4:-}"
	as_root systemctl stop "$SERVICE" || true
	backup_database
	install_binaries "$server_src" "$invite_src"
	as_root systemctl start "$SERVICE"
	as_root systemctl --no-pager --full status "$SERVICE" || true
	;;
rollback)
	read_config
	[ -x "$TARGET.previous" ] || die "previous binary is missing or not executable: $TARGET.previous"
	echo "rolling back $SERVICE"
	as_root systemctl stop "$SERVICE" || true
	restore_pre_deploy_db
	as_root cp "$TARGET" "$TARGET.rolled-back"
	install_as_root 0755 "$TARGET.previous" "$TARGET.new"
	as_root mv "$TARGET.new" "$TARGET"
	if [ -x "$INVITE_TARGET.previous" ]; then
		as_root cp "$INVITE_TARGET" "$INVITE_TARGET.rolled-back" || true
		install_as_root 0755 "$INVITE_TARGET.previous" "$INVITE_TARGET.new"
		as_root mv "$INVITE_TARGET.new" "$INVITE_TARGET"
	fi
	as_root systemctl start "$SERVICE"
	as_root systemctl --no-pager --full status "$SERVICE" || true
	;;
esac
