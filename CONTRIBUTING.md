# Contributing to Assess

Thanks for your interest in contributing. Assess is a Go + React app for running
classroom exams on a single laptop over the LAN.

## Before you start

- Read [`PRODUCT.md`](PRODUCT.md) for product truth and [`web/DESIGN.md`](web/DESIGN.md)
  / [`internal/tui/DESIGN.md`](internal/tui/DESIGN.md) for the design systems.
- Open an issue first for anything bigger than a small fix, so we can agree on
  the approach before you invest time.

## Development setup

Prerequisites: Go (see `go.mod`), Node 24, pnpm.

```sh
# Go side
go build ./cmd/ax
go vet ./...
go test ./...

# Student web app
cd web
pnpm install
pnpm build        # also regenerates internal/server/web/dist
pnpm typecheck
```

The `web` build embeds into the Go binary via `internal/server/web/dist`, so
run `pnpm build` before `go build` when the UI changes.

## Commit style

Conventional Commits with a gitmoji after the type:

```
type(scope): :gitmoji: summary
```

Examples: `feat(web): :sparkles: add ...`, `fix(tui): :bug: ...`.

Types: `feat`, `fix`, `perf`, `refactor`, `style`, `docs`, `test`, `build`, `chore`.

## Pull requests

- Keep PRs focused on one concern.
- Fill in the PR template checklist.
- CI must be green (tests, lint, web build) before merge.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). By
participating you agree to uphold it.