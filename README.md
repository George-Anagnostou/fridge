# The Fridge

A simple personal inventory app (Go + SQLite).

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

Then open http://localhost:8080 (or the port you set).

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

`make build` writes `fridge-local`, `fridge-remote`, and `fridge-server` to `./build/`.
