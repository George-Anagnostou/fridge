# Fridge revival plan

Personal inventory app for George: after a grocery trip, add items quickly from a phone and see what's in the fridge, freezer, and pantry — with nutrition facts and expiration dates.

This document is a plan only. It does not implement features, change hosting, or set up auto-deploy. Nothing should reach a production host without George's explicit approval.

Default branch is `master`. There is no `main`.

---

## 1. Current state (as of 2024-10, last commit 2024-09-01)

Repo: [George-Anagnostou/fridge](https://github.com/George-Anagnostou/fridge). Public. Description: "Is your refrigerator running?" No open issues or PRs. No `.github/` workflows, Dockerfile, `fly.toml`, Railway/Procfile, or other deploy config. **Merges to `master` do not auto-deploy** — there is nothing to deploy to.

### Stack

| Layer | What it is today |
| --- | --- |
| Language | Go 1.23 (`go.mod`); builds cleanly with Go 1.27.3 |
| Library | `fridge/` — `Fridge` + `Item`, SQLite via `github.com/mattn/go-sqlite3` (CGO) |
| HTTP | stdlib `net/http` in `cmd/http`, listens on `:8080` |
| HTML | one unstyled template: `cmd/http/templates/index.html` |
| Local CLI | `cmd/cli/localCLI` (stdlib `flag`) talks to `./fridge.db` directly |
| Remote CLI | `cmd/cli/remoteCLI` (`urfave/cli/v2`) talks to `http://localhost:8080` |
| Auth | none |
| Tests | none (`go test ./...` is a no-op success) |
| CI | none |

`go build ./...` and `go vet ./...` succeed when a C compiler is present (gcc is required for `go-sqlite3`).

### Data model and storage

SQLite file hardcoded to `./fridge.db`. Schema is created in `InitDB` with `CREATE TABLE IF NOT EXISTS` (no migrations, no version table):

```sql
CREATE TABLE IF NOT EXISTS fridge (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    quantity NUMBER NOT NULL,
    expiration_date DATETIME,
    last_changed DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

A trigger (`update_last_changed`) bumps `last_changed` on UPDATE. There is no location, category, barcode, brand, serving size, or nutrition. An item is just name + quantity + expiration. Adding the same product twice creates two rows (which is reasonable for separate lots with different dates).

### What the web app can do

- `GET /` — list items (name, quantity, expires on YYYY-MM-DD) and a form to add one. Expiration is required. Viewport meta tag is present; there is no CSS.
- `POST /` — add item from the form.
- `POST /remove` — delete by id, then **303 See Other** to `/`.
- `GET /items` — JSON list.
- `POST /items` — JSON add.

No edit/update. No "use soon" view. No fridge/freezer/pantry. No barcode path.

A smoke test from a directory that contains `cmd/http/templates/` confirmed add, list, and remove work end-to-end.

### What is broken or fragile

- **Makefile is unusable as written.** `CLI_LOCAL_SRC` and `CLI_REMOTE_SRC` are swapped, and both targets pass a single `main.go` file to `go build`, so `Run` / `PrintUsage` are undefined. Correct incantation is `go build -o <out> ./cmd/cli/localCLI` (package path, not a file). This environment also does not have `make` installed; that is a machine issue, not a repo issue.
- **Template path is cwd-dependent.** `init()` does `template.Must(template.ParseGlob("cmd/http/templates/*.html"))`. Running the binary from any other directory panics: `pattern matches no files`. The Makefile-built `build/fridge-server` would hit this if launched from `build/`.
- **Mutex deadlocks.** `listAndAddHandler` and `postItemsHandler` call `myFridge.Lock()` and `return` on add/lookup errors without `Unlock()`. The in-memory `Fridge.Items` slice is unused; SQLite is the source of truth, so the mutex is the wrong concurrency tool anyway.
- **`POST /remove` always 303s.** The remote CLI treats only HTTP 200 as success, so `fridge-remote remove` fails even when the item was deleted. `removeHandler` also continues after a bad id (Atoi error is logged, not returned).
- **Empty JSON list is `null`**, not `[]`, because `ListItems` returns a nil slice.
- **Remote CLI `add`** declares quantity as `IntFlag` but reads it with `c.Float64("quantity")`.
- Typos only: `createTrigerSQL`, "expiraiton", "refridgerator".

---

## 2. What to keep, what to replace

### Keep

- The Go backend and module path `github.com/George-Anagnostou/fridge`.
- SQLite as the datastore (`go-sqlite3` is fine; CGO just means deploy images need a C compiler or a pre-built binary).
- The existing item identity (`id`) and fields: `name`, `quantity`, `expiration_date`, `last_changed`.
- Treating each add as its own lot (same name/barcode can appear more than once with different expiry). Confirm with George; see open questions.
- stdlib `net/http` and HTML templates. No new web framework.
- The local CLI as a debug tool, once the Makefile is fixed. Do not spend early PRs expanding it.
- `*.db` already gitignored.

### Replace or fix (do not grow on top of)

- The Makefile (swap, package-path builds).
- Cwd-relative template loading — use `embed.FS`.
- Hardcoded `./fridge.db` — `FRIDGE_DB` env (default `./fridge.db`) so tests and deploy can point at a file.
- `CREATE TABLE IF NOT EXISTS` as the only schema story — add numbered migrations. Existing DBs must keep working.
- The `Fridge` mutex-around-handlers pattern — `defer` at minimum; prefer dropping the mutex and letting SQLite serialize writes (`_busy_timeout`, maybe WAL later).
- The unstyled single-page form — keep the server-rendered approach, restyle and extend it for a phone.
- Required expiration in the add form — become optional, defaulted from FoodKeeper, user-overridable.
- Remote CLI bugs only if we touch that code; it is not on the daily-use path.

### Do not add (deferred)

- Receipt OCR.
- Photo input (other than the live barcode camera).
- The official Open Food Facts Go SDK (it still talks to API v0). Use raw `net/http` against API v3.
- Auto-deploy from `master`. George's rule: nothing goes to production without his explicit approval. There is currently no production host and no deploy pipeline; keep it that way until he signs off on a later deploy PR.

---

## 3. Product decisions already made

These are locked for implementation unless George changes them.

**Daily flow.** Phone browser, after grocery shopping: scan or type a barcode, confirm the prefilled product, set quantity and location, accept or override the expiry, save. Inventory view shows locations, nutrition, and a "use soon" list.

**Barcode scanning.** Phone-friendly page uses [html5-qrcode](https://github.com/mebjas/html5-qrcode). Do not rely on `BarcodeDetector` — it is off by default in iOS Safari. Camera access requires **HTTPS or localhost**.

**Lookup chain** (stop at the first complete-enough hit, still allow USDA to fill gaps):

1. SQLite cache (OFF and USDA stored as **separate records**).
2. Open Food Facts API v3 over raw HTTP: `GET https://world.openfoodfacts.org/api/v3/product/{barcode}`. Request only needed `fields`. Descriptive `User-Agent` of the form `Fridge/0.1 (https://github.com/George-Anagnostou/fridge; email)`. Rate limits: **15 product reads/min/IP** and **10 searches/min/IP** — enforce in-process. Credit OFF in the UI (ODbL). **Never use OFF's `expiration_date` field.**
3. USDA FoodData Central, branded foods only, match `gtinUpc` **exactly**. Free API key from env (`USDA_FDC_API_KEY`), never committed.
4. Manual entry.

**Barcode normalization** before every cache key and API call:

- 1–7 digits → left-pad to 8 (EAN-8 / UPC-E length).
- 9–12 digits → left-pad to 13 (EAN-13 / UPC-A with number system).
- 8 or 13+ digits → leave as-is.

**Cache TTLs.** Hits: 7–30 days (implement as a single constant, default **14 days**, easy to change). Misses (confirmed 404 / not found): **24 hours**. **Never cache 429 or 5xx.**

**Expiry estimates.** USDA FoodKeeper JSON: `https://www.fsis.usda.gov/shared/data/EN/foodkeeper.json` (current file is FMA-Data v128, last modified 2018-09-06). Vendor a snapshot in the repo. Pick the shelf-life field that matches the item's **location** (pantry / refrigerate / freeze; prefer date-of-purchase `DOP_*` when present). Use the conservative end of a min–max range. If FoodKeeper says "Package use-by date" or "Not Recommended", do not invent a date — leave expiry blank and let George type it. User override always wins; store that it was overridden.

**Categories.** US OFF fill rates are uneven (name 98%, nutrition 90%, kcal 86%, brand 69%, serving 66%, categories **44%**, quantity 22%). Add a **manual category** field. When mapping to FoodKeeper, use manual category first, then USDA branded-food description/category, then OFF categories if present.

---

## 4. Ordered, PR-sized steps

Each PR should be independently reviewable and mergeable to `master` without going to a production host. Test with `go test ./...` (and handler tests where noted). Do not start these until this plan PR is accepted.

### PR 1 — Make it build, run locally, and have tests

**Title:** Fix local build, embed templates, and add SQLite tests

**Why first:** The library already compiles, but the Makefile is wrong, the server panics off repo-root, handlers can deadlock, and there are zero tests to hang later work on.

**Files touched:**

- `Makefile` — point each target at the package (`./cmd/cli/localCLI`, `./cmd/cli/remoteCLI`, `./cmd/http`), unswap names.
- `README.md` — how to run: `go run ./cmd/http` (or the built binary) from any directory; `FRIDGE_DB`; `go test ./...`.
- `fridge/database.go` — `InitDB(path string)`; honor `FRIDGE_DB` at the call sites; set a busy timeout.
- `fridge/fridge.go` — `ListItems` returns an empty slice, not nil; keep current CRUD signatures unless a small cleanup helps tests.
- `fridge/fridge_test.go` — **new**: temp DB, add / get / list / remove.
- `cmd/http/main.go` — `embed.FS` for templates; configurable listen addr (`PORT`, default `8080`); `defer Unlock` or drop the mutex; return after parse errors in `removeHandler`; JSON `[]` for no items.
- `cmd/cli/localCLI/*`, `cmd/cli/remoteCLI/*` — only as needed to pass the DB path and to treat `POST /remove` 303 as success (or change remove to 303 + JSON for XHR and 303 for forms consistently; document it).

**Schema:** no change.

**Endpoints:** same paths; behavior fixes only.

**Tests:** table-driven CRUD against a temp SQLite file; a small `httptest` check that `GET /` is 200 without a special cwd; remove of a missing/invalid id does not hang the server.

---

### PR 2 — Migrations and the daily inventory model

**Title:** Add SQLite migrations, locations, categories, and item edits

**Files touched:** `fridge/database.go`, new `fridge/migrate.go` + `fridge/migrate_test.go`, `fridge/fridge.go` + tests, `cmd/http/main.go`, `cmd/http/templates/index.html`.

**Schema changes** (additive, existing rows kept):

- `schema_migrations (id INTEGER PRIMARY KEY, applied_at TEXT)` or equivalent.
- `fridge` columns:
  - `location TEXT NOT NULL DEFAULT 'fridge'` — check/enum in Go: `fridge`, `freezer`, `pantry`.
  - `category TEXT` — manual, free or a short FoodKeeper-aligned list.
  - `barcode TEXT` — normalized digits, nullable.
  - `brand TEXT`
  - `serving_size TEXT`
  - nutrition: either a `nutrition_json TEXT` blob or simple columns (`kcal`, `protein_g`, `fat_g`, `carb_g`, `fiber_g`, `sugar_g`, `sodium_mg`). Prefer columns for the facts the UI will show; leftover detail can stay in JSON.
  - `nutrition_source TEXT` — `off`, `usda`, `manual`, empty.
  - `expiration_date` remains nullable (stop requiring it).
  - `expiration_source TEXT` — `user`, `foodkeeper`, empty.
  - `foodkeeper_product_id INTEGER` — nullable, for later defaulting.

**Endpoints:**

- Keep `GET /`, `POST /` (add), `POST /remove`, `GET /items`, `POST /items`.
- Add `POST /items/{id}` (or `POST /edit`) to update name, quantity, location, category, expiration (and later nutrition).
- List filters: `GET /?location=fridge|freezer|pantry` and `GET /?soon=1` (expired, or expiring within N days; default N=3, constant).

**Tests:** migrate a DB that only has the original 5 columns; edit an item; filter by location; "use soon" ordering (expired first, then soonest). Quantity may decrease to zero — decide in this PR: allow 0 or treat 0 as remove; default to allow 0 and show it, unless George prefers auto-delete.

---

### PR 3 — Phone-friendly inventory UI

**Title:** Mobile inventory UI with locations and a use-soon view

**Files touched:** `cmd/http/templates/` (layout + list + add/edit forms), new `cmd/http/static/` (CSS, maybe a little JS for progressive enhancement), `cmd/http/main.go` (embed and serve static), handler tests.

**Schema / endpoints:** none new, besides rendering the PR 2 filters as tabs/sections. Large tap targets, readable type, one-handed add form, location selector, "Use soon" at the top, remaining items grouped by location. Expiration optional. No camera yet. No auth yet.

**Tests:** handler tests for add / edit / remove / location tab / use-soon through the HTML form posts; visually check on a narrow viewport (document in the PR). This is the first version George can actually use daily by typing items.

---

### PR 4 — Barcode normalization and lookup cache

**Title:** Normalize barcodes and add a SQLite lookup cache

**Files touched:** new `fridge/barcode.go` + `barcode_test.go`, `fridge/cache.go` + `cache_test.go`, `fridge/migrate.go` (new tables), maybe a thin `GET /lookup?barcode=` that only hits the cache (live clients come in PRs 5–6).

**Schema:**

```sql
CREATE TABLE lookup_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    barcode TEXT NOT NULL,          -- normalized
    source TEXT NOT NULL,           -- 'off' | 'usda'
    status TEXT NOT NULL,           -- 'hit' | 'miss'
    http_status INTEGER,
    payload TEXT,                   -- raw JSON on hit; null on miss
    fetched_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    UNIQUE (barcode, source)
);
```

OFF and USDA are separate rows for the same barcode. Reads that return 429 or 5xx are not written. Hits expire in 14 days; misses in 24 hours. Cache key is the **normalized** barcode.

**Endpoints:** optional `GET /lookup?barcode=` returning `{normalized, off, usda}` from cache only (no network), so the cache is usable and testable before API keys exist.

**Tests:** padding table (1–7 → 8, 9–12 → 13, 8 and 13 unchanged, leading zeros preserved as strings); cache hit/miss/expiry with a fake clock; 429/500 must not insert a row; unique `(barcode, source)`.

---

### PR 5 — Open Food Facts API v3 client

**Title:** Look up products from Open Food Facts v3 (cached, rate-limited)

**Files touched:** new `fridge/off.go` + `off_test.go`, `fridge/lookup.go` (chain start), `cmd/http/main.go` (`GET /lookup` now may call OFF on a cold cache), templates (ODbL credit in the footer/about line), README (User-Agent / credit).

**Schema:** none (uses `lookup_cache` source=`off`).

**Endpoints:** `GET /lookup?barcode=` — normalize, check OFF cache, on miss or expiry call

`GET https://world.openfoodfacts.org/api/v3/product/{code}?fields=product_name,brands,quantity,serving_size,categories,categories_tags,nutriments`

Map into the form-prefill shape: name, brand, serving, nutrition, category suggestion if present. **Ignore any OFF expiration/use-by field.** 404 → cache miss 24h and fall through (USDA comes in PR 6). 429/5xx → return a "lookup temporarily unavailable, enter manually" and do not cache.

In-process rate limiter: 15 product reads/min. If we later add name search, cap at 10/min and do not search-as-you-type.

User-Agent: `Fridge/0.1 (https://github.com/George-Anagnostou/fridge; anagnostougeorgejohn@gmail.com)` unless George wants a different contact. Optional `OFF_BASE_URL` for the staging host `https://world.openfoodfacts.net` (basic auth `off:off`) in tests/dev.

**Tests:** `httptest` server covering 200 hit, 404 miss, 429 not cached, field mapping, and rate-limiter behavior. No live OFF calls in CI.

---

### PR 6 — USDA FoodData Central + FoodKeeper default expiry

**Title:** USDA nutrition fallback and FoodKeeper expiry estimates

**Files touched:** new `fridge/usda.go` + tests, new `fridge/foodkeeper.go` + tests, vendored `data/foodkeeper.json` (snapshot of the FSIS file, with source URL and version noted in a short `data/README.md`), `fridge/lookup.go` (full chain), `cmd/http` (prefill expiry + nutrition; `USDA_FDC_API_KEY` from env), README (how to get a free FDC key, never commit it).

**Schema:** none new required. Uses `lookup_cache` source=`usda`. Item columns from PR 2 (`expiration_source`, `foodkeeper_product_id`, nutrition fields) get filled at add time.

**Lookup order for a barcode:** cache → OFF (PR 5) → USDA branded search, **exact `gtinUpc` match** on the normalized code (try the normalized form; if no row, try common unpadded variants already covered by normalization). Merge: OFF name/brand/nutrition win when present; USDA fills gaps (especially when OFF nutrition is missing). Then FoodKeeper: manual category if set, else USDA description/category keywords, else OFF categories. Compute a suggested expiry from **today** (date of purchase) and the item location:

- Fridge → `DOP_Refrigerate_*` or `Refrigerate_*`
- Freezer → `DOP_Freeze_*` or `Freeze_*`
- Pantry → `DOP_Pantry_*` or `Pantry_*`

Convert Days / Weeks / Months to a date. If only a max is set, use it; if min and max, use **min** (conservative). "Package use-by date" / "Not Recommended" / empty → no default, user must enter. Prefill the date but leave it editable; if the user changes it, `expiration_source=user`.

**Endpoints:** `GET /lookup?barcode=` returns nutrition + `suggested_expiration` + `foodkeeper_product` + `sources`. Add/edit already accept an expiration override. `GET /foodkeeper?q=` optional later; not required for v1 if category is a picker of FoodKeeper product names.

**Tests:** FDC client with `httptest` (exact gtinUpc match, no key leaked in logs); FoodKeeper date math; mapping prefers manual category; OFF expiration field is never read; missing key returns a clear error and continues to manual entry. No live USDA calls in CI.

---

### PR 7 — In-page barcode scanning

**Title:** Scan barcodes on the phone with html5-qrcode

**Files touched:** `cmd/http/static/` (vendored html5-qrcode, scan JS, CSS), templates (scan control on the add page; manual barcode field always available), `cmd/http/main.go` if a CSP or static route is needed, README (HTTPS or localhost for the camera).

**Schema / backend:** none new. Scan → `GET /lookup?barcode=` → prefill the existing add form → George confirms quantity, location, expiry, category → save. If both sources miss, the form stays manual with the barcode filled in.

**Tests:** lookup integration already covered; add a handler test that a normalized manual barcode posts through the same prefill path. Manual camera QA on iOS Safari and Android Chrome over **localhost or HTTPS** (document results in the PR). HTTP on a LAN IP will not get camera access — that is why deploy/hosting is the next, approval-gated step.

---

### PR 8 — Deploy (requires George's explicit approval)

**Title:** Documented, manual deploy path with HTTPS and a persistent SQLite volume

**Do not merge or ship this until George approves the host, the domain, and the auth model.** Do not add a "deploy on push to master" workflow.

**Likely files:** `Dockerfile` (build with CGO/gcc, or a two-stage image), a tiny compose or Caddy/Caddyfile snippet for local HTTPS, `.env.example` (`USDA_FDC_API_KEY`, `FRIDGE_DB`, `PORT`, later auth secrets), README deploy section, maybe a **manual** `workflow_dispatch` CI job later — never `on: push` to `master`.

**Needs, once a host is chosen:**

- Persistent disk for `fridge.db` and a backup story (copy the file; it is the whole app state).
- **HTTPS** so the phone camera works off localhost (a home-network HTTP server is not enough for scanning, unless George only ever uses the app on the same device via localhost or a tailnet with TLS).
- Auth if the app is reachable beyond George's own devices: start with a single shared password or Tailscale/SSO, not a full user system, unless the household question says otherwise.
- Env vars only on the host — never in git.

**Tests:** image builds; server starts with `FRIDGE_DB` on a volume; `/` is 200 over the intended URL.

---

## 5. Open questions only George can answer

Ranked by how much they block later PRs (PRs 1–7 can proceed on the defaults below).

1. **Where should this be hosted?** Home network vs a public subdomain (or a tailnet). This is the big one: the camera needs HTTPS or localhost, and a public URL almost certainly needs auth. Default until answered: develop on localhost; do not set up a host.
2. **Who uses it — only George, or the household?** Drives whether we add login, a shared password, or nothing (localhost / VPN only). Default: single daily user, no auth until it leaves localhost.
3. **When the same barcode is bought again, new lot or merge quantity?** Default in this plan: always a new row so expiries stay honest. Merging would need a rule for mixed dates.

Smaller follow-ups, not blockers: confirm the OFF User-Agent email; cache hit TTL 14 days vs 7 or 30; whether a FoodKeeper "use by on package" item should still get a freezer default when stored in the freezer.

---

## 6. Suggested sequence

```
PR 1  local run + tests
PR 2  migrations + locations/edit/use-soon (data)
PR 3  phone UI (usable by typing)
PR 4  barcode normalize + cache
PR 5  Open Food Facts
PR 6  USDA FDC + FoodKeeper
PR 7  html5-qrcode scan
PR 8  deploy — only after George approves host and auth
```

Receipt OCR and photo input stay off the list.
