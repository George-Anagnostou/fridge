# Fridge revival plan

Plan only — this PR does not change app behavior or hosting. The default
branch is `main`. Merges to `main` do not auto-deploy. Production deploys stay
manual and need explicit approval.

## Current state

Go 1.23 + SQLite + stdlib `net/http` on `:8080`. An item is `name`,
`quantity`, `expiration_date`, and `last_changed`. Add, list, and remove work;
there is no edit, location, auth, or barcode lookup on `main` yet. Build and
test fixes: [PR #2](https://github.com/George-Anagnostou/fridge/pull/2).
Accounts and admin-only invites: [PR #3](https://github.com/George-Anagnostou/fridge/pull/3)
(stacked on #2). Pure-Go SQLite and CI: [PR #4](https://github.com/George-Anagnostou/fridge/pull/4)
(stacked on #3).

## Decisions

- **Stack:** keep the Go backend and SQLite. Use stdlib `net/http` and
  server-rendered HTML; no new web framework. Use `modernc.org/sqlite` (pure
  Go, CGO-free Linux builds). Receipt OCR and photo input are deferred. Do
  not use the official Open Food Facts Go SDK (API v0); call API v3 over raw
  HTTP.
- **Hosting:** a subdomain of a personal website (exact domain TBD at deploy
  time). Mirror bookmarks: nginx → `127.0.0.1:8082`, systemd, data in
  `/var/lib/fridge`, and deploy / rollback / backup scripts like bookmarks.
  HTTPS there is what makes phone camera scanning work.
- **Users:** a public multi-user app. Sign-up is invite-only during
  development; only an admin can issue sign-up invites. Each user can create
  fridge(s) and add items. Fridge-join invites are a separate member feature
  in the fridges PR.
- **Auth:** email + password in the accounts PR, then a small follow-on PR
  for Sign in with Google (OAuth 2.0 / OpenID Connect, via
  `golang.org/x/oauth2` or stdlib). Verify the ID token or userinfo and link
  by verified email. New Google users still need a valid invite.
  `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, and callback base URL come from
  env, are never committed, and the feature is disabled when unset.
  Server-side sessions (HTTP-only secure cookie, `sessions` table). Passwords
  hashed with argon2id. CSRF on form posts. Email verification, password
  reset, and remote CLI updates for auth are deferred.
- **Inventory lots:** buying the same item again always creates a new entry
  with its own expiration. Merging or splitting lots can be handled later in
  the UI.
- **Data:** items are scoped to a fridge. The product lookup cache is global
  (shared across users).
- **CI:** minimal GitHub Actions on push to `main` and on pull requests
  (gofmt, vet, `go test -race`, CGO-free build). No deploy steps.
- **Lookup chain:** cache → Open Food Facts API v3
  (`GET https://world.openfoodfacts.org/api/v3/product/{barcode}`) → USDA
  FoodData Central branded foods (`gtinUpc` exact match; `USDA_FDC_API_KEY`
  from env, never committed) → manual entry.
- **Open Food Facts:** User-Agent
  `Fridge/0.1 (https://github.com/George-Anagnostou/fridge)` plus optional
  contact from `OFF_CONTACT`. Rate limits: 15 product reads/min/IP, 10
  searches/min/IP. Credit OFF (ODbL) in the UI. Never use OFF's
  `expiration_date`.
- **Barcodes:** 1–7 digits left-pad to 8; 9–12 left-pad to 13; 8 or 13+
  unchanged. Cache hits 14 days (a single constant in the 7–30 range); misses
  24h; never cache 429/5xx. OFF and USDA are separate cache rows, keyed by the
  normalized barcode.
- **Expiry:** vendor a USDA FoodKeeper snapshot
  (`https://www.fsis.usda.gov/shared/data/EN/foodkeeper.json`, v128). Use
  location-specific shelf life (prefer `DOP_*`) and the conservative min of a
  range. "Package use-by date" or "Not Recommended" means no invented date.
  User override always wins. Manual category field (OFF categories are
  sparse); map to FoodKeeper via manual category, then USDA, then OFF.

## PR sequence

Each PR should be independently reviewable and mergeable to `main` without deploying.

1. **Fix local build, embed templates, and add SQLite tests** — [PR #2](https://github.com/George-Anagnostou/fridge/pull/2).
2. **Migrations, users, sessions, and invite-only sign-up** — [PR #3](https://github.com/George-Anagnostou/fridge/pull/3).
   Numbered migrations; email + password; CSRF; HTTP-only session cookie;
   admin-only single-use sign-up invites.
3. **Sign in with Google** — OAuth 2.0 / OIDC; link by verified email; still
   invite-only for new accounts; disabled when env vars are unset. Secrets
   never committed. Tests: link existing account; reject new email without an
   invite; off when credentials are missing.
4. **Fridges, membership, and item scoping** — `fridges` and membership
   tables; fridge-join invites; migrate existing rows into a default fridge.
   Tests: isolation; members can read/write; join invites are single-use.
5. **Inventory model: locations, category, edit, and use-soon** — location
   (`fridge` / `freezer` / `pantry`), manual category, barcode, brand,
   serving, nutrition, expiry source; edit; "use soon" (expired first, then
   within N days, default 3). Same item again is a new row. Tests: migrate,
   edit, filters.
6. **Mobile inventory UI** — phone-friendly CSS, location tabs, use-soon at
   the top, optional expiry. Typing only; no camera yet. Tests: form-post
   handlers for add, edit, remove, and filters.
7. **Barcode normalization and lookup cache** — normalize helper;
   `lookup_cache` with separate `off` / `usda` rows; TTLs as above. Optional
   cache-only `GET /lookup?barcode=`. Tests: padding, hit/miss/expiry, 429/5xx
   not cached.
8. **Open Food Facts API v3 client** — raw HTTP, rate-limited, cached;
   `OFF_CONTACT` and optional `OFF_BASE_URL` from env. ODbL credit in the UI.
   Ignore OFF expiry. Tests: `httptest` for 200, 404, and 429 (not cached);
   no live OFF calls in CI.
9. **USDA FoodData Central and FoodKeeper defaults** — exact `gtinUpc` match;
   fill nutrition gaps; vendor the FoodKeeper snapshot. Suggest expiry from
   location and date of purchase; user-overridable. Tests: exact match, date
   math, manual category wins; no live USDA calls in CI.
10. **In-page barcode scanning** — vendored html5-qrcode (not
    `BarcodeDetector`, off by default in iOS Safari). Camera needs HTTPS or
    localhost. Scan → `/lookup` → prefill the add form; manual entry always
    available. Tests: the same prefill path for a typed barcode; document
    camera QA on iOS/Android over HTTPS or localhost.
11. **Deploy to the subdomain** — approval-gated, manual, no deploy-on-push.
    nginx → `127.0.0.1:8082`, systemd, `/var/lib/fridge`, and deploy /
    rollback / backup scripts like bookmarks. Env vars only on the host.
    Exact domain TBD. Do not add deploy steps to CI on `main`.

Pure-Go SQLite and CI are in [PR #4](https://github.com/George-Anagnostou/fridge/pull/4)
(stacked on #3), not a separate numbered step.

## Open questions

- **Multiple fridges and roles:** may a user own more than one fridge (default yes)?
  Roles: owner + member (default).
