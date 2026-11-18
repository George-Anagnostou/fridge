# Deploy

The fridge app runs on the same VPS as bookmarks and mealsplit: a CGO-free
linux/amd64 Go binary behind nginx, with systemd, SQLite, and local backups
(plus an optional offsite copy).

```text
internet → nginx :443 (fridge.georgeanagnostou.com) → 127.0.0.1:8082 → SQLite
internet → nginx :443 (preview.fridge.georgeanagnostou.com) → 127.0.0.1:8083 → SQLite
```

The VPS architecture is assumed to be linux/amd64, the same default bookmarks
uses when it cross-compiles `bookmarkd`. Confirm with `uname -m` on the VPS
before the first deploy (`x86_64` is amd64).

Sign-up stays invite-only: there is no public registration. Pages send
`noindex` (`<meta name="robots">` and nginx `X-Robots-Tag`). The first account
must be created with an admin invite on the server after the first deploy.

| Path | Purpose |
|------|---------|
| `/usr/local/bin/fridge-server` | Production binary (preview: `fridge-preview-server`) |
| `/usr/local/bin/fridge-invite` | Invite CLI for that environment |
| `/usr/local/bin/fridge-server.previous` | Previous binary, used by rollback |
| `/etc/fridge/fridge.env` | Production config (mode 0600; preview: `fridge-preview.env`) |
| `/var/lib/fridge/fridge.db` | Production database (preview: `/var/lib/fridge-preview`) |
| `/var/backups/fridge/` | Daily and pre-deploy backups |
| `/usr/local/sbin/fridge-switch` | Install/rollback helper; the only command deploy users can sudo |
| `/etc/nginx/sites-available/fridge.georgeanagnostou.com` | nginx site |

Pushes to `main` deploy to production through the GitHub `production` environment,
which waits for a required reviewer. Pushes to `preview` deploy to preview immediately.
Pull requests run checks only; they never get deployment credentials.

## Checklist (one-time)

Do these before the first automatic deploy. DNS for both hostnames is already in place.

1. **Confirm the VPS is linux/amd64** (`uname -m` prints `x86_64`).
2. **Create GitHub environments**
   - `production`: allowed only from `main`, **required reviewers** enabled.
   - `preview`: allowed only from `preview`, no required reviewers.
3. **Add environment secrets and variables** (separate values per environment):

   | Name | Kind | Purpose |
   |------|------|---------|
   | `DEPLOY_SSH_KEY` | Secret | Private key for that environment's deploy user |
   | `DEPLOY_KNOWN_HOSTS` | Secret | VPS host key line(s) |
   | `DEPLOY_HOST` | Variable | VPS IP or hostname |
   | `DEPLOY_USER` | Variable | `fridge-deploy` (production) or `fridge-preview` (preview) |
   | `DEPLOY_PATH` | Variable | `/usr/local/bin` |
   | `VERIFY_URL` | Variable | `https://fridge.georgeanagnostou.com` or `https://preview.fridge.georgeanagnostou.com` |

   Generate new keys for these accounts; do not reuse a personal SSH key. Production
   `VERIFY_URL` must be exactly `https://fridge.georgeanagnostou.com`.
4. **Run bootstrap on the VPS as root**, from a checkout of this repository:

   ```sh
   sudo sh scripts/bootstrap-debian.sh
   ```

   This creates the `fridge` service user, `fridge-deploy` and `fridge-preview`
   SSH users, directories, systemd units, backup timers, the narrow sudoers
   rule, HTTP nginx sites, and (when port 80 and DNS work) Let's Encrypt
   certificates plus the HTTPS sites. It does not start the app until a binary
   has been deployed, and it does not write API keys.
5. **Install the GitHub public keys** on the VPS:

   ```text
   /home/fridge-deploy/.ssh/authorized_keys
   /home/fridge-preview/.ssh/authorized_keys
   ```
6. **Edit the environment files** `/etc/fridge/fridge.env` and
   `/etc/fridge/fridge-preview.env`. Examples live in `deploy/`. Keep
   `COOKIE_SECURE=1` and `TRUST_PROXY=1`. Leave `OFF_CONTACT` and
   `USDA_FDC_API_KEY` blank until those values are ready. Optional offsite
   copies use `FRIDGE_OFFSITE_DEST` (and `FRIDGE_OFFSITE_TOOL=rsync` or `rclone`).
7. **Deploy a binary** (merge to `main`/`preview` once CI is configured, or
   `make update` from a machine that can SSH to the VPS). Then **create the
   first admin invite on the server**:

   ```sh
   set -a && . /etc/fridge/fridge.env && set +a
   /usr/local/bin/fridge-invite -admin -base-url https://fridge.georgeanagnostou.com
   ```

   Open the printed sign-up link, create the first account, and keep later
   sign-up limited to invites from an admin or a store owner.
8. **Create the `preview` branch** once this work is on `main`:

   ```sh
   git fetch origin
   git switch -c preview origin/main
   git push -u origin preview
   ```

## Routine deploys and rollback

Automatic: merge to `preview` or `main` and wait for the Actions deploy job
(production waits for the environment approval). GitHub serializes each branch
and skips superseded commits. A failed `/healthz` check (including a commit SHA
mismatch) automatically rolls back to `.previous` and the pre-deploy database.

Manual (from a checkout that can SSH to the VPS, with `.env.deploy` filled in
from `.env.deploy.example`):

```sh
make update      # checks, builds linux/amd64 binaries, deploys, verifies /healthz
make rollback    # restore the previous binary and pre-deploy database
make verify      # check public /healthz without deploying
```

`FRIDGE_DEPLOY_SERVICE` is `fridge` or `fridge-preview`.

## Restore drill

On the VPS, after backups exist:

```sh
sudo sh scripts/restore-drill.sh /var/backups/fridge
```

This copies the newest backup to a temp directory and checks integrity; it does
not touch the live database.

## Cloudflare

The nginx sites set `X-Real-IP` to the TCP peer (`$remote_addr`). The app
trusts that header only when the direct peer is loopback (`TRUST_PROXY=1`).
If the hostname is later proxied through Cloudflare, that peer will be
Cloudflare, not the client. A commented note in `deploy/nginx/fridge.conf`
describes the `CF-Connecting-IP` change; do not apply it until proxying is
confirmed.

## Testing the deploy files

```sh
make test-deploy   # shellcheck (if installed), syntax, local-mode tests (no SSH)
```

CI runs these tests and `nginx -t` on the production and preview configs
(using throwaway certificates) before any automatic deployment. The deploy
job is skipped on pull requests.
