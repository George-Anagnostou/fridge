#!/bin/sh
# One-time Debian VPS bootstrap for production and preview. Run as root.
# This script does not deploy a binary or write secrets. See docs/DEPLOY.md.
set -eu

if [ "$(id -u)" -ne 0 ]; then
	echo "run this script as root on the Debian VPS" >&2
	exit 1
fi

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"
REPO_ROOT="$(CDPATH='' cd -- "$SCRIPT_DIR/.." && pwd)"
PROD_DOMAIN="${FRIDGE_DOMAIN:-fridge.georgeanagnostou.com}"
PREVIEW_DOMAIN="${FRIDGE_PREVIEW_DOMAIN:-preview.fridge.georgeanagnostou.com}"

echo "installing Debian packages"
apt-get update
apt-get install -y nginx sqlite3 rsync certbot

echo "creating service and deploy users"
if ! getent passwd fridge >/dev/null; then
	useradd --system --home /var/lib/fridge --shell /usr/sbin/nologin fridge
fi
for deploy_user in fridge-deploy fridge-preview; do
	if ! getent passwd "$deploy_user" >/dev/null; then
		useradd --create-home --shell /bin/bash "$deploy_user"
	fi
	install -d -o "$deploy_user" -g "$deploy_user" -m 0700 "/home/$deploy_user/.ssh"
	auth="/home/$deploy_user/.ssh/authorized_keys"
	if [ ! -e "$auth" ]; then
		umask 077
		touch "$auth"
		chown "$deploy_user:$deploy_user" "$auth"
		chmod 0600 "$auth"
	fi
done

echo "creating directories"
install -d -o fridge -g fridge -m 0750 /var/lib/fridge /var/lib/fridge-preview
install -d -o root -g root -m 0750 /etc/fridge /var/backups/fridge /var/backups/fridge-preview
install -d -o root -g root -m 0755 /var/www/fridge/acme /var/www/fridge-preview/acme

install_env() {
	src="$1"
	dest="$2"
	if [ -e "$dest" ]; then
		echo "leaving existing $dest in place"
		return 0
	fi
	install -o root -g root -m 0600 "$src" "$dest"
	echo "installed example $dest (edit before the first start)"
}
install_env "$REPO_ROOT/deploy/fridge.env.example" /etc/fridge/fridge.env
install_env "$REPO_ROOT/deploy/fridge-preview.env.example" /etc/fridge/fridge-preview.env

echo "installing systemd units"
install -o root -g root -m 0644 "$REPO_ROOT/deploy/systemd/fridge.service" /etc/systemd/system/fridge.service
install -o root -g root -m 0644 "$REPO_ROOT/deploy/systemd/fridge-preview.service" /etc/systemd/system/fridge-preview.service

echo "installing fridge-switch helper"
install -o root -g root -m 0750 "$SCRIPT_DIR/fridge-switch.sh" /usr/local/sbin/fridge-switch

echo "installing sudoers rule"
tmp_sudoers="$(mktemp)"
trap 'rm -f "$tmp_sudoers"' EXIT
install -m 0440 "$REPO_ROOT/deploy/sudoers" "$tmp_sudoers"
if ! visudo -c -f "$tmp_sudoers"; then
	echo "sudoers file failed validation; not installing" >&2
	exit 1
fi
install -o root -g root -m 0440 "$tmp_sudoers" /etc/sudoers.d/fridge
chmod 0440 /etc/sudoers.d/fridge

echo "installing backup timers"
sh "$SCRIPT_DIR/install-fridge-backup.sh" fridge
sh "$SCRIPT_DIR/install-fridge-backup.sh" fridge-preview

install_nginx_site() {
	name="$1"
	src="$2"
	dest="/etc/nginx/sites-available/$name"
	if [ -e "$dest" ]; then
		echo "leaving existing nginx site $dest in place"
	else
		install -o root -g root -m 0644 "$src" "$dest"
	fi
	ln -sf "$dest" "/etc/nginx/sites-enabled/$name"
}

echo "installing bootstrap nginx sites (HTTP only, for certificate issuance)"
install_nginx_site "$PROD_DOMAIN" "$REPO_ROOT/deploy/nginx/fridge-http.conf"
sh "$SCRIPT_DIR/preview-nginx.sh" http > /tmp/fridge-preview-http.conf
install_nginx_site "$PREVIEW_DOMAIN" /tmp/fridge-preview-http.conf
rm -f /tmp/fridge-preview-http.conf
nginx -t
systemctl reload nginx || systemctl restart nginx

issue_cert() {
	domain="$1"
	webroot="$2"
	cert_path="/etc/letsencrypt/live/$domain/fullchain.pem"
	if [ -f "$cert_path" ]; then
		echo "certificate already exists for $domain"
		return 0
	fi
	if ! certbot certonly --non-interactive --webroot \
		--webroot-path "$webroot" \
		--cert-name "$domain" -d "$domain" \
		--deploy-hook "nginx -t && systemctl reload nginx"; then
		echo "certbot could not issue a certificate for $domain." >&2
		echo "Leave the HTTP bootstrap site in place, fix DNS or port 80, then rerun bootstrap or certbot." >&2
		return 1
	fi
	return 0
}

certs_ok=0
if issue_cert "$PROD_DOMAIN" /var/www/fridge/acme; then
	certs_ok=$((certs_ok + 1))
	echo "installing HTTPS nginx site for $PROD_DOMAIN"
	if [ -f "/etc/nginx/sites-available/$PROD_DOMAIN" ]; then
		cp "/etc/nginx/sites-available/$PROD_DOMAIN" "/etc/nginx/sites-available/$PROD_DOMAIN.previous"
	fi
	install -o root -g root -m 0644 "$REPO_ROOT/deploy/nginx/fridge.conf" "/etc/nginx/sites-available/$PROD_DOMAIN"
fi
if issue_cert "$PREVIEW_DOMAIN" /var/www/fridge-preview/acme; then
	certs_ok=$((certs_ok + 1))
	echo "installing HTTPS nginx site for $PREVIEW_DOMAIN"
	if [ -f "/etc/nginx/sites-available/$PREVIEW_DOMAIN" ]; then
		cp "/etc/nginx/sites-available/$PREVIEW_DOMAIN" "/etc/nginx/sites-available/$PREVIEW_DOMAIN.previous"
	fi
	sh "$SCRIPT_DIR/preview-nginx.sh" https > /tmp/fridge-preview-https.conf
	install -o root -g root -m 0644 /tmp/fridge-preview-https.conf "/etc/nginx/sites-available/$PREVIEW_DOMAIN"
	rm -f /tmp/fridge-preview-https.conf
fi

if ! nginx -t; then
	echo "nginx -t failed; restoring previous site files if present" >&2
	for domain in "$PROD_DOMAIN" "$PREVIEW_DOMAIN"; do
		if [ -f "/etc/nginx/sites-available/$domain.previous" ]; then
			cp "/etc/nginx/sites-available/$domain.previous" "/etc/nginx/sites-available/$domain"
		fi
	done
	nginx -t
	exit 1
fi
systemctl reload nginx || systemctl restart nginx

echo "reloading systemd and enabling app units"
systemctl daemon-reload
systemctl enable fridge fridge-preview
if [ -x /usr/local/bin/fridge-server ]; then
	systemctl start fridge || echo "fridge failed to start; deploy a binary and inspect journalctl -u fridge"
fi
if [ -x /usr/local/bin/fridge-preview-server ]; then
	systemctl start fridge-preview || echo "fridge-preview failed to start; deploy a binary and inspect journalctl -u fridge-preview"
fi

echo
echo "bootstrap complete"
echo "Next:"
echo "  1. Add the GitHub environment SSH public keys to /home/fridge-deploy/.ssh/authorized_keys and /home/fridge-preview/.ssh/authorized_keys."
echo "  2. Edit /etc/fridge/fridge.env and /etc/fridge/fridge-preview.env (leave unused API keys blank)."
echo "  3. Deploy a binary (push to main/preview after CI is configured, or make update)."
echo "  4. Create the first admin invite on the server, for example:"
echo "       set -a && . /etc/fridge/fridge.env && set +a"
echo "       /usr/local/bin/fridge-invite -admin -base-url https://$PROD_DOMAIN"
if [ "$certs_ok" -lt 2 ]; then
	echo "  5. Finish any missing certificates, then replace the HTTP bootstrap site with the HTTPS template."
fi
