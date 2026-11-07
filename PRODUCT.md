# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

- **Students (primary for the web app).** Secondary school and university,
  aged 14 and up, taking a timed exam in a classroom. They join from a school
  PC or their own phone through a link, QR code or short session code, and
  never install anything. The situation is stressful: a clock, a grade and
  visible anti-cheat rules. Their job is to read each question, answer, review
  and submit with confidence that nothing is lost.
- **Teacher (primary for the CLI/TUI).** Creates exams as YAML folders, starts
  a session from the terminal on their own laptop, supervises live and reads
  results. The teacher never uses the web app as an admin; administration is
  terminal-only.

## Product Purpose

Assess (`ax`) lets a teacher create, run and proctor exams from the terminal
for a classroom on the same local network. Success: every student can join
in seconds, answer without losing work (even through Wi-Fi drops or a server
restart), and receive a fair, server-enforced time limit and grade; the
teacher sees who is where and what happened, and keeps the last word on every
penalty.

## Positioning

A self-contained, open-source exam server in one portable binary: no cloud,
no accounts, no installs for students, and it works with no Internet at all.
Anti-cheat uses only what a browser exposes, with transparent and reversible
penalties instead of invasive proctoring (no webcam, no lockdown browser).

## Operating Context

- Classroom LAN; the teacher's laptop runs the server; plain HTTP by default.
- No Internet is guaranteed: every asset (fonts, math rendering, icons) ships
  inside the binary.
- Students use mixed devices: school desktops, laptops and phones.
- Exams are general purpose: any subject (programming, AI, sciences,
  languages, math…). Content is Markdown with images, **code blocks with
  syntax highlighting** (rendered offline) and math (KaTeX). Code and images
  must read well on a phone as well as on a desktop.
- Rituals: lobby wait → common start → timed answering → review → submit or
  automatic submission at time-out.

## Capabilities and Constraints

- Question types: single choice, multiple choice, true/false, short answer
  (number or text). One question per screen, free navigation, flag for review,
  per-question time limits optional.
- Server-authoritative timer with 10/5/1-minute warnings; extra time can be
  granted per student; autosave with an offline queue.
- Anti-cheat states the student can see: away-from-tab countdown, strike
  counter (3 strikes annul the attempt), privacy curtain on focus loss,
  required/suggested fullscreen, prompt read in word ranges, options revealed
  on hover, watermark with the student's name, second-tab block, annulled
  screen, typed honor pledge.
- Result after submission per exam: nothing, score, or full review after the
  session closes.
- Branding per exam: optional institution logo and accent color from the
  manifest.
- Technical: React + TypeScript + Vite + Tailwind v4 + shadcn/ui (Radix);
  initial JS ≤ 150 KB gz (excluding KaTeX), CSS ≤ 30 KB; self-hosted fonts.
- Languages: English (default) and Spanish, changed in Settings.
- The visual design system lives in `web/DESIGN.md` (not `docs/design.md`,
  which is the architecture record). The teacher's terminal has its own
  system in `internal/tui/DESIGN.md`, which inherits that world.

## Visual Constraints (from the user)

- Bold, polished and memorable; never generic, never form-like. It must not
  resemble Moodle, Google Forms, Classroom or Kahoot.
- Calm to read for a whole exam: it must not tire the eyes or add pressure.
- Students should enjoy using it.
- No visual world is pinned yet. A Wim Crouwel–style gridded specimen was
  liked as a reference for the level of impact, not chosen as the direction.

## Brand Commitments

- Name: **Assess** (CLI `ax`). Shown discreetly; the exam and the
  institution's branding are the protagonists.
- Voice: Spanish uses **tú**; close, calm and plain. Rules are stated firmly
  and never hostile; penalties are explained, not threatened.
- Identity: **Registro** (see `branding/`): the name on a record rule, closed by
  the magenta block. It never competes with the exam or the institution's mark.

## Evidence on Hand

- Example exam: `examples/algebra-01` (Spanish, algebra, math notation).
  Design work must also use non-math content (code, images).
- No users, testimonials, institutions or metrics exist; none may be
  fabricated.

## Product Principles

1. **Never lose a student's work.** Saving is automatic and visible; offline
   and restarts are expected, not exceptional.
2. **Calm under a clock.** Every screen lowers anxiety: clear state, gentle
   warnings, no surprises.
3. **Transparent rules.** Students know what is recorded and what each
   penalty costs before they start.
4. **The teacher has the last word.** Automatic measures are reversible and
   shown as signals, not verdicts.
5. **Portable and offline.** One binary, no network dependency, light enough
   for 100 students on a laptop over Wi-Fi.

## Accessibility & Inclusion

- WCAG 2.2 AA baseline: keyboard operable, visible focus, 4.5:1 text contrast,
  reduced-motion respected, zoom to 200% without loss.
- Per-student accommodations set by the teacher from the TUI: disable hover
  reveal and privacy curtain (screen-reader and low-vision users), extra time.
- Options revealed on hover must also reveal on keyboard focus and
  press-and-hold on touch.
