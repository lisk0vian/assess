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
  active; with none, bare `ax` in a terminal opens the menu), `ax status [--json]`, `ax logs [-f]`, `ax stop`, `ax stop --suspend`,
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
- Keys: S start, + 5 min for everyone, ⇧M message to everyone, ⇧P pause or
  resume everyone, E end (confirm: Y end and keep the server for results, A
  end and stop the server; once ended, E stops the server); on the selected
  student T extra time, M message, P pause/resume (their clock keeps running),
  X annul (confirm), U readmit, D remove (withdraws if they joined; confirm);
  N add a student; A/R approve or reject the oldest pending device request;
  L lock; Q detach (the exam keeps running). Labels follow the state (pause
  becomes resume).
- The session ends by itself once every student has turned in or run out of
  time. Withdrawing or annulling the last one does not end it, so they can
  still be readmitted.
- A readmitted student continues on the same page, on the question they were
  on (the page stays connected while the session is open).
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
- Seat map: deferred. The phase 6 collusion analysis works without seats;
  seat-aware weighting is added when the map exists.

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

## Editor and menu (implemented in phase 7)

- `ax new` (no folder, in a terminal) is a wizard for teachers who never read
  the format. Four steps in a stepper: **Datos** (title, folder derived from
  the title, duration presets in words, language, what students see,
  instructions with a default), **Preguntas** (a list with a live preview of
  each question as students see it, the right answer marked; add, edit,
  delete, reorder; the empty state explains the four types), **Vigilancia**
  (profile with a plain-language description of what students will notice,
  shuffling), **Revisar** (summary; the exam is written to a scratch folder
  and validated before anything touches the real one). Then: start it now,
  keep editing, or quit. Esc goes back a step; leaving with work asks first.
  Built with Bubble Tea, bubbles (list, viewport, help, key, spinner), huh
  and lipgloss. Question ids are assigned in order (q001…).
- Files stay first-class: `ax format` prints the [authoring guide](authoring.md)
  (a complete valid example, checked by the tests, plus a checklist and a
  request a teacher can give an AI) and the format reference, so an AI with
  only the binary can write an exam and loop on `ax validate --json`.

- `ax edit <dir>` opens the exam in the terminal: the settings row, the items
  in order (pools grouped with their questions, questions no item names
  marked) and the validation result, refreshed after every change.
- Huh forms, styled like the panel and with Spanish key help:
  - **Settings**: title, duration, language, what students see on turning in,
    instructions, shuffling, proctoring profile and strike limit. The exam id
    is shown but not editable (it links the session history).
  - **Question with its answer**: type and id (new questions only), prompt,
    points; options one per line (the line at a position keeps that option's
    id and pin, new lines take the next free letter) and the right ones
    picked from them; true/false; short number with tolerance, or short text
    with accepted answers (normalization kept, or the usual defaults); time
    limit and option reveal. Decimal commas are accepted.
- Keys: Enter edit, N new, D delete (with its answer and references; pools
  keep leaving one question out and become a plain item with one left),
  Shift+↑↓ move an item, E open the question (or the manifest) in
  `$VISUAL`/`$EDITOR` (notepad or vi by default; `code -w` works), C open the
  answer key, R reread, S start the exam (refused while there are errors),
  Q quit. Esc discards a form.
- Saving (`exam.Draft`): every confirmed form writes at once. New content is
  merged into the existing YAML nodes, so comments, key order, quoting of
  unchanged values, block style of unchanged text and the blank lines between
  top-level sections survive; files are replaced atomically. A file with
  YAML syntax errors is never overwritten: open it with E. Saving unchanged
  content leaves every example byte for byte identical (tested).
- Menu (bare `ax`, no exam running, in a terminal): resume the open session
  (when its server stopped), edit, create (folder, title, language, then the
  editor), start (exam folders found up to three levels below the current
  one, or any path), report of the last ended session, quit.

## Reports (implemented in phase 6)

- `ax report [--session id] [--json]`: only once the session has ended; asks
  for the panel password and closes itself after 5 idle minutes when one is
  set. Piped output is plain text, `--json` is the full analysis for tools.
  In the panel, **I** opens the report of an ended session.
- The analysis (`internal/report`) grades every attempt from scratch,
  ignoring penalties, so it sees what students actually answered. Scores and
  times count submitted attempts only; collusion and "left and got it right"
  consider every attempt.
- Views, drawn with ntcharts v2 bar charts:
  1. **Resumen**: counts, mean/median/deviation/range, histograms of the score
     (10 % bins) and of the time used (from each student's own start), and
     the students with strikes or evidence.
  2. **Preguntas**: % correct per question, blank rate and discrimination
     (correlation between getting it right and the rest of the exam). Notes
     flag negative discrimination (check the key), questions that barely
     discriminate, possible leaks, hits after leaving and voided answers.
  3. **Incidencias**: warn/high signals over time (≤ 30 bars), totals by
     signal kind, active strikes, reverted penalties, time removed.
  4. **Indicios** (evidence, never proof):
     - *Similar answers*: for every pair, the chance two wrong answers to a
       question match comes from how the whole class answered it wrong
       (Σ nₖ(nₖ−1) / N(N−1)), so a shared popular distractor or a true/false
       says nothing and a shared odd wrong number says a lot. The chance of
       the observed matches or more is a Poisson binomial tail, multiplied by
       the number of pairs compared. Medium: ≥ 3 identical wrong answers and
       corrected p < 0.05, or ≥ 4 identical answers saved within 20 s of each
       other while the questions were at different positions (so same pace in
       the same order does not count) on at least a third of the shared
       questions. High: corrected p < 0.01, or both signs together.
     - *Left and got it right*: a hidden/blur/fullscreen-exit absence on a
       question followed, within 90 s of coming back, by the final correct
       answer to that question. Weighted by difficulty (share of the other
       students who did not get it right); strong when ≥ 50 %.
  5. **Historial**: the sessions of the same exam id (any published version)
     with their mean and median, and *possibly leaked questions*: correct
     rate at least 20 points above the earlier sessions pooled (≥ 10 earlier
     answers, ≥ 5 now) and one-sided z ≥ 2.33. The advice is to rewrite or
     rotate the question.
- Live: the panel marks "left and got it right" as ↩N in AVISOS and lists the
  hits at the top of Incidencias; the difficulty is provisional until
  everyone turns in.

## Anti-cheat engine (implemented in phase 5)

- Profiles. `standard` and `strict` protect the same way: pledge, curtain,
  copy and paste blocked, capture shield, watermark, required fullscreen,
  hover-only options. They differ only in tolerance:

  | | `standard` | `strict` |
  |---|---|---|
  | Away from the tab | warning from 3 s; after 10 s a strike and −20 % of the remaining time | warning from 1.5 s; after 3 s a strike and −50 % of the remaining time |
  | Another window | warning from 3 s, strike after 10 s | warning from 2 s, strike after 5 s |
  | Out of required fullscreen | strike after 5 s | strike after 3 s |
  | Copy or screen capture | first time a warning, then strike and the question is voided | strike and the question is voided |
  | Paste into an answer | first time a warning, then strike | strike |
  | Strikes that annul | 3 | 2 (the first one is the warning) |
  | Devices | any | desktop |

  `off` turns every protection off and ignores the overrides. Automation and
  forged telemetry are strikes; extensions, typing bursts and a second tab
  only warn. Nothing counts during the first 15 s of an attempt except
  automation and forged telemetry.
- One absence counts once: coming back to the tab but out of fullscreen
  (switching tabs leaves it) is the same absence, which ends when fullscreen
  is restored.
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
- Photo protections, each switchable on its own (`proctoring.lens`,
  `frozen_pointer`; questions override `lens`), on by default in `standard`
  and `strict`. The server resolves exam → question → student and sends
  `lens` per question. The teacher can switch the lens off for one student
  (`attempt.reading`, V in the panel) as an accessibility accommodation.
  `frozen_pointer` is a warn-only signal, measured only while a question is
  on screen and the student is not paused; the panel lists it with "left
  and got it right" when a correct answer follows.

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
  (first tap reveals, second chooses); only one unselected option is readable
  at a time, while a chosen option stays readable so the student can confirm
  it.
- Timed questions ask before opening, because opening starts their clock;
  an exposure bar counts down and the answer locks at zero. Voided questions
  say so and lock.
- `devices: desktop` turns away touch-only devices before the exam.
- Classroom HTTP on a LAN address is not a secure context: no
  `crypto.randomUUID`, `crypto.subtle` or `navigator.clipboard`. The app must
  run without them, and is tested from the LAN address, not localhost.
- Lens: the prompt's rendered words are measured (text ranges and atomic
  images/math); one range of 4–15 words is readable at a time, the rest are
  veiled like hover-only options. Each range starts with the last two words of
  the previous one, so the next one has context, and ranges prefer a natural
  pause. Tapping a zone jumps to the range containing that word; the controls
  under the prompt, horizontal swipe and the keyboard move one range. The active range is
  scrolled into view when it would fall outside the screen, and the position
  is remembered per question while the page is open. The first view reads the
  ranges automatically at the teacher's pace; moving the pointer or scrolling
  does not stop it, while navigating, focusing an answer or pausing does. When
  the automatic pass ends, the prompt is veiled again; tapping a zone or the
  replay control starts it over. Manual navigation keeps the chosen range
  visible. Only the automatic pass types word by word, at a readable pace.
  Hidden text is clipped, not removed, so screen readers read the whole
  prompt. Resize/fullscreen changes re-measure the boxes.
- Screen captures cannot be blocked by a web page. Mitigations: the exam
  hides while the Windows/Command key is held (Win+Shift+S, Cmd+Shift+4
  start with it), PrintScreen replaces the clipboard image with a notice and
  counts like a copy, printing is blocked (Ctrl+P and print CSS), options can
  be hover-only, and the watermark names the student. A phone photo is
  answered by the photo protections above; a video of the screen still
  defeats them.
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
- **Against photos** (phone + vision AI): a lens that shows one range of words
  of the prompt at a time, and a warn-only "stayed still, then answered"
  signal. Each one switchable.
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
- Reaching the strike limit (3 in `standard`, 2 in `strict`) annuls the
  attempt immediately; the teacher can readmit, remove strikes or revert any
  penalty, with a note.
- Away countdown in the tab title/favicon, optional system notification;
  strike counter always visible to the student.
- False-positive guards: ignore focus loss < 1–2 s, one continuous absence =
  one strike, grace period at start, network drops never count.
- Profiles: `proctoring: off | standard | strict` (default `standard`).
  `standard` and `strict` share every protection and differ in tolerance;
  `off` disables everything (see "Anti-cheat engine").

**Deliberately excluded:** devtools detection, random presence checks,
VM/remote-desktop detection, `getDisplayMedia`, webcam (it needs a secure
context, and the classroom LAN is HTTP), AI video analysis, adversarial text
aimed at AI models (brittle, model-specific). **Tried and dropped:** text
drawn as moving noise (a still frame showed only noise, but students could
not read it comfortably). **Known limit:** a second device cannot be
detected; a photo catches one range of the prompt or one option, and an AI
often answers from the prompt alone. A screen recording still captures the
whole automatic pass. Mitigated by the teacher's presence, time pressure and
question design.

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

## Settings and translations (implemented in phase 8)

- **Settings file**: `~/.config/assess/config.yaml` on every system (on
  Windows, in the user folder), or `$XDG_CONFIG_HOME/assess`, `$AX_CONFIG` or
  `--config`. Written on the first run with every setting commented out. Keys
  are declared once in `internal/config` (kind, default, choices), which
  drives validation, `ax config`, the Settings screen and the file template.
- **Precedence**: flag > `AX_<KEY>` environment variable > file > default.
  Flag defaults come from the settings, so `--help` shows them. Anti-cheat
  thresholds are not settings, the manifest always wins, and `new_exam.*`
  only pre-fills new exams.
- **Errors**: an invalid value stops every command except `ax config` with
  `file:line: error: key: message`; unknown keys are warnings.
- **Commands**: `ax config [get|set|reset|edit|path|info]`, the Settings entry
  of the menu, and a welcome screen on the first run that asks the language
  (preselecting the system's).
- **Translations**: one catalog per language in `locales/<lang>.yaml` (English
  is the reference and the default; Spanish is built in), plus catalogs in
  `~/.config/assess/locales/`. Missing keys fall back to English; plurals use
  `one`/`other`. `ax i18n list|check|new` help contributors, and a test checks
  that every key used in Go and in the web app exists and that the built-in
  catalogs are complete.
- **Student page**: its texts are the `web:` section of the catalogs. The
  server injects the exam language's texts into `index.html`, so the bundle
  carries no dictionaries; `/api/i18n` serves them to the Vite dev server.
  The language is the manifest's `language`, else the `student_language`
  setting, else the interface language.
- Exam validation messages stay in English: they belong to the format
  reference that AI assistants read.

## Vault (implemented in phase 9)

- **What it is**: a folder holding `vault.yaml`, `assess.db`,
  `exams/<id>.examx`, `classes/<name>.yaml` (a readable copy of each class),
  `sessions/YYYY/<date> <exam> <class>/{summary.md,results.csv,record.json}`
  and `backups/`. Copy the folder to move everything to another computer.
- **Which one**: `--vault`, then a `.assess-vault/` folder in the current
  folder or a parent, then the `vault.path` setting (`ax vault use`), then the
  default vault in the data folder (created plain on first use). Named vaults
  are listed in `vaults.yaml` next to the settings file; forgetting one never
  deletes it.
- **Encryption** (age): `ax vault init` encrypts by default (`--no-encrypt`,
  `vault.encrypt`). A random X25519 identity encrypts every file; it is kept
  in `vault.yaml` wrapped twice, with the password (scrypt) and with a
  recovery key shown once. Files can be added without the password (they are
  encrypted to the public recipient). `ax vault encrypt|decrypt|passwd`
  convert in place without losing data; an interrupted `encrypt` finishes
  when run again. File names (exam titles, class names) stay readable;
  contents do not.
- **Database**: an ordinary SQLite file whose sensitive columns are sealed
  with AES-256-GCM (`internal/seal`, key derived from the identity with
  HKDF): published content and answer keys, assets, names and identifiers,
  answers, event data, notes. Identifiers are sealed deterministically so
  lookups, unique indexes and joins still work. Codes, times, states and
  scores stay plain, so status commands work while locked. Without the key
  sealed values cannot be read or written: nothing sensitive is ever stored
  in the clear by accident. `secure_delete` is on and reseals compact the file.
- **Unlocking**: the password is asked once; a small agent (`ax vault agent`,
  127.0.0.1, token readable only by the user) keeps the key in memory for
  `vault.unlock_minutes` of inactivity (0 always asks). `ax vault lock`, the
  panel's lock (manual or idle) and a timed-out report close it. Forgetting
  or merging students, decrypting, exporting and changing the password always
  ask again. The exam server receives the database key through its standard
  input, never on its command line or on disk; `ax resume` unlocks again.
- **One password**: with an encrypted vault the panel and the reports are
  protected by the vault's password; `ax admin set-password` remains for
  plain vaults.
- **Students and classes**: tables `students`, `classes`, `class_members`
  (migration 0004). Everyone who joins a session becomes a student; the
  identifier joins them across sessions. `ax student`, `ax class`
  (CSV import detects separator and encoding, previews and warns about
  duplicates), `ax start --class 3B [--roster-only]`: the class is
  pre-registered and, in roster-only mode, anybody else waits for approval
  (key **G** in the panel approves and adds them to the class).
- **Finding things**: the identifier is the default reference; otherwise a
  forgiving search (`internal/search`: no case, accents, spaces or dashes,
  substrings, words, one or two typos) offers a "Did you mean…?" picker in a
  terminal, or lists the candidates in the error. `ax find`, `ax history`,
  `ax exams`, `ax gradebook` (CSV or a CGO-free `.xlsx`). `ax explore` (and
  the menu) is the TUI for all of it, running the same commands.
- **Archive and backups**: when a session ends (server or `ax stop`) its
  archive is written and an automatic backup made (`vault.keep_backups`).
  `ax backup` (ZIP, `VACUUM INTO`, works during an exam) and `ax restore`
  into a new folder. Forgetting a student rewrites the affected archives;
  older backups keep their data.
- **Migration**: the database of earlier versions moves into the default
  vault on first use; the original is kept in `backups/pre-vault-*.db` and
  the finished sessions are archived.

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
8. **Settings & translations** — `~/.config/assess/config.yaml`, `ax config`,
   Settings screen, catalogs for the terminal and the student page.
9. **Vault** — optional encryption, classes and students, session archive,
   search, Explore, gradebook, `.examx`, backup/restore, several vaults.
10. **Distribution & hardening** — GoReleaser, load/chaos tests, install,
   backup and upgrade docs.

## Dependencies

`spf13/cobra`, `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`,
`charm.land/lipgloss/v2`, `charm.land/huh/v2`,
`github.com/NimbleMarkets/ntcharts/v2` (needs Go ≥ 1.26.8),
`github.com/coder/websocket`, `modernc.org/sqlite`, `go.yaml.in/yaml/v3`
(maintained fork of the archived `gopkg.in/yaml.v3`),
`filippo.io/age`, `golang.org/x/crypto/argon2`, a terminal QR library.
Each is added in the phase that first needs it.
