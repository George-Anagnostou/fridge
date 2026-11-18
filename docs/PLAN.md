# Fridge build plan

A small, unadvertised app for tracking what's in your stores. A "fridge" is any
store (fridge, freezer, pantry, cabinet). SQLite-backed CRUD plus barcode
scanning, on the existing Go backend. Merges to `main` never auto-deploy.

## Scope

- Go stdlib `net/http`, server-rendered HTML, pure-Go SQLite (`modernc.org/sqlite`).
- Accounts: email + password, sessions, CSRF; invite-only sign-up. The admin
  can invite anyone; a store owner can invite by email when sharing a store,
  and that invite is tied to the store and permission. Invites are shown as
  links for now (no email sending yet).
- Stores: a user can own several. The owner shares a store with other users and
  sets each member's permission (view, or edit = add/update/delete items).
- Items: name, quantity, expiry, barcode, brand, basic nutrition. Buying the
  same item again adds a new row.
- Barcode: html5-qrcode phone page → normalize UPC/EAN → local cache →
  Open Food Facts API v3 → USDA FoodData Central (`gtinUpc`) → manual entry.
  Cache hits 14 days, misses 24h, never cache 429/5xx.
- Deferred: Google sign-in, email/password reset, FoodKeeper expiry defaults,
  receipt OCR, remote CLI.

## Steps

1. Fix build, embed templates, tests — [PR #2](https://github.com/George-Anagnostou/fridge/pull/2).
2. Accounts, sessions, admin-only invite sign-up — [PR #3](https://github.com/George-Anagnostou/fridge/pull/3).
3. Pure-Go SQLite, minimal CI, Go 1.27.4 — [PR #4](https://github.com/George-Anagnostou/fridge/pull/4).
4. Stores and sharing: stores table, members with view/edit permission,
   owner manages members; sharing by email adds an existing user or creates a
   sign-up invite tied to the store and permission (shown as a link; no email
   sending yet); existing items move into a default store.
5. Item CRUD in a store: edit, barcode/brand/nutrition fields, use-soon list.
6. Mobile UI: phone-friendly store list and add/edit forms.
7. Barcode normalization + lookup cache.
8. Open Food Facts v3 client (rate-limited, ODbL credit, ignore OFF expiry).
9. USDA FDC fallback (`USDA_FDC_API_KEY` from env).
10. html5-qrcode scan page that prefills the add form (needs HTTPS or localhost).
11. Deploy like bookmarks (manual, approval-gated).

## Decided

- Permissions are view and edit only.
- Store owners can invite new users by email when sharing.
- Invites are shown as links for now; no email sending yet.
