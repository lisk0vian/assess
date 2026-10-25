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

## Student channel and join control (implemented in phase 3)

- Browsers keep a WebSocket (`/api/ws`, same-origin, attempt cookie) that
  receives `{"type":"changed"}` whenever the session or attempt changes
  (start, extra time, submission, end); they then reload `/api/me`. Pings every
  15 s keep the teacher's "online" count accurate. A slow poll covers socket
  outages.
- Answers stay on idempotent HTTP `PUT`s with sequence numbers. Moving answers
  and integrity signals into one HMAC-signed channel (`telemetry_tampered`)
  lands with the anti-cheat signals in phase 5, when there is telemetry to
  protect.
- Only private, loopback and link-local addresses reach the student server
  (`--allow-public` opts out, for teachers using their own tunnel).
- Joining closes `--join-window` after the start (default 15 min); students
  already in keep resuming. Join attempts are rate-limited per IP (12/min).
- A second tab of the same browser shows "already open". Classroom HTTP is not
  a secure context, so tabs negotiate over `BroadcastChannel` (Web Locks is
  HTTPS-only).
- `ax results [--format csv|json] [--sep ";"]` exports one row per student
  with per-question points; CSV carries a UTF-8 BOM for Excel.

## Teacher TUI (implemented in phase 4)

- Views (Tab): **Alumnos** (live table: state, question the student is on,
  answered, online, score, time used, extra time), **Ranking** (submitted,
  by score then time), **Enlace** (URL, code, QR with the code pre-filled).
- Keys: S start, + 5 min for everyone, E end (confirm); on the selected
  student T extra time, M message, P pause/resume (their clock keeps running),
  X annul (confirm), U readmit, D remove (withdraws if they joined; confirm);
  N add a student; A/R approve or reject the oldest pending device request;
  L lock; Q detach (the exam keeps running).
- Approval requests: a student ID already in use on another device, or a
  join after `--join-window`, waits on "Esperando a tu profesor" until the
  teacher decides. Approved devices share the attempt.
- `ax admin set-password` (Argon2id in `admin.json`, owner-only) protects
  `ax attach`, `ax stop` and the panel, which locks after 5 idle minutes or with
  L and then shows no names. Pause state and pending requests live in memory:
  a daemon restart clears them (the attempts themselves are in SQLite).
- Integrity (phase 5): an AVISOS column (active strikes / limit · warn and
  high signals), a total in the header, and an **Incidencias** view with the
  newest signals (time, student, signal, duration, question, consequence;
  reverted ones are marked). Z reverts the selected incident's penalties, or
  in Alumnos the student's newest one, after asking for a note.
- Seat map: deferred (optional; planned with the collusion report in phase 6,
  which is where seat-aware weighting is used).

## Teacher TUI (design decisions)

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

## Anti-cheat engine (implemented in phase 5)

- `internal/proctor` maps a signal to a decision (warn, strike, lost time,
  voided question) by profile. `strict`: away > 15 s = strike and 1 min lost,
  focus loss > 10 s = strike, copy = strike (the second one also voids the
  question), paste = strike. `standard` is gentler: away > 30 s or focus loss
  > 20 s = strike, the first copy and pastes only warn. Short glances (< 3–5 s)
  record nothing; nothing counts during the first 15 s of an attempt except
  automation and forged telemetry. Extensions, typing bursts and a second tab
  only warn.
- Manifest: `proctoring.fullscreen`, `devices`, `reveal` and `strike_limit`
  override the profile defaults.
- Penalties live in their own table and are never deleted: reverting one keeps
  it with a note, gives lost time back and regrades a closed attempt. Reaching
  the strike limit annuls the attempt on the spot; readmitting is a separate,
  explicit teacher action.
- Integrity signals travel on the live socket, numbered and signed with
  HMAC-SHA256 under a per-attempt key (HMAC of a per-session secret and the
  attempt id). A bad signature is itself a `telemetry` signal (strike); a
  repeated number is ignored. The browser ships its own SHA-256 because
  `crypto.subtle` needs a secure context. Timed signals (away, focus,
  fullscreen) send a start and an end; the server keeps the longer of its
  own measurement and the browser's. Answers stay on HTTP: when they arrive
  while the socket has been silent for a minute, a `telemetry_gap` warning is
  recorded.
- `reveal: hover` options are sent sealed (XOR with an HMAC keystream bound to
  question and option, base64): they never appear in the DOM or in plain
  network responses until revealed. It is obfuscation, not encryption.
- `time_limit` is enforced by the server from the first opening of the
  question (plus 5 s grace); voided questions reject answers and score 0.
- The honour pledge is recorded once per attempt (`POST /api/pledge`).

## Student integrity UI (implemented in phase 5)

- Before the exam (standard and strict), a sheet lists what is recorded and
  its consequence for the exam's profile, and the student types the honour
  pledge (pasting is disabled). "Start" is also the gesture fullscreen needs;
  `suggested` offers a windowed start.
- During the exam: strike boxes in the header, a watermark with name, ID and
  time over the sheet, text selection, copy, paste and right-click blocked
  (copies get a poisoned clipboard), and every signal signed and sent on the
  live socket (queued while it is down).
- One continuous absence is one episode, counted as its most serious kind
  (hidden tab > out of fullscreen > focus elsewhere). The curtain hides the
  exam at once; past the warning threshold it becomes a countdown to the
  strike, mirrored in the tab title. A strike shows "Strike n of N" with the
  reason and a reminder that the teacher can revert it; warnings are a quiet
  notice.
- Hover-only options show a ruled bar until pointed at, focused or tapped
  (first tap reveals, second chooses); only one is readable at a time.
- Timed questions ask before opening, because opening starts their clock;
  an exposure bar counts down and the answer locks at zero. Voided questions
  say so and lock.
- `devices: desktop` turns away touch-only devices before the exam.
- Classroom HTTP on a LAN address is not a secure context: no
  `crypto.randomUUID`, `crypto.subtle` or `navigator.clipboard`. The app must
  run without them, and is tested from the LAN address, not localhost.
- Screen captures cannot be blocked by a web page. Mitigations: the exam
  hides while the Windows/Command key is held (Win+Shift+S, Cmd+Shift+4
  start with it), PrintScreen replaces the clipboard image with a notice and
  counts like a copy, printing is blocked (Ctrl+P and print CSS), options can
  be hover-only, and the watermark names the student. A phone photo is the
  known limit.
- An absence whose page never reports its end (reload, closed tab) is closed
  by the server with its own measurement when the student reconnects, unless
  the new page re-announces it within 5 s.
- Readmitting an attempt annulled by strikes reverts the newest strikes
  (noted "readmitido") until one is left before the limit.

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
