---
name: Assess · Mesa de luz, terminal
description: The teacher's terminal panels, the same world as the student app translated to a cell grid.
extends: web/DESIGN.md
target: Go, Bubble Tea v2, Lip Gloss v2, ntcharts
status: The design is approved. Every screen of the build order is built; the new capabilities come next.
colors:
  light:
    bg: "#e8e6e1"          # the table: screen ground
    surface: "#f8f7f4"     # the sheet: work area
    surface2: "#f0efeb"    # sheet beneath: selected rows, submitted tiles, key bar
    ink: "#141414"
    ink2: "#5b5a56"        # labels, secondary text
    ink3: "#8f8d88"        # pending marks only, never text
    rule: "#cdcbc5"
    ruleStrong: "#141414"
    accent: "#ec2a8a"
    accentInk: "#a30e58"   # magenta as text
    inverse: "#141414"
    onInverse: "#f8f7f4"
    onInverse2: "#c9c7c1"
  dark:
    bg: "#111110"
    surface: "#1c1c1a"
    surface2: "#232321"
    ink: "#ecebe6"
    ink2: "#a7a59f"
    ink3: "#6f6d68"
    rule: "#353431"
    ruleStrong: "#bcbab4"
    accent: "#ff4fa3"
    accentInk: "#ff7ab9"
    inverse: "#ecebe6"
    onInverse: "#141413"
    onInverse2: "#4a4945"
  fixed:
    qrLight: "#f8f7f4"     # QR keeps these in both themes
    qrDark: "#141414"
grid:
  sizes: "120×36 is the reference; 80×24 must always work; below 72 columns ask for a wider window"
  rows:
    strip: "3 rows plus a strong rule at ≥100 columns, 2 rows plus a rule below"
    tabs: 1
    keys: "1 row, or 2 when the keys do not fit"
---

# Design system: Assess in the terminal

This file is the source of truth for every teacher panel in `internal/tui`. Read it before adding or changing a screen. The visual world and its reasons live in `web/DESIGN.md`; this file only records how that world lands on a cell grid. If the two disagree about colour or rules, `web/DESIGN.md` wins and this file gets fixed.

To see it, open [design/canvas.html](design/canvas.html) in a browser. It holds the components, the main screens, the flows and a working keyboard prototype, drawn on a real cell grid. All its data is synthetic.

## Principles

1. **One product.** Someone who knows the student app should recognise this at a glance: same palette, sheets on a table, ruled tables, no rounded corners.
2. **The Yours-Only Rule.** Magenta marks what belongs to the teacher: the cursor, focus, and what waits for their decision. It never colours data, charts or decoration.
3. **The Reversed-Plate Rule.** Serious states invert to ink: 5 minutes or less, penalties, annulment, no connection. No red, no amber.
4. **Nothing depends on the exact drawing.** See [Terminals are not equal](#terminals-are-not-equal).
5. **State is never only colour.** Every state also has a symbol, a word or an inversion.
6. **Signals, not verdicts.** The teacher has the last word. Indicators of cheating say what was seen and never accuse.
7. **The same keys.** Redesigns keep the existing shortcuts.

## Terminals are not equal

Fonts, line spacing and glyph coverage change from one terminal to another, and the design has to survive that without configuration.

- **Every chart carries its numbers.** A bar is a quick read; the printed value is the data.
- **Bars leave a gap.** Horizontal bars use `▄` (half a cell), and unit stacks use `▄` per row, so no solid block depends on rows touching. A font that opens a line between rows looks the same.
- **Hatching is decoration.** `▒` always comes with a bold or coloured word next to it, so it can render as dots, as a flat tone or as noise.
- **Fixed columns for symbols.** Glyphs from levels 2 and 3 may be ambiguous width in East Asian locales; they sit in reserved cells with free space beside them.
- **Own the colours.** Paint every cell's background. Never inherit the terminal's, so a light or dark profile cannot break contrast. Pick the theme from the detected background.
- **16 colours still work.** Hierarchy has to hold with bold and inversion alone. Surfaces may collapse into one colour.

### Glyph levels

| Level | Glyphs | Rule |
|---|---|---|
| 1, safe | `█ ▀ ▄ ▌ ▐ ░ ▒ ▓ ─ │ ┌ ┐ └ ┘ ■ ·` | Use freely |
| 2, normal | `▁ ▂ ▃ ▅ ▆ ▇ ● ○ □ ▸ ↑ ↓ ← →` | Need a fallback |
| 3, ornament | `✓ ⇧ ↩ ╱` | Always need a fallback, and never carry meaning alone |

Fallbacks, for consoles that lack the glyph:

| Glyph | Fallback | Glyph | Fallback |
|---|---|---|---|
| `●` | `*` | `✓` | `v` |
| `○` | `o` | `⇧` | `^` |
| `□` | `.` | `↩` | `<` |
| `▸` | `>` | `╱` | `/` |
| `↑ ↓` | `^ v` | `← →` | `< >` |
| `▁ ▂ ▃` | `▄` | `▅ ▆ ▇` | `█` |

Also allowed: printable ASCII and Spanish punctuation and letters (`áéíóúüñ ¿¡ º ª · — – « » … × ± −`). Banned: emoji, double-width characters, and rounded box corners.

## Translation from the web

| Web | Terminal |
|---|---|
| Sheet on a table | `surface` block on `bg` |
| Sheet askew beneath | Straight strip of `surface2`, offset one cell. Tilt does not exist |
| Shadow | None. The change of background carries depth |
| Sofia Sans, condensed caps | The terminal's font. CAPS and bold for labels, no tracking |
| 46 px clock | Three-row digits, half blocks, 3 cells wide plus a gap |
| Magenta hatch on your answer | `▒` in `accent`, plus bold `accentInk` text |
| Tone swatch | Ink fill and `·`: answering darkens the record |
| Three strike squares | `■□□` |
| Isolated submit block | Inverted block for the one main action |
| Sheet slide with mass | Instant change on key press |
| Hover | Does not exist. Everything is reachable by keyboard |

## Components

Sizes are in cells. Each exists in the canvas with its variants.

- **Strip.** Top band on `surface` ending in a `ruleStrong` line. Exam title and class on the left, then cells with the value above and a word below, split by `rule`. The clock sits right. A label never sits above a big number.
- **Big digits.** Three rows. At 5 minutes or less the cell inverts. Below 100 columns the clock is plain bold text.
- **Tabs.** The active one is `surface` on `bg`, bold. The others are `ink2`.
- **Key bar.** `surface2` band at the bottom. Keys are inverted caps; labels are `ink2`. Selection actions first, session actions after. Wraps to two rows.
- **Contact tile.** 23×4 at ≥100 columns, 19×2 below. Name, tone strip, position and strikes. `surface` while answering, `surface2` once submitted or not joined, inverted when annulled. Selected: `▒▒` and `accentInk` name.
- **Table row.** Full bleed with `rule` hairlines or none. Normal, selected (`surface2` plus `▒` plus `accentInk`), inverted.
- **Request band.** `▒▒`, a bold sentence and its keys. It marks what waits for the teacher.
- **Field.** Question in bold, value, `▌` caret in `accent`, then a hint in `ink2`.
- **Notice.** One line starting with `✓`, above the keys, until the next key.
- **Plate.** Inverted block with a question, a calm explanation and keys. Always for destructive actions.
- **Action block.** Inverted block for one main action, isolated from navigation.
- **Option row.** Key cell, text, `rule` below. The ruled table from the student app, not cards.
- **Code cells.** Six ruled cells for the session code.
- **Choice chips.** One short option per chip, moved with `←→`. The active one is inverted. Use them for up to five options that fit on a line.
- **Inline field.** `surface2` band with a bold value and the `▌` caret.
- **Check row.** `✓` and a sentence; what is missing inverts and starts with `!`.
- **Panel.** Box over the current screen with one task and its outcome stated. Side panel for the student card, so the class stays in view.
- **Action list.** Panel over the current screen with the actions of the selected row. Destructive actions stay last and always open a plate.
- **QR.** `qrterminal` half blocks with fixed `qrLight` and `qrDark`, so it scans in both themes.

### Charts

Grey by rule: `ink` for the main series, `ink2` for secondary. Magenta only marks the cursor.

| Chart | Use | How it is drawn |
|---|---|---|
| Unit stack | How many in each band | One `▄▄▄▄` per student, the count above. Above 8 per band, scale it and say so |
| Scatter | Two values per person | `●` on a grid. A digit replaces it when points share a cell |
| Margin | Distribution under an axis | `▁`…`█` along the axis |
| Half bar | Comparing rows | `▄`, whole cells, number beside it |
| Stacked | Warnings and serious signals | Half cells on the grid, `ink2` and `ink` |
| Quadrant | Item analysis | Dots, flagged items by name, a labelled zone per axis |
| Dumbbell | Before and after | `○───●` on one axis, both values printed |

Panels draw on `grid` (`grid.go`, `widgets.go`), the same cell model as the canvas: painting cells keeps backgrounds whole, which nested `Render` calls cannot do. `ntcharts` remains for the plain-text report (`ReportText`). Do not add a chart dependency.

## Screens

### Live session: Hoja de contactos

The class at a glance. Each student is a frame: answering darkens it, and what needs attention inverts. Strip, tabs (Alumnos, Incidencias, Clasificación, Enlace), request band, view bar, tile grid, key bar. Data comes from `daemon.Board`. The arrows move in two dimensions.

**Large classes.** The product targets 100 students, so the sheet adapts to what is on screen, not to the class size:

| Density | Tile at ≥100 columns | Tile below | Fits at 120×36 |
|---|---|---|---|
| Large | 23×4, five per row | Not used | 25 |
| Medium | 19×2, six per row | 19×2, four per row | 78 |
| Compact | 29×1, four per row | 39×1, two per row | Over 100, then scrolls |

- **View bar.** `TODOS`, `ATENCIÓN`, `RESPONDIENDO`, `ENTREGADOS`, `SIN ENTRAR`, each with its count. `F` cycles, `1`–`5` jumps. Filtering a few students brings back large tiles.
- **Attention** means strikes, offline, paused or annulled. Above 25 students these go first in their own section, at most two rows, with `+N más`; the rest scroll beneath.
- **Stability.** At 25 or fewer, tiles never change place, because the teacher learns where each student is. Above that, stability gives way to not missing a problem.
- **Dónde va la clase.** A small chart of how many students are at each stretch of the exam, from `Answered`.

Rejected structures, so nobody repeats the exercise: **Partitura** (a strip per student and a column per question) needed per-question data that `StudentRow` lacks; **Riel + hoja** (the student's layout) showed the class only as a list.

### Live actions

`Enter` on a tile opens the student's card as a side panel, so the class stays in view. The card shows position, strikes, what happened and what can be done. `T`, `M`, `Z` and `U` open small panels, with the outcome stated before confirming: *Terminará a las 11:02 en vez de a las 10:52*.

Two truths from `internal/session` that the screens must state:

- **Readmitting does not give time back.** The deadline is absolute, so the panel offers extra time in the same step.
- **Reverting a strike does not lift an annulment.** Readmitting is a separate action.

### Fixing and sharing

The report finds the problem; these screens solve it. `Enter` on a question offers: void it for everyone, give everyone the point, accept another answer, change the correct answer, or edit it for next time. **Nothing applies before showing who it affects**: new mean, how many grades go up and down, and the students most affected. It can be undone, it stays on the record, and students who saw their grade see the new one.

Export offers a web page, Excel, CSV or text. It states that the file is not encrypted and how many students it names. Indicios are off by default, because they are unverified signals.

Comparing groups uses sessions of the same exam and shows where they differ most. It warns that two groups are not comparable by themselves.

### Question types

All types share one structure: type, prompt, answer. Only the answer changes.

| Type | Answer | Note |
|---|---|---|
| Multiple | `Space` marks several | All or nothing, or partial, explained in words |
| True or false | Two halves, `←→` | As in the student app |
| Short, number | Value and margin | Says what is accepted: *Se acepta exactamente 4* |
| Short, text | List of accepted forms | Ignore case, accents, extra spaces. Shows examples of what counts |
| Pool | Pick N of M | All must be worth the same, so nobody gains by chance |

Terminals cannot draw images or formulas. The editor shows the source, the student view shows a labelled placeholder, and `P` opens the true student view in the browser. Never imitate them with ASCII art.

### Locks

Both protect students' data, so they show only what is essential. **Locking never stops the exam.** The locked panel shows the time left and two counts, and never a name. The closed vault shows only the password field. A wrong password states what happens next. Recovery happens on the same screen with the recovery key and a new password.

### First use

From the first `ax` to a ready exam. It must never be a wall of setup: one action per screen, and leaving halfway is fine.

Bienvenida, Crear la bóveda, Clave de recuperación, Bóveda vacía, then the exam editor. The vault is encrypted by default because it holds students' data; `Ctrl+U` skips the password. The recovery key is shown once, as today, so its screen waits for `Enter`. The empty Inicio offers four starts, and every empty section says what it is for and offers its first action. Revisar ends on *Probarlo yo*, to rehearse in a browser without students.

### Before the lobby

What fails should fail here, before 30 students are waiting. Preparar sesión chooses exam, group, who may enter and duration, and checks the exam, the network, the firewall and the group. Opening the room only lets students in; the clock starts with `S`.

A problem is solved where it appears: the firewall offers `A` to open the port, `I` for a script for IT, `H` for help, and `Enter` to carry on anyway.

If nobody has joined after 2 minutes, an inverted band offers help without blocking anything. **Los alumnos no pueden entrar** is an ordered guide: network, firewall, try from a phone, and network isolation, with the QR and other addresses beside it. The firewall state comes from `firewall.Check`; outside Windows that step does not appear.

### Recovering a session

The most stressful moment in the product, so the order is fixed: first reassure with numbers, then explain, and only then offer the action.

*Tu examen está a salvo*, what was saved, how long it stopped, and what happened meanwhile. **It must say that the clock did not stop**: the end time is absolute (`docs/design.md`), so the screen offers to give the lost time back to everyone, preselecting the minutes it was down. `Enter` resumes, and a line then counts students reconnecting. Terminating opens a plate. No red, no alarm, and technical detail stays in a side column and behind `L`.

If the time ran out while it was down, ask for extra time before going on. Never hand in work without saying so.

### Report

Summary first, then the proof. Every chart answers a question the teacher has, with data `internal/report` already computes. The views are the five that exist: Resumen, Preguntas, Incidencias, Indicios, Historial. `←→` or `1`–`5` change views, `↑↓` moves through questions and pairs, `O` sorts questions by difficulty.

| View | Question | Answer | Data |
|---|---|---|---|
| Resumen | How did it go? | Big mean and median, range, spread, time, and *Lo primero*, with three findings and the key that opens each | `Summary` |
| | How are grades spread? | Unit stack with the median marked | `Scores` |
| | Did time and grade go together? | Scatter with the time distribution underneath | `Students[].Percent`, `Minutes` |
| | Who needs a look? | Table of students with strikes or signals | `Students` |
| Preguntas | Which questions failed? | Half bars with percent, blanks and discrimination | `Questions` |
| | Which questions are bad? | Quadrant of difficulty and discrimination, then the selected question with a plain-language diagnosis on a plate | `Rate`, `Discrimination`, `Leak` |
| Incidencias | When, what and who? | Totals, stacked timeline, kinds, students with most signals | `Incidents`, `Students` |
| Indicios | Should I look at anyone? | The disclaimer first. Pairs with same errors against the number expected by chance, then why each one is flagged and the away hits | `Pairs`, `AwayHits` |
| Historial | Is this session normal? | Sessions with mean and median, and leaked questions as before and after | `History`, `Leaks` |

The report never invents a threshold: there is no pass mark in the data, so it never says how many passed. *Lo primero* is computed, and shows nothing it cannot back up.

### Exam: create and edit

Creating and editing are one screen. A new exam is simply an empty one, so the teacher learns one place. Replaces the separate `Wizard` and `Editor` models.

The promise: **nothing is lost, and nothing stops you.** It saves itself, shows `✓ guardado 10:42`, and an incomplete question is allowed. It only blocks starting the exam, and the Revisar step takes you to each problem.

| Step | Job | Key decisions |
|---|---|---|
| 1 Datos | Title, group, duration, what the student sees at the end | Four short fields with defaults, and the student's view beside them |
| 2 Preguntas | List, read and write questions | Rail with `▀▀` ready and `··` incomplete, sheet with the question exactly as written, totals by type |
| Writing | One question | Type chips, multi-line prompt, options with `Space` to mark the right one, live student view. `Esc` is *listo*, never *discard*. `Ctrl+E` opens the external editor for long Markdown, code or math |
| Pegar | Many questions at once | Paste on the left, what was understood on the right. Nothing is added until `Enter`, and incomplete questions come in marked. Teachers already have questions in documents or from an AI |
| 3 Reglas | Proctoring | Three presets in plain words, with what students will notice and what the teacher will see |
| 4 Revisar | Is it ready? | Totals and the start block on the left, the to-do list on the right, each item whole and in plain words. The start block stays disabled, with the reason, until nothing is missing |

The correct answer uses magenta because it is the teacher's own mark: the same treatment as *tu respuesta* for the student.

Keys: `Tab` next step, `N` new, `V` paste, `Enter` edit, `?` the rest (`D` delete, `E` external editor, `⇧↑↓` move). Only what a beginner needs stays on the key bar.

### Vault hub

Home, explorer and vault become one screen with six sections, so the teacher never needs a separate command. Replaces the `huh` menu in `internal/cli/home.go` and the tabs of `Explore`, and absorbs `ax vault`, `ax backup new`, `ax class`, `ax student` and `ax class grades` for interactive use. The commands stay for scripts.

| Section | Job | Shows |
|---|---|---|
| Inicio | What do I do now? | Actions ordered as `homeOptions`, and recent exams |
| Exámenes | Manage exams | Table with sessions and last mean, and the selected exam's trend and questions to review |
| Grupos | Manage classes | Rail of groups and each group as a contact sheet of students with their mean |
| Alumnos | Find a student | List and record: mean, sessions, warnings, note |
| Sesiones | Find a result | Table of sessions; `Enter` opens the report |
| Bóveda | Keep data safe | Name, state, size, backups, password, switching vault, USB, settings |

Rules of the hub:

- **Type to filter, as today.** Lists have no letter shortcuts. Actions live behind `Enter`, `Ctrl+N` adds, `Esc` clears the filter or goes back. Sections without a filter (Grupos, Bóveda, Libro de notas) may use letters.
- **`/` searches everything**: exams, groups, students, sessions and question text.
- **Preview before bulk changes.** Importing a list shows new, unchanged and broken rows first, and states what happens to broken ones.
- **Show the cost before a loss.** Forgetting a student states the sessions and groups affected, on a plate.
- **Libro de notas** is a table of students by exam with a final mean. Below 50 % goes bold, as a reading aid, not a pass mark. `X` and `C` export.
- **The group's contact sheet reuses the live tile**, so the class looks the same during the exam and after it.

### What exists and what is new

Design may go ahead of the code. This table keeps that honest.

| Piece | Today | Needs |
|---|---|---|
| Exam fields, validation, external editor | Yes, on the new screen | Fields and chips are drawn on the grid: `textField` replaces `huh` here |
| Autosave of a new exam | Yes: the folder is created with the first title | Group in Datos: an exam has no group yet, so the screen shows language and instructions instead |
| Live student view | Yes, on the sheet, from wide layouts up | |
| Pegar varias preguntas | Yes: `V` on Preguntas, parser in `paste.go` | Numbered or blank-line separated; `a)` or `-` options, `*` marks the right one, `R:` a short answer, `*V`/`*F` true or false. `Alt+Enter` types a new line, since `Enter` adds |
| Exámenes, Grupos, Alumnos, Sesiones | Yes, on the hub | Question counts and durations in the exam table need reading each package |
| Group mean, per-exam trend | Yes: group mean is the mean of its sessions | Questions to review per exam needs reports |
| Libro de notas on screen | Yes | |
| CSV import preview | Imports directly | Dry run that reports each row |
| Density and view bar | No | Layout only, from `daemon.Board` |
| First-run flow | Yes, over `vault.Init`; encrypted unless `Ctrl+U` | Choosing the folder; an older ax still migrates as before |
| Example exam | Not in the binary | Embed one |
| Probarlo yo | Yes: `P` on Revisar, *Probarlo yo* on an exam of the hub, `ax exam try [exam]` | A session in a throwaway database, served on `127.0.0.1` only; ends on `Enter` with the grade, and nothing reaches the vault. Without an exam it rehearses the example exam |
| Pre-flight checks | Yes, on Preparar sesión | |
| Open the port from the TUI | Yes, `A` on Preparar sesión | |
| Recovery screen | Yes, from Inicio; time down from the last heartbeat | The count of saved answers; `ax session resume` stays plain for scripts |
| Give time back on resume | Yes, `ExtendSession` before the server restarts | |
| Student card and panels | Actions exist as keys | Layout, over `AttemptAction` and `RevertPenalties` |
| Time back on readmit | Separate actions | Do both in one step |
| Void a question for everyone | Yes: `Enter` on Preguntas in the report; out of the grade and the maximum | |
| Accept another answer later | Yes, as a recorded correction; the published key stays as it was | |
| Impact preview | Yes, `PreviewCorrection` | |
| Report export | `--json`, text and results CSV | Web page and report Excel |
| Compare groups | Yes: *Comparar los grupos* on an exam of the hub, `ax exam compare <exam> [class...]` | Each class's latest finished session, mean and median, questions by gap, and the warning that classes are not comparable by themselves. `report.Compare` holds the numbers |
| All question types, pools, tolerance, partial | Yes | New layout |
| Student view in the browser | Yes, through Probarlo yo | |
| Locked panel | Yes | Layout |
| Closed vault, recovery | Commands | Screen over `vault` |
| Help on every screen | Only in the wizard | Generate it from each screen's key table |
| Settings | Yes, on the grid | |
| Duplicate an exam | Yes, `Draft.Rename` over a copy | Screen instead of a form |
| Import and export `.examx` | `ax exam import`, `ax exam export` | Screens over `examx` |
| Search in question text | `ax find` covers students, groups, exams and sessions | Add question text |
| Backups, password, switching vault, USB | Yes, as commands | Screens over those functions |

## State rules

Draw only the main states. Derive the rest from here. If a case is missing, add it here instead of improvising on the screen.

| Condition | Treatment |
|---|---|
| Empty | `ink2` line with the next action. No illustration |
| Connecting | One `ink2` line in the sheet. The strip stays put |
| No connection to the daemon | Inverted strip, data keeps its last time |
| Locked | Empty sheet with counts and password field. No names |
| Nobody joined after 2 minutes | Band with the network help, leading to Enlace |
| 5 minutes left | Clock on a plate |
| Last minute | Inverted clock with a magenta base |
| Paused | The word *en pausa* in the tile. The time keeps running |
| Student offline | `○` and the word |
| Submitted | `surface2`, strip in `ink2`, and grade |
| Annulled or withdrawn | Inverted, with the cause and the readmit key |
| Session ended | `TERMINADO` replaces the clock, and the key opens the report |
| View with no data | Header plus one `ink2` line |
| Server error | One-line plate above the key bar |
| After an action | `✓` line above the keys until the next key |
| Long text | Only names in a list or rail are cut with `…`, measured in cells. Anything to be read, such as hints, problems and notes, wraps |
| Saving | `✓ guardado 10:42` in the header |
| Incomplete data | `!` in the row; Revisar leads to each problem |
| Vault locked | Only the password field, with nothing behind it |
| Irreversible action | Plate stating the loss in numbers |
| First use | One clear action per screen; nothing must be finished |
| Over 25 students | Attention first, the rest below and scrollable |
| Empty filter | `ink2` line; `F` returns to Todos |
| Reconnecting after resume | A line with the count until everyone is back |
| Time ran out while stopped | Ask for extra time before going on |
| Main action unavailable | Block stays, grey, with the reason |

## Layout by size

| Condition | Rule |
|---|---|
| ≥100 columns | Three-row strip, big digits, QR, charts side by side |
| 72–99 columns | Two-row strip, clock as text, one chart per view, two rows of keys |
| <72 columns | Ask for a wider window. The exam keeps running |
| ≥30 rows | Gaps between rows and detail panels |
| <24 rows | The active view only, with scrolling |

Measure text in cells with `charmbracelet/x/ansi`; `%-24s` miscounts accents and CJK.

## Help, settings and exam management

**Help.** `?` works on every screen and shows only that screen's keys, grouped. It is generated from the same table that handles the keys, so it cannot go stale. The key bar shows what a beginner needs; `?` shows the rest.

**Settings** live in the Vault section, with a rail of the groups from `config.Groups()`. Each setting explains itself and gives its name in the file. Changes save when going back, as today.

**Exam management** lives in the exam's action list. Duplicate copies questions, answers and rules, never sessions. Export says that the file carries the correct answers, and offers to leave them out (`examx.Options.NoKey`) or add a password (`examx.Seal`). Import previews the file before adding it, and replacing an exam says that its sessions are untouched.

## Language

**English is the default**, as `language` in `internal/config` already says. Spanish is built in, and Settings changes it. The canvas opens in English and has a button for Spanish.

- Every label fits both languages. English is often longer: the validator found real overflows in it that Spanish hid.
- Never concatenate a translated word to a column; measure after translating.
- Names follow `locales/en.yaml`: *Classes*, *Evidence*, *strikes*. Spanish keeps *grupos*, *indicios*, *avisos*.
- A new string goes in every catalogue in `locales/`, and in `design/en.json` when it appears on the canvas.

The canvas source is written in Spanish and translated by `design/en.json`. To find what is missing, set `window.__cap = new Set()`, clear `TRC`, redraw, and read what was captured.

## Decisions

| Question | Decision |
|---|---|
| Grades after the session closes | Can be corrected, with a preview of who changes, a record, and undo |
| What students see after a correction | The new grade |
| New vaults | Encrypted by default, with the option not to |
| Interface language | English by default, Spanish in Settings |

## Build order

1. Done: foundations in `theme.go`, `grid.go` and `widgets.go`.
2. Done: live session in `monitor_view.go`, with density, filters, card, help and prompts.
3. Done: report in `report_view.go`, with the five views. Export comes with the new capabilities.
4. Done: exam editor in `editor.go`, `question_edit.go`, `editor_view.go` and `textfield.go`, joining `Wizard` and `Editor`. Pegar is done in `paste.go`; the student view in the browser comes with the new capabilities.
5. Done: vault hub in `explore.go`, Settings in `settings.go`, help on those screens, duplicate and export. The commands behind the actions still ask through `huh` forms outside the hub. Import preview, questions to review, USB and question text in `/` come with the new capabilities.
6. Done: first use, Preparar sesión and recovery in `setup_view.go`, wired in `internal/cli/setup.go`. Probarlo yo is done in `internal/cli/try.go`. The nobody-joined band, the network guide, the firewall script for IT and the reconnection count come later.

**After that**, because they are new capabilities rather than screens: correcting grades (done, in `internal/session/correction.go`), Pegar varias (done, in `paste.go`), Probarlo yo with the example exam (done, in `internal/cli/try.go`), and comparing classes (done, in `internal/report/compare.go` and `internal/cli/compare.go`).

## Adding a screen

1. Name the task, the data and the states.
2. Compose it from existing components. A new one goes in this file and in the canvas.
3. Check the two sizes, both themes, and 16 colours.
4. Keep magenta for the cursor and what waits for a decision. Invert what is serious.
5. Reuse existing keys. Put the selection's actions first in the key bar.
6. Add text to every catalogue in `locales/`.
7. Update the canvas when a screen or a rule changes, and run the build below.

## The canvas

| File | What it is |
|---|---|
| `design/canvas.html` | The built page. Open it directly; it works offline with a fallback font |
| `design/canvas.src.html` | The source: engine, validator, components, screens and prototype |
| `design/build.js` | `node internal/tui/design/build.js` writes `canvas.html` |
| `design/_cells/` | Real `ntcharts` and `qrterminal` output. Ignored by `go build ./...` |

The canvas validator rejects anything a terminal cannot draw: glyphs off the list, double-width characters, colours outside the tokens, and text outside the grid. Its *Terminal difícil* button shows the fallbacks and gaps between rows. *16 colores* shows the palette reduced to the standard Windows console colours.

To refresh the real cells: `cd internal/tui/design/_cells && go run main.go`, which writes `cells.json` next to it.
