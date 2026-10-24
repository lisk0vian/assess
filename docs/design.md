# Assess design

Assess (`ax`) is an open-source Go tool to create, run and proctor exams in a
**classroom LAN**. The teacher works in the terminal (CLI + Bubble Tea TUI);
students answer in a browser and never install anything.

This document records the decisions taken during the design review
(questions Q1–Q76). Change it through pull requests; when a decision is
reversed, edit it here and note why in the commit.

## Principles

- **Self-contained.** One executable embeds the student web app; data lives in
  local files (exam folders + SQLite). No Node.js, Go or external database is
  needed to run it.
- **Server is the authority.** Time, grading, penalties and answer keys live on
  the server; the browser only displays and reports.
- **Signals, reversible penalties.** Anti-cheat uses browser APIs only.
  Automatic penalties exist, but every one is transparent to the student and
  reversible by the teacher.
- **Ship working slices.** Every phase ends with something usable.

## Scope (MVP)

- Scenario: teacher and students on the same LAN. Internet access is
  documented as "put it behind your own tunnel/proxy".
- Question types: `single`, `multiple`, `truefalse`, `short` (number with
  tolerance, or normalized text). `essay` and `parametric` are reserved.
- Content: sanitized Markdown + images from `assets/` + KaTeX.
- Result shown to students: `none | score | review` per exam (default
  `score`); `review` only after the session closes.
- Languages: code, CLI and technical docs in English; student UI and TUI
  translated (es default, en).
- License: Apache-2.0.

## Runtime architecture

```
 teacher terminal                          classroom LAN
┌──────────────┐  control API   ┌───────────────────────┐  HTTP + WebSocket  ┌──────────┐
│  ax (TUI/CLI)│◄──────────────►│ ax daemon (1 session) │◄──────────────────►│ browsers │
└──────────────┘ 127.0.0.1:rand │  SQLite (WAL)         │  :27337            └──────────┘
                    + token     └───────────────────────┘
```

- **One background server process per session**, detached from the terminal
  (Windows: `DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP`; Unix: `setsid`).
- The TUI is a client: `Ctrl+C`/`q` only detaches. The exam ends only when time
  runs out, the teacher ends it, or `ax stop` is run.
- Commands: `ax start <dir>`, `ax attach` (or bare `ax` when a session is
  active), `ax status [--json]`, `ax logs [-f]`, `ax stop`, `ax stop --suspend`,
  `ax resume`.
- **Absolute end time** stored in SQLite: the clock keeps running while the
  server is down; on resume the TUI offers global extra time to compensate.
- Health: lock file with PID + heartbeat row in SQLite. `ax status` reports
  `Running`, `Not running` or `Crashed` (with the last error log lines).
  Logs: `log/slog` JSON in the data dir, size-rotated.
- **Local control API** on `127.0.0.1` (random port) authenticated with a token
  file readable only by the current user.
- One active session per data directory.
- Network: student port **27337** by default (avoids common dev ports; falls
  back to the next free one), `--port`, `--host`; LAN address auto-detected
  (asks when several); TUI shows URL, session code and a QR code. Plain HTTP on
  the LAN; optional `--tls-cert/--tls-key`.

## Data

- Exams are referenced by **folder path**; there is no central library.
  Format: see [format.md](format.md).
- Data dir: OS user data dir (`%AppData%\assess` on Windows), overridable with
  `--data-dir` / `AX_DATA_DIR`.
- Starting a session creates an **immutable published version** (with hash).
- Randomization: shuffled questions/options (default on), pinned options,
  pools (`pick k of n`); the per-attempt seed is stored.
- Navigation is free with "flag for review", except questions with
  `time_limit`, which lock when their time expires.
- Scoring: per-question points, optional partial credit for `multiple`, no
  negative marking; computed server-side and tied to the published hash.
- SQLite via `modernc.org/sqlite` (no CGO), WAL mode, a single writer
  connection plus a read-only pool, `database/sql` with explicit SQL and
  migrations embedded from `internal/storage/sqlite/migrations/`. A database
  written by a newer `ax` is refused instead of being silently downgraded.
- Published exams (content, answer key and asset bytes) are stored in SQLite
  by hash, so a session never depends on the exam folder after it starts.
- Answers are upserted only when their client sequence number is higher than
  the stored one: retries and out-of-order deliveries are harmless.
- Student IDs are normalized (trimmed, uppercased, inner spaces removed) so
  "a 123" and "A123" are the same student.
- Results: `ax results <session> --format csv|json`.
- Backups: `ax backup` (`VACUUM INTO`, works while running), `ax restore`, and
  an automatic backup when each session ends.

## Participants

- Join with a shared link + short **session code** + name and student ID.
  Optional `--roster students.csv` restricts IDs and shows who is missing.
- **Lobby + common start** triggered from the TUI; late joiners get the
  remaining time. Global or per-student extra time; early close.
- Reconnection: an attempt cookie resumes on the same device. Another device,
  or a reused ID, needs **teacher approval in the TUI** and is logged.
- Manual add/remove from the TUI (Huh form) and `ax student add|remove|list`.
  Removing a student who started marks the attempt `withdrawn` (data kept,
  can be readmitted).
- Network drops: the browser queues changes with sequence numbers
  (idempotent); the server accepts what it **receives** before the end plus a
  30 s grace period (configurable).

## Teacher TUI (the only admin interface)

- Live: header (time left, connected, waiting, submitted, server health),
  student table (current question, progress, connection, signals, strikes),
  ranking by score then time, live signal log, per-student timeline, optional
  seat map coloured by signals.
- Actions: start, extra time, end, kick/void, readmit, approve reconnection,
  message a student, pause a student, annotate/revert a penalty, remove a
  strike, add/remove student.
- Optional password (`ax admin set-password`, Argon2id), asked on `attach`,
  `stop` and reports; **auto-lock** after 5 min idle and manual lock (`L`);
  a locked TUI shows no personal data. The password protects the TUI, not the
  disk — lock the OS session too.
- Reports (`ax report`, closed sessions only, rendered with ntcharts v2):
  score histogram, time distribution, % correct per question, signal timeline,
  session history, collusion analysis (identical wrong answers, synchronized
  answering, weighted by seating), "left-and-got-it-right" correlation, leaked
  question detection across sessions.

## Anti-cheat (browser APIs only — no installs, no Safe Exam Browser)

**Deter**
- Pools and shuffling; seat-aware pool assignment so neighbours differ.
- Watermark with name, student ID and time over the question area.
- Text selection, context menu, copy and paste blocked; poisoned clipboard if a
  copy still happens ("Protected content — Assess · ID · time").
- Questions fetched one at a time; options **revealed on hover** (focus for
  keyboard, press-and-hold on touch), absent from the DOM until revealed and
  obfuscated with the attempt key.
- **Privacy curtain**: the exam blurs instantly on blur/visibility loss.
- Optional per-question `time_limit`.
- `fullscreen: off | suggested | required`.
- Transparent notice listing what is recorded and the penalty table, plus a
  typed honor pledge, accepted before starting.

**Detect** (each signal has severity `info | warn | high`; no global "risk score")
- Visibility changes, focus loss, copy/paste/PrintScreen, fullscreen exit.
- Disconnections (informational only).
- Multiple tabs (Web Locks + `BroadcastChannel`; the second tab is blocked),
  device change, same device for two students.
- Abnormal speed, abrupt window resize.
- `event.isTrusted === false`, `navigator.webdriver`.
- Injected DOM from extensions (warn only).
- `beforeinput` forensics: pasted/dropped/replaced insertion (high), typing
  bursts (warn) — type and speed only, never content.
- Optional AI canary text.
- **Telemetry and answers share one channel** (WebSocket, sequence numbers,
  per-attempt HMAC); missing heartbeats or sequence gaps raise
  `telemetry_tampered` (high).

**Act** (server-side rule engine)
- Each signal maps to `warn`, `time_penalty`, `void_question`,
  `point_penalty` or `strike`; `standard` and `strict` profiles ship tables.
- **3 strikes annul the attempt** immediately; the teacher can readmit,
  remove strikes or revert any penalty, with a note.
- Away countdown in the tab title/favicon, optional system notification;
  strike counter always visible to the student.
- False-positive guards: ignore focus loss < 1–2 s, one continuous absence =
  one strike, grace period at start, network drops never count.
- Profiles: `proctoring: off | standard | strict` (default `standard`);
  `devices: any | desktop` (`desktop` in `strict`).

**Deliberately excluded:** devtools detection, random presence checks,
VM/remote-desktop detection, `getDisplayMedia`, anti-OCR obfuscation, webcam,
AI video analysis. **Known limit:** a second device (photo + vision AI) cannot
be detected; mitigated by time pressure, variation and the teacher's presence.

## Student web app

- One React + TypeScript + Vite app with Tailwind v4, shadcn/ui (Radix),
  lucide-react and tw-animate-css. Markdown via react-markdown + remark-math +
  rehype-katex (lazy, only when math is present) + rehype-sanitize. API types
  generated from Go with `tygo`.
- Performance: brotli/gzip pre-compressed, hashed, `immutable` assets embedded
  in the binary; self-hosted fonts and no CDN (works offline); optimistic UI.
  CI budget: initial JS ≤ 150 KB gz (excluding KaTeX), CSS ≤ 30 KB, load < 1 s
  on LAN, interaction < 50 ms.
- Design: system light/dark, accent and logo from the manifest; one question
  per screen with a navigator (sidebar on desktop, bottom sheet on mobile) and
  keyboard shortcuts; calm timer with 10/5/1-minute warnings; review screen
  before submitting.
- Design workflow uses the **`/impeccable`** skill: design tokens in
  `web/DESIGN.md` mapped to the shadcn/Tailwind theme → prototypes of the six
  screens and all anti-cheat states → implementation → critique, audit,
  hardening and polish passes before closing phases 3 and 5. Priority: reduce
  student anxiety; warnings are firm, never hostile.

## Portability

- `.examx`: ZIP with `examx.json` (format version, generator version, SHA-256
  per file) plus the exam folder. Never contains participants or results.
- Encrypted with **age** (passphrase) by default when the answer key is
  included; `--no-key` exports questions only; `--no-encrypt` forces
  plaintext with a warning.
- Safe import: no path traversal or symlinks, size/count/ratio limits, hash
  check and validation before writing, no overwrite without `--force`.

## Quality and distribution

- Target: 100 concurrent students per session on a regular laptop; load-tested
  with 200.
- Tests: golden-file format tests, grading edge cases, real SQLite in temp
  dirs, `httptest`, `teatest`, Playwright, load test, chaos test (kill the
  daemon, `ax resume`).
- CI: GitHub Actions on Windows/Linux/macOS (`go test`, `golangci-lint`, web
  build, `size-limit`). Releases with GoReleaser (unsigned at first; SmartScreen
  warning documented).

## Phases

0. **Foundations** — repo, module, this document, example exam, CI, license.
1. **Format & validation** — YAML schema, `ax new`, `ax validate`.
2. **Domain & SQLite** — migrations, publication, attempts, pools/seeds,
   grading, events.
3. **Daemon + student web (first delivery)** — design system and prototypes
   with `/impeccable`; `start/status/logs/stop/resume/attach`; minimal TUI
   with links, QR and counters; join control (`join_until`, private networks
   only, rate limits, headcount); single tab; unified telemetry channel;
   lobby, server timer, queued autosave, submit, `ax results`.
4. **Supervision TUI** — live table, ranking, actions, reconnection approval,
   add/remove, password and lock, seat map.
5. **Anti-cheat** — signals, rule engine and strikes, hover reveal,
   curtain, fullscreen modes, watermark, `time_limit`, profiles, device
   policy, input forensics, poisoned clipboard, honor pledge,
   `telemetry_tampered`.
6. **Reports** — `ax report` with ntcharts and the analyses above.
7. **TUI editor** — home menu, Huh forms, answer-key editing, `$EDITOR`.
8. **Portability** — `.examx` with age, safe import, backup/restore.
9. **Distribution & hardening** — GoReleaser, load/chaos tests, install,
   backup and upgrade docs.

## Dependencies

`spf13/cobra`, `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`,
`charm.land/lipgloss/v2`, `charm.land/huh/v2`,
`github.com/NimbleMarkets/ntcharts/v2` (needs Go ≥ 1.26.8),
`github.com/coder/websocket`, `modernc.org/sqlite`, `go.yaml.in/yaml/v3`
(maintained fork of the archived `gopkg.in/yaml.v3`),
`filippo.io/age`, `golang.org/x/crypto/argon2`, a terminal QR library.
Each is added in the phase that first needs it.
