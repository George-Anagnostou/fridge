#!/bin/sh
# Restore drill: copy the newest backup into a temporary directory, verify the
# copy with SQLite, and report the item count. The live database and the backup
# files are never modified.
#
# usage: restore-drill.sh [BACKUP_DIR]
set -eu

if [ "$#" -gt 1 ]; then
	echo "usage: $0 [BACKUP_DIR]" >&2
	exit 2
fi

if [ "$#" -eq 0 ] && [ -r /etc/fridge/fridge.env ]; then
	# shellcheck disable=SC1091
	. /etc/fridge/fridge.env
fi
BACKUP_DIR="${1:-${FRIDGE_BACKUP_DIR:-/var/backups/fridge}}"

latest=""
for file in "$BACKUP_DIR"/fridge-*.db; do
	if [ -f "$file" ]; then
		latest="$file"
	fi
done
if [ "$latest" = "" ]; then
	echo "no backups found in $BACKUP_DIR" >&2
	exit 1
fi

if [ -n "$(find "$latest" -mtime +1)" ]; then
	echo "warning: newest backup is more than two days old: $latest" >&2
fi

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/fridge-restore-drill.XXXXXX")"
trap 'rm -rf "$work_dir"' EXIT
restored="$work_dir/fridge.db"
cp "$latest" "$restored"

echo "backup:         $latest"
echo "restored to:    $restored"

integrity="$(sqlite3 -readonly "$restored" 'PRAGMA integrity_check;')"
if [ "$integrity" != "ok" ]; then
	echo "integrity_check failed: $integrity" >&2
	exit 1
fi
echo "integrity:      ok"
if sqlite3 -readonly "$restored" "SELECT 1 FROM sqlite_schema WHERE name='items' LIMIT 1;" | grep -q 1; then
	echo "item rows:      $(sqlite3 -readonly "$restored" 'SELECT count(*) FROM items;')"
else
	echo "item rows:      (no items table)"
fi
echo "restore drill passed"
