# Example exams

Ready-made exams to try every feature of `ax`. Each one covers a different
case. Run the commands from the repository root. On Windows, use `.\ax.exe`
in PowerShell or `./ax.exe` in Git Bash.

| Folder | Language | Proctoring | What it shows |
|---|---|---|---|
| [`algebra-01`](algebra-01) | es | `standard` | Math (KaTeX), every question type, a question pool, a 90 s timed question |
| [`programacion-go`](programacion-go) | es | `strict` | Code blocks, required fullscreen, options revealed on hover, desktop only, 2 min timed question, review after the session, custom accent |
| [`ml-basics-en`](ml-basics-en) | en | `standard` | English UI, SVG diagrams from `assets/`, a pool, logo and accent |
| [`historia-ventana`](historia-ventana) | es | `standard` + overrides | Tables, lists, quotes, a map, 5 strikes, options always visible except on one question |
| [`uni-fisica-2020-1`](uni-fisica-2020-1) | es | `standard` | Eight real Physics questions from the UNI admission exam 2020-1 (Peru), from its official solutions booklet; fixed order, review after the session |
| [`uscis-civics`](uscis-civics) | en | `off` | Real questions from the official U.S. naturalization civics list; 10 of 20 per student, short text answers |
| [`quimica-secundaria`](quimica-secundaria) | es | `standard` + photo protections, windowed | The reading guide, the stillness warning, hover options; the lens off on a short question; a 2 min timed question with a long text |
| [`quimica-san-marcos`](quimica-san-marcos) | es | `standard`, windowed | Chemistry admission practice (UNMSM style), fixed order, visible options, review after the session |
| [`quimica-repaso-san-marcos`](quimica-repaso-san-marcos) | es | `off` | Quick chemistry review (UNMSM style), 12 one-line questions, 8 minutes, review with the mistakes at the end |
| [`cultura-general-san-marcos`](cultura-general-san-marcos) | es | `standard`, windowed | General culture admission practice (UNMSM style), fixed order, visible options, review with the mistakes at the end |
| [`quiz-rapido`](quiz-rapido) | es | `off` | No proctoring, fixed order, hidden score, 5 minutes (try the automatic end) |
| [`invalid/`](invalid) | — | — | Broken exams that `ax exam check` rejects with clear messages |

Profiles: `standard` and `strict` turn on every protection (pledge, curtain,
copy and paste blocked, capture shield, watermark, required fullscreen,
hover-only options, the lens). `standard` warns first and annuls at 3 strikes; `strict`
counts at once and annuls at the second strike. `off` turns everything off.

`go test ./internal/exam` checks that the valid examples stay valid and the
invalid ones keep failing.

## 1. Validate

```sh
ax exam check examples/algebra-01 examples/programacion-go examples/ml-basics-en examples/historia-ventana examples/quimica-secundaria examples/quimica-san-marcos examples/quimica-repaso-san-marcos examples/cultura-general-san-marcos examples/quiz-rapido examples/uni-fisica-2020-1 examples/uscis-civics
ax exam check examples/invalid/clave-incorrecta   # missing answer, unknown option
ax exam check examples/invalid/campo-desconocido  # typo in a key, bad duration
ax exam check examples/invalid/pool-injusto       # pool picks everything, unequal points
ax exam check examples/invalid/vigilancia-mal     # proctoring values out of range
ax exam check --json examples/invalid/pool-injusto
```

## 2. Run a session

```sh
ax start examples/programacion-go     # publishes, starts the server, opens the panel
```

The panel shows the link, the session code and a QR code (the **Enlace**
view). Open the link on another device on the same network (phone or laptop),
or use `http://localhost:27337/` on the same machine. Then join with the code,
a name and an ID.

In the panel:

- **S** starts the exam for everyone in the lobby.
- **Tab** switches views: Alumnos, Ranking, Incidencias, Enlace.
- **T** gives the selected student extra time, **+** gives everyone 5 minutes.
- **M** messages a student, **P** pauses or resumes them, **X** annuls, **U** readmits.
- **V** lets a student read prompts whole, without the lens, and gives it
  back (only in exams that use the lens).
- **⇧M** messages everyone, **⇧P** pauses or resumes everyone.
- **Z** reverts a penalty (it asks for a note).
- **N** adds a student, **D** removes one, **A/R** approves or rejects a device.
- **E** ends the exam: **Y** keeps the server running to see results, **A**
  also stops the server. The exam also ends by itself when everyone has
  turned in.
- **L** locks the panel, **Q** detaches (the exam keeps running).

```sh
ax status            # is the server running? (Running / Not running / Crashed)
ax session open            # reopen the panel (plain `ax` does the same)
ax session logs -f           # follow the server log
ax stop              # end the exam and stop the server
ax session results --sep ";" --out notas.csv   # CSV for Excel with a Spanish locale
ax session results --format json
ax session report            # once the exam ended: charts, questions, evidence (I in the panel)
ax session report --json > informe.json
```

To try several exams without mixing data, give each one its own data folder:

```sh
ax --data-dir ./data/quiz start examples/quiz-rapido --port 27338
```

## 3. Scenarios to try

**Integrity (`programacion-go`, `historia-ventana`, `algebra-01`)**

1. Join and type the pledge. "Comenzar" enters fullscreen.
2. Switch to another tab. The tab title counts down; after 3 s in `strict`
   (10 s in `standard`) it counts. Back on the tab you are out of fullscreen:
   the same absence ends when you press "Activar pantalla completa", and you
   see "Aviso 1 de 2" (`strict`) and lose half of the remaining time (20 %
   in `standard`).
3. Press Ctrl+C or PrintScreen on a question. In `strict` it is a strike and
   the question is voided; in `standard` the first time is only a warning.
   Hold the Windows key: the exam disappears until you let go.
4. Leave fullscreen with Esc. You have 3 s (`strict`) or 5 s (`standard`).
5. In the panel, open **Incidencias**, select the incident and press **Z**:
   the strike is reverted with your note, and the lost time comes back.
   Readmitting (**U**) brings the student back to the question they were on,
   without reloading.
6. The second strike annuls a `strict` exam; `standard` needs three (five in
   `historia-ventana`). Readmit with **U**: it leaves one strike of margin.

**Photos of the screen (`quimica-secundaria`).** A prompt is read in ranges of
words, one range at a time; each range repeats the last two words of the
previous one for context, and the first view reads them automatically. Tap the
zone you want to read, or use the reading controls, swipe or the keyboard.
Stay still for 20 s, then answer correctly: the
panel lists it under **Incidencias** ("quieto sin tocar ratón ni teclado").
In the panel, **V** on a student lets them read without the lens. q002 turns
the lens off. This example runs in a window: `fullscreen: off`.

**Hidden options.** In `programacion-go`, options show only while the
pointer is over them or they have keyboard focus (Tab). On a phone, the first
tap reveals an option and the second chooses it.

**Timed question.** Open the 2-minute question in `programacion-go`: it asks
before opening, shows a countdown bar, and locks at zero.

**Two tabs and two devices.** Open the exam link in a second tab: it is
blocked. Join with the same ID from another device: the panel asks you to
approve it (**A**).

**Crash and resume.** While an exam runs, kill the server (Task Manager, or
`taskkill /F /PID <pid>` with the PID from `ax status`). `ax status` then
reports *Crashed*. Run `ax session resume`: students reconnect and keep their answers.

**Time runs out.** Start `quiz-rapido` (5 minutes) and wait. Every attempt is
submitted with what was saved. The score stays hidden (`result_visibility: none`).

**Review.** `programacion-go`, `quimica-san-marcos`,
`quimica-repaso-san-marcos` and `cultura-general-san-marcos` use
`result_visibility: review`. After **E** ends the session, students see each
question marked correct or incorrect; every mistake shows the question, the
answer they gave and the correct answer.

**Phones.** `programacion-go` (`devices: desktop`) turns phones away with
"Usa un ordenador". The others work on phones.

## 4. Make your own

```sh
ax                    # menu: create, edit or start an exam
ax new                # step-by-step wizard, no files to learn
ax new mi-examen      # template with every question type
ax exam guide             # the file format: hand it to an AI to write exams for you
ax edit mi-examen     # forms for settings, questions and answers; E opens $EDITOR
ax exam check mi-examen
```

The format is described in [docs/format.md](../docs/format.md).
