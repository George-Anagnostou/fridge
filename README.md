# The Fridge

A simple personal inventory app (Go + SQLite). Item pages and APIs require a
logged-in account. Sign-up is invite-only; only an admin can create sign-up
invites. (Inviting someone to share a fridge is a separate feature.)

## Run locally

The HTTP server reads `FRIDGE_DB` (default `./fridge.db`) and `PORT` (default `8080`). Templates are embedded in the binary, so you can start it from any directory.

```sh
go run ./cmd/http
```

Or build and run the binary:

```sh
go build -o fridge-server ./cmd/http
./fridge-server
```

```sh
FRIDGE_DB=/tmp/fridge.db PORT=3000 ./fridge-server
```

Then open http://localhost:8080 (or the port you set). You will be redirected to
log in.

### First admin

Bootstrap the first admin with an invite that grants admin on sign-up (uses
`FRIDGE_DB`, default `./fridge.db`). No credentials are hardcoded:

```sh
go run ./cmd/invite -admin
```

```sh
FRIDGE_DB=/tmp/fridge.db go run ./cmd/invite -admin -ttl 24h -base-url http://localhost:8080
```

Open the printed `Sign up:` URL and create an account (password at least 8
characters). That account can sign in at `/login` and create further sign-up
invites at `/admin/invites`. Those links do not grant admin; use
`go run ./cmd/invite -admin` again if another admin is needed.

Session cookies are HTTP-only and SameSite=Lax. They are only marked `Secure`
when `COOKIE_SECURE=1` (or `true` / `yes`) is set, which is what you want
behind HTTPS. Leave it unset for local `http://localhost` development.

Login attempts are rate-limited in SQLite (10 tries per 10 minutes per IP).
The server uses the connection address by default. Behind nginx, set
`TRUST_PROXY=1` so it uses `X-Real-IP` (falling back to the first
`X-Forwarded-For` entry).

The local CLI talks to the database directly and does not log in. The remote
CLI is not yet updated for authentication, so its HTTP calls will be rejected
until it can send a session.

The local CLI uses the same `FRIDGE_DB` default:

```sh
go run ./cmd/cli/localCLI list
FRIDGE_DB=/tmp/fridge.db go run ./cmd/cli/localCLI add -name Milk -qty 1 -exp 2026-12-01
```

The remote CLI talks to a server at http://localhost:8080. A successful `remove` is a `303` redirect; the CLI treats that as success and does not need to follow it.

## Test

```sh
go test ./...
CGO_ENABLED=1 go test -race ./...
```

The SQLite driver is pure Go (`modernc.org/sqlite`), so `CGO_ENABLED=0 go build ./...` works. Race tests still need CGO for the race detector.

## Makefile

`make build-server` cross-compiles a CGO-free Linux amd64 server to `dist/fridge-server-linux-amd64`. `make build` also writes the local CLI, remote CLI, and invite tool to `./build/`.
