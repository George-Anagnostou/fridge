#!/bin/sh
# Install the backup and optional offsite-copy units for one fridge environment.
# usage: install-fridge-backup.sh fridge|fridge-preview
set -eu

SERVICE="${1:-fridge}"
case "$SERVICE" in
	fridge)
		ENV_FILE="/etc/fridge/fridge.env"
		BACKUP_UNIT="fridge-backup"
		BACKUP_SCRIPT="/usr/local/sbin/fridge-backup"
		OFFSITE_UNIT="fridge-offsite"
		OFFSITE_SCRIPT="/usr/local/sbin/fridge-offsite"
		DEFAULT_DB="$DATA_DIR/fridge.db"
		DEFAULT_BACKUP_DIR="/var/backups/fridge"
		;;
	fridge-preview)
		ENV_FILE="/etc/fridge/fridge-preview.env"
		BACKUP_UNIT="fridge-preview-backup"
		BACKUP_SCRIPT="/usr/local/sbin/fridge-preview-backup"
		OFFSITE_UNIT="fridge-preview-offsite"
		OFFSITE_SCRIPT="/usr/local/sbin/fridge-preview-offsite"
		DEFAULT_DB="$DATA_DIR/fridge.db"
		DEFAULT_BACKUP_DIR="/var/backups/fridge-preview"
		;;
	*)
		echo "usage: $0 fridge|fridge-preview" >&2
		exit 2
		;;
esac

if [ "$(id -u)" -ne 0 ]; then
	echo "run this script as root on the Debian VPS" >&2
	exit 1
fi

DB_PATH="${FRIDGE_DB:-$DEFAULT_DB}"
BACKUP_DIR="${FRIDGE_BACKUP_DIR:-$DEFAULT_BACKUP_DIR}"
BACKUP_RETENTION_DAYS="${FRIDGE_BACKUP_RETENTION_DAYS:-30}"

if [ -r "$ENV_FILE" ]; then
	# shellcheck disable=SC1090
	. "$ENV_FILE"
	DB_PATH="${FRIDGE_DB:-$DB_PATH}"
	BACKUP_DIR="${FRIDGE_BACKUP_DIR:-$BACKUP_DIR}"
	BACKUP_RETENTION_DAYS="${FRIDGE_BACKUP_RETENTION_DAYS:-$BACKUP_RETENTION_DAYS}"
fi

echo "installing sqlite3 and rsync if needed"
apt-get update
apt-get install -y sqlite3 rsync

echo "creating backup directory $BACKUP_DIR"
install -d -o root -g root -m 0750 "$BACKUP_DIR"

echo "installing $BACKUP_SCRIPT"
cat >"$BACKUP_SCRIPT" <<'BACKUP'
#!/bin/sh
set -eu

DB_PATH="${FRIDGE_DB:-__DEFAULT_DB__}"
BACKUP_DIR="${FRIDGE_BACKUP_DIR:-__DEFAULT_BACKUP_DIR__}"
RETENTION_DAYS="${FRIDGE_BACKUP_RETENTION_DAYS:-30}"

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup_path="${BACKUP_DIR}/fridge-${timestamp}-$$.db"
tmp_path="${backup_path}.tmp"
trap 'rm -f "$tmp_path"' EXIT

install -d -o root -g root -m 0750 "$BACKUP_DIR"

if [ ! -f "$DB_PATH" ]; then
	echo "database does not exist: $DB_PATH" >&2
	exit 1
fi

sqlite3 "$DB_PATH" ".backup '$tmp_path'"
integrity_result="$(sqlite3 "$tmp_path" "PRAGMA integrity_check;")"
if [ "$integrity_result" != "ok" ]; then
	echo "backup failed integrity_check: $integrity_result" >&2
	exit 1
fi

chmod 0640 "$tmp_path"
mv "$tmp_path" "$backup_path"

find "$BACKUP_DIR" \
	-type f \
	-name 'fridge-*.db' \
	-mtime "+${RETENTION_DAYS}" \
	-delete

echo "created backup: $backup_path"
BACKUP
sed -i "s|__DEFAULT_DB__|$DEFAULT_DB|g; s|__DEFAULT_BACKUP_DIR__|$DEFAULT_BACKUP_DIR|g" "$BACKUP_SCRIPT"
chown root:root "$BACKUP_SCRIPT"
chmod 0750 "$BACKUP_SCRIPT"

echo "installing $OFFSITE_SCRIPT"
cat >"$OFFSITE_SCRIPT" <<'OFFSITE'
#!/bin/sh
# Copy local backups to the offsite destination configured in the
# environment file. Files are only added, never deleted, so the
# destination keeps its own retention.
set -eu

BACKUP_DIR="${FRIDGE_BACKUP_DIR:-__DEFAULT_BACKUP_DIR__}"
DEST="${FRIDGE_OFFSITE_DEST:-}"
TOOL="${FRIDGE_OFFSITE_TOOL:-rsync}"

if [ "$DEST" = "" ]; then
	echo "FRIDGE_OFFSITE_DEST is not set; skipping offsite copy"
	exit 0
fi

case "$TOOL" in
	rsync)
		rsync -a --include='fridge-*.db' --exclude='*' "$BACKUP_DIR/" "${DEST%/}/"
		;;
	rclone)
		rclone copy --include='fridge-*.db' "$BACKUP_DIR" "$DEST"
		;;
	*)
		echo "unsupported FRIDGE_OFFSITE_TOOL: $TOOL (use rsync or rclone)" >&2
		exit 1
		;;
esac

echo "copied backups from $BACKUP_DIR to $DEST with $TOOL"
OFFSITE
sed -i "s|__DEFAULT_BACKUP_DIR__|$DEFAULT_BACKUP_DIR|g" "$OFFSITE_SCRIPT"
chown root:root "$OFFSITE_SCRIPT"
chmod 0750 "$OFFSITE_SCRIPT"

echo "installing systemd backup units"
backup_hour="03:20:00"
offsite_hour="04:20:00"
if [ "$SERVICE" = "fridge-preview" ]; then
	backup_hour="03:35:00"
	offsite_hour="04:35:00"
fi

cat >"/etc/systemd/system/$BACKUP_UNIT.service" <<UNIT
[Unit]
Description=Back up fridge SQLite database ($SERVICE)

[Service]
Type=oneshot
Environment=FRIDGE_DB=$DB_PATH
Environment=FRIDGE_BACKUP_DIR=$BACKUP_DIR
Environment=FRIDGE_BACKUP_RETENTION_DAYS=$BACKUP_RETENTION_DAYS
EnvironmentFile=-$ENV_FILE
ExecStart=$BACKUP_SCRIPT
UNIT

cat >"/etc/systemd/system/$BACKUP_UNIT.timer" <<UNIT
[Unit]
Description=Run fridge database backup daily

[Timer]
OnCalendar=*-*-* $backup_hour
Persistent=true
RandomizedDelaySec=15m

[Install]
WantedBy=timers.target
UNIT

cat >"/etc/systemd/system/$OFFSITE_UNIT.service" <<UNIT
[Unit]
Description=Copy fridge database backups offsite ($SERVICE)
Wants=network-online.target
After=network-online.target $BACKUP_UNIT.service

[Service]
Type=oneshot
Environment=FRIDGE_BACKUP_DIR=$BACKUP_DIR
EnvironmentFile=-$ENV_FILE
ExecStart=$OFFSITE_SCRIPT
UNIT

cat >"/etc/systemd/system/$OFFSITE_UNIT.timer" <<UNIT
[Unit]
Description=Copy fridge database backups offsite daily

[Timer]
OnCalendar=*-*-* $offsite_hour
Persistent=true
RandomizedDelaySec=15m

[Install]
WantedBy=timers.target
UNIT

echo "enabling backup and offsite timers"
systemctl daemon-reload
systemctl enable --now "$BACKUP_UNIT.timer" "$OFFSITE_UNIT.timer"

if [ -f "$DB_PATH" ]; then
	echo "running initial backup"
	systemctl start "$BACKUP_UNIT.service"
	journalctl -u "$BACKUP_UNIT.service" -n 5 --no-pager
else
	echo "database does not exist yet; timer enabled, first backup will run after data exists"
fi

echo "next scheduled backup and offsite copy:"
systemctl list-timers "$BACKUP_UNIT.timer" "$OFFSITE_UNIT.timer" --no-pager
echo "test the offsite copy with: systemctl start $OFFSITE_UNIT.service"
