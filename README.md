# The Fridge

A simple personal inventory app (Go + SQLite). Item pages and APIs require a
logged-in account. Sign-up is invite-only.

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

### First invite

Create a single-use invite and print a sign-up link (uses `FRIDGE_DB`, default
`./fridge.db`):

```sh
go run ./cmd/invite
```

```sh
FRIDGE_DB=/tmp/fridge.db go run ./cmd/invite -ttl 24h -base-url http://localhost:8080
```

Open the printed `Sign up:` URL, create an account with email and password
(at least 8 characters), then log in at `/login` later as needed.

Session cookies are HTTP-only and SameSite=Lax. They are only marked `Secure`
when `COOKIE_SECURE=1` (or `true` / `yes`) is set, which is what you want
behind HTTPS. Leave it unset for local `http://localhost` development.

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
```

## Makefile

`make build` writes `fridge-local`, `fridge-remote`, `fridge-server`, and `fridge-invite` to `./build/`.
