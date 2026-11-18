#!/bin/sh
# Render preview from the production template; keep headers identical.
set -eu
root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
case "${1:-https}" in
	http) template="$root/deploy/nginx/fridge-http.conf" ;;
	https) template="$root/deploy/nginx/fridge.conf" ;;
	*) echo "usage: $0 [http|https]" >&2; exit 1 ;;
esac
sed \
	-e 's/fridge\.georgeanagnostou\.com/preview.fridge.georgeanagnostou.com/g' \
	-e 's|127\.0\.0\.1:8082|127.0.0.1:8083|g' \
	-e 's|/var/www/fridge|/var/www/fridge-preview|g' \
	-e 's|/etc/fridge/fridge.env|/etc/fridge/fridge-preview.env|g' \
	-e 's/shared:fridge_ssl/shared:fridge_preview_ssl/g' \
	"$template"
