# Fridge revival plan

Plan only — this PR does not change app behavior or hosting. Merges to `master`
do not auto-deploy. Production deploys stay manual and need explicit approval.

## Current state

Go 1.23 + SQLite (`go-sqlite3`) + stdlib `net/http` on `:8080`. An item is
`name`, `quantity`, `expiration_date`, and `last_changed`. Add, list, and
remove work; there is no edit, location, auth, or barcode lookup, and no CI or
deploy config. The Makefile was broken, templates were cwd-dependent, and some
handlers could deadlock. Those build and test fixes are in
[PR #2](https://github.com/George-Anagnostou/fridge/pull/2).

## Decisions

- **Stack:** keep the Go backend and SQLite. Use stdlib `net/http` and
  server-rendered HTML; no new web framework. Receipt OCR and photo input are
  deferred. Do not use the official Open Food Facts Go SDK (API v0); call API
  v3 over raw HTTP.
- **Hosting:** a subdomain of a personal website (exact domain TBD at deploy
  time). HTTPS there is what makes phone camera scanning work.
- **Users:** a public multi-user app. Sign-up is invite-only during
  development. Each user can create fridge(s), add items to them, and invite
  other users to share a fridge (membership).
- **Auth:** email + password by default (see open questions). Server-side
  sessions (HTTP-only secure cookie, `sessions` table). Passwords hashed with
  bcrypt or argon2id. CSRF protection on form posts. Single-use, expiring
  invite tokens for both sign-up and joining a fridge.
- **Data:** items are scoped to a fridge. The product lookup cache is global
  (shared across users).
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

Each PR should be independently reviewable and mergeable to `master` without
deploying.

1. **Fix local build, embed templates, and add SQLite tests** — done in
   [PR #2](https://github.com/George-Anagnostou/fridge/pull/2).
2. **Migrations, users, sessions, and invite-only sign-up**
   - Numbered SQLite migrations; `users` and `sessions` tables; register,
     login, logout; password hashing; CSRF; HTTP-only secure session cookie.
   - Sign-up requires a single-use, expiring invite token.
   - Tests: migrate a fresh DB and an original-schema DB; reject
     unauthenticated writes; expired or reused invites fail.
3. **Fridges, membership, and item scoping**
   - `fridges` and membership tables; invite tokens to join a fridge.
   - All items scoped to a fridge; migrate existing rows into a default
     fridge.
   - Tests: isolation between fridges; members can read/write; non-members
     cannot; join invites are single-use and expiring.
4. **Inventory model: locations, category, edit, and use-soon**
   - Additive columns: location (`fridge` / `freezer` / `pantry`), manual
     category, barcode, brand, serving, nutrition, expiry source.
   - Edit an item; filter by location and "use soon" (expired first, then
     within N days, default 3).
   - Tests: migrate the prior schema; edit; location and use-soon filters.
5. **Mobile inventory UI**
   - Phone-friendly CSS, location tabs, use-soon at the top, optional expiry.
     Typing only; no camera yet.
   - Tests: form-post handlers for add, edit, remove, and filters.
6. **Barcode normalization and lookup cache**
   - Normalize helper; `lookup_cache` with separate `off` / `usda` rows; TTLs
     as above. Optional cache-only `GET /lookup?barcode=`.
   - Tests: padding table; hit/miss/expiry; 429/5xx not cached.
7. **Open Food Facts API v3 client**
   - Raw HTTP, rate-limited, cached; `OFF_CONTACT` and optional `OFF_BASE_URL`
     from env. `GET /lookup` may call OFF on a cold cache. ODbL credit in the
     UI. Ignore OFF expiry fields.
   - Tests: `httptest` for 200, 404, and 429 (not cached), plus field mapping.
     No live OFF calls in CI.
8. **USDA FoodData Central and FoodKeeper defaults**
   - Exact `gtinUpc` match; fill nutrition gaps; vendor the FoodKeeper
     snapshot. Suggest expiry from location and date of purchase;
     user-overridable.
   - Tests: exact match, date math, manual category wins, OFF expiry never
     read. No live USDA calls in CI.
9. **In-page barcode scanning**
   - Vendored html5-qrcode (not `BarcodeDetector`, which is off by default in
     iOS Safari). Camera needs HTTPS or localhost. Scan → `/lookup` → prefill
     the add form; manual barcode entry always available.
   - Tests: the same prefill path for a typed barcode; document camera QA on
     iOS/Android over HTTPS or localhost.
10. **Deploy to the subdomain** — approval-gated, manual, no deploy-on-push.
    - Dockerfile, persistent volume for `fridge.db`, HTTPS, env vars only on
      the host (`USDA_FDC_API_KEY`, `FRIDGE_DB`, `PORT`, session secrets,
      `OFF_CONTACT`). Exact domain TBD at deploy time. Do not add `on: push`
      to `master`.
    - Tests: image builds; server starts with a volume; `/` is 200 over the
      intended URL.

## Open questions

- **Auth method:** email + password (default) vs magic-link email vs OAuth.
- **Multiple fridges and roles:** may a user own more than one fridge (default
  yes)? Roles: owner + member (default).
- **Same barcode again:** new lot/row (default, so expiries stay distinct) vs
  merge quantities.
- **Who can issue sign-up invites:** any existing user, or admin only?
