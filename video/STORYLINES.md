# STORYLINES — Assess (ax) README film

Three storylines for a 25–35 s launch film that will sit in the project README.
Assumption line: 1920×1080, 30 fps, English titles over the product's real
Spanish UI (the README is in English, the exam screens are Spanish by default),
music bed + action effects, no voiceover, no paid assets (no HeyGen voice, no
avatar), no logo mark invented — the end card is the ASSESS wordmark in the
product's own condensed caps.

---

## 1. One laptop, thirty screens  *(recommended)*

- **Device / form:** a moment in someone's day → object-to-object chain.
- **What you see and feel:** 8:55 a.m., one teacher's laptop on a light-table
  desk; a terminal line starts the exam, a QR square fills the frame, and the
  same exam arrives on a phone, a school PC, a laptop — thirty sheets of the
  same paper, calmly ticking. It feels like a classroom that just works, no
  internet, no install, no panic.
- **First two seconds:** the terminal already running `ax start
  ./examples/algebra-01`, the QR square growing out of the last line until it
  owns the frame.
- **Beats:** hook (terminal → QR fills frame) → tension (the old way: a stack
  of printed papers, then a cloud quiz stuck on "connection lost") → reveal
  (*Assess — exams from the terminal*) → proof (3 verbs: **scan** → the sheet
  opens in the browser, **answer** → magenta marks "Tu respuesta", **watch** →
  the clock, "Guardado", the teacher's live panel) → payoff (time's up, the
  camera pulls back through the same desk to `ax report`: score chart and
  incident timeline drawing themselves) → claim ("One binary. No cloud, no
  accounts, no installs for students.") → end card (ASSESS wordmark + the real
  command `go build ./cmd/ax`, held 2 s).
- **Look:** light-table ground `#e8e6e1`, square paper sheets with one soft
  shadow, 1px ink rules, tracked condensed capitals, magenta `#ec2a8a` only on
  the student's own answer and position, hatching as the only texture.
- **Sound:** calm minimal pulse (≈100 bpm); the old way sits near silence with
  one lagging spinner, the bed opens on the QR, a soft key tick per verb, and
  one riser into the report payoff.

## 2. The rule breaks (bold pick)

- **Device / form:** type carries the argument → cut on the beat.
- **What you see and feel:** giant Sofia Sans Extra Condensed capitals on a
  ruled grid — NO INTERNET / NO ACCOUNTS / NO INSTALLS — each cut landing hard
  on the beat, and each word snaps into the real screen that proves it (NO
  INSTALLS becomes a phone browser opening the exam). Fast, loud, poster-like.
- **First two seconds:** frame 0 is a full-bleed ruled grid with one word
  already on it — **NO INTERNET** — cut on the first hit.
- **Beats:** hook (word on grid) → tension (words the category sells:
  SEAMLESS? CLOUD? locked over an offline bar) → reveal (the product screen
  takes the frame) → proof (the word→screen pairs: NO INSTALLS, ONE BINARY,
  SERVER-GRADED) → payoff (all the words collapse into one magenta-hatched
  exam sheet) → claim → end card.
- **Look:** paper ground with ink grid, condensed caps at poster scale, one
  magenta field for the payoff, no dark void.
- **Sound:** metronomic 120 bpm hits, one cut per half beat, effects dropped
  where the drums already hit, riser into the collapse.

## 3. The result first

- **Device / form:** the result first → one frame that rebuilds (reverse).
- **What you see and feel:** the film opens on the finished report — score
  chart drawing, incident timeline filling — then pulls back *through* the
  report into `ax report` in the terminal, back through the submitted sheets,
  the clock, the lobby QR, until the plain YAML folder that started it all
  lands on disk. Satisfying undo: you see where the grade came from.
- **First two seconds:** the report's score chart drawing itself, values
  counting up, already in motion.
- **Beats:** hook (report building) → tension (a question with similar wrong
  answers flagged — *how did this happen?*) → reveal (rewind: this came from a
  session you can watch) → proof (report → submit screen → answering → QR →
  YAML folder) → payoff (the folder opens into the exam's questions, real
  Markdown and math) → claim → end card.
- **Look:** same light-table materials, chart drawn in ink hairlines, magenta
  only on the flagged incident.
- **Sound:** warm reversed swell under the rewind, each step back on a beat,
  resolving to a quiet tick when the file lands.

---

**Recommendation: 1 — "One laptop, thirty screens"**, because a stranger
follows it in one read, it shows the real product on both sides (terminal and
browser), and it ends on the command a README reader can run.
