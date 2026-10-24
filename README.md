# Assess (`ax`)

Create, run and proctor exams from the terminal. Students answer from any
browser on the classroom network — nothing to install.

> **Status: pre-alpha (phase 0).** The design is settled; features are being
> built phase by phase. See [docs/design.md](docs/design.md).

## What it will do

- Exams are plain YAML folders you can edit and version
  ([format](docs/format.md), [example](examples/algebra-01)).
- `ax start ./my-exam` launches a background server and a live TUI: links, QR
  code, who is on which question, signals, extra time, results.
- The server enforces time, grading and penalties; answer keys never reach the
  browser.
- Browser-only anti-cheat with transparent, reversible penalties.
- One self-contained executable for Windows, Linux and macOS.

## Development

Requirements: Go 1.26.8+ (Node.js 24 and pnpm arrive with the web app in phase 3).

```sh
go build ./cmd/ax
go test ./...
./ax version
```

## License

[Apache-2.0](LICENSE)
