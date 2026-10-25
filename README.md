# Assess (`ax`)

Create, run and proctor exams from the terminal. Students answer from any
browser on the classroom network — nothing to install.

> **Status: pre-alpha (phase 6).** The design is settled; features are being
> built phase by phase. See [docs/design.md](docs/design.md).

## What it will do

- Exams are plain YAML folders you can edit and version
  ([format](docs/format.md), [example](examples/algebra-01)).
- `ax start ./my-exam` launches a background server and a live TUI: links, QR
  code, who is on which question, signals, extra time, results.
- The server enforces time, grading and penalties; answer keys never reach the
  browser.
- Browser-only anti-cheat with transparent, reversible penalties.
- `ax report` after the exam: score and time charts, how each question
  behaved, the incident timeline, and evidence worth a look (similar wrong
  answers, correct right after leaving, questions that may have leaked).
- One self-contained executable for Windows, Linux and macOS.

## Development

Requirements: Go 1.26.8+. The student web app is prebuilt into
`internal/server/web/dist`, so Go alone builds a complete `ax`. To work on the
web app you also need Node.js 24 and pnpm.

```sh
go build ./cmd/ax
go test ./...

./ax new ./my-exam --title "Álgebra 1"   # scaffold a valid exam
./ax validate ./my-exam                  # check it
./ax validate examples/algebra-01

./ax start examples/algebra-01           # background server + live panel
./ax status                              # running / crashed / not running
./ax stop                                # end the exam (or --suspend)
```

Web app (`web/`):

```sh
cd web && pnpm install
pnpm dev        # Vite on :5173, proxies /api to a local `ax start`
pnpm build      # typecheck + build into internal/server/web/dist
go run github.com/gzuidhof/tygo@v0.2.21 generate   # (repo root) regenerate API types
```

## License

[Apache-2.0](LICENSE)
