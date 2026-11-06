# SPEC — Assess README film

GOAL:    A stranger to the repo understands in one watch: a teacher starts an exam from the
         terminal, students join in a browser with nothing installed, answers save themselves,
         and the report comes back — and wants to run `go build ./cmd/ax`.
OUTPUT:  HyperFrames, 1920 x 1080, 30 fps, English titles over the product's real Spanish UI.
TIME:    34.0 s x 30 fps = 1020 frames. No music source available in this environment
         (no HeyGen sign-in, no catalog), so the film is silent: the clock is the motion itself,
         cuts land on actions, each state change happens on the frame its cursor clicks.
LOOK:    Medium: paper and ink on a light table — the product's own design system (web/DESIGN.md).
         ground:   #e8e6e1 flat, with a hairline grid every 96 px at rgba(20,20,20,.05); no vignette, no grain.
         ink:      #141414 text and strong rules; secondary #5b5a56; hairline rule #cdcbc5.
         sheet:    #f8f7f4 with one shadow `0 1px 2px rgba(20,20,20,.08), 0 18px 40px -8px rgba(20,20,20,.2)`;
                   sheets beneath tilt -1.4deg / +2.1deg; radius 0 everywhere.
         accent:   #4f46e5 — the example exam's own brand (`examples/algebra-01/manifest.yaml`
                   `branding.accent`), the colour the captured screens really use. Ration: one
                   highlighted word per title, the student's own answer in every screen, the
                   terminal caret. Never a field, never a background.
         display:  Sofia Sans Extra Condensed 800, ALL CAPS, tracking +0.14em — hero titles 128 px,
                   secondary lines 88 px, labels 26 px; never below 26 px.
         body:     Sofia Sans 400/600, 30 px minimum for anything meant to be read.
         mono:     JetBrains Mono 400, 30–34 px — every terminal and report line.
         light:    flat; the only depth is the sheet shadow and the terminal panel's
                   `0 18px 40px -8px rgba(20,20,20,.28)`.
OBJECTS:
         terminal panel — ink #141414 rectangle, mono transcript, accent block caret; no fake
                   window chrome, no traffic-light dots;
         sheet      — a screenshot sits on the desk as paper: full-bleed image inside a #f8f7f4
                   sheet with the design's one shadow, its edge visible on the ground;
         cursor     — drawn arrow, 60 px tall (5.5% of frame), #f8f7f4 fill, 2 px #141414 stroke,
                   enters from off-screen, clicks cause the next change on the same frame;
         paper stack— three plain sheets, ruled top row, for the "old way";
         spinner    — one 2 px ink arc, one finite rotation over its beat, inside a blank sheet
                   labelled only by the film's own words (no invented product, no competitor);
         phone sheet— the real 390x844 screenshot as a small sheet beside the desktop sheet.
MOTION:  The medium moves it: paper is placed and lifted (enter: power4.out 0.40 s, ±1.5deg
         settling to 0; exit: power2.in 0.35 s), ink is printed (top-down reveal by a ground-
         coloured cover scaling away, transform-origin bottom, ease none). camera: power3.inOut
         0.50 s on scale/translate of a wrapper, 1 hero move per shot; secondary moves travel
         60% as far and start 0.12 s later. cursor: power2.inOut 0.45 s, then the click. screen
         state changes are a cut on the click frame — never a cross-dissolve. type: lands once,
         never drifts, never fades up from below.
SHOTS:
  S1 hook      0.00–3.40  terminal prints `ax start` line by line (real transcript, session 3NY25X);
                          camera pushes from the whole panel to the Students URL + Code 3NY25X.
                          reads: `Exam server running in the background (pid 23744)` … `Code 3NY25X`.
                          hands off: the code line stays as the object.
  S2 tension   3.40–6.60  the code line is lifted away as paper: a stack of printed exam sheets
                          drops onto the desk, then a blank sheet with a turning arc and the film's
                          own words `NO INTERNET, NO EXAM`; both sheets are lifted out of frame.
  S3 reveal    6.60–8.90  grid rules draw across the empty desk; two lines print on:
                          `CREATE, RUN AND PROCTOR` / `EXAMS FROM THE TERMINAL`
                          (README line 1, verbatim), TERMINAL in accent.
  S4 join      8.90–13.30 real join sheet slides up (empty), cut to the filled form (code 3NY25X),
                          cursor clicks ENTRAR; cut to the pledge screen (the rules table,
                          `No se usa la cámara…`), cut to `ESTÁS DENTRO` lobby.
  S5 answer   13.30–19.10 the exam sheet lands full-frame (q03, workspace); camera pushes 1.8x
                          into the option table: veiled rows, cursor hovers A and the veil lifts
                          (s10), click → row A hatched + `ELEGIDA` (s11), click B → second row
                          hatched (s12); camera pulls back to the whole sheet with the rail filled.
  S6 phones   19.10–20.60 the desktop sheet shrinks to the left, the real phone screenshot lands
                          right; titles print: `ANY BROWSER, ANY DEVICE` / `NOTHING TO INSTALL`
                          (README line 1).
  S7 submit   20.60–24.20 review table sheet (all 6 RESPONDIDA), confirm dialog lands over it,
                          cursor clicks `ENTREGAR AHORA` → cut to the result sheet `8 / 9`.
  S8 report   24.20–28.60 the ink terminal returns: real `ax status` (session ended, submitted 1)
                          prints, then `ax report` prints top-down — summary, the SCORES chart
                          drawing in, `TO REVIEW` line; hold on `Mean 89 %`.
  S9 claim    28.60–30.80 sheets clear; two lines print: `ONE PORTABLE BINARY`
                          `NO CLOUD, NO ACCOUNTS, NO INSTALLS` (PRODUCT.md positioning),
                          BINARY in accent.
  S10 endcard 30.80–34.00 a sheet slides across the frame and wipes the wordmark `ASSESS`
                          in behind it (Sofia Sans Extra Condensed 800, +0.14em, lockup ~460 px
                          = 24% of frame width), then the command `go build ./cmd/ax` prints in
                          mono under it and holds 1.9 s.
BANNED:  a dark or navy void as the ground; glow, bloom, particles, lens flares; purple or blue
         gradients, glass cards; a cream-paper-serif-italic look with eyebrow labels and pill
         chips; everything centred; every element fading and sliding up; titles that keep moving
         after they land; three cards in a row, KPI grids, a title over a dimmed screenshot; the
         logo docked in a corner; invented browser chrome, an invented QR code, an invented
         dashboard; red or amber alarm colours; CSS filters on any screenshot; `repeat: -1`,
         `Date.now()`, `Math.random()`, network fetches.
QA:      f0     terminal on the desk, first line readable;
         f95   code 3NY25X fills the push-in;
         f170  paper stack + arc visible, no text clipped;
         f260  both title lines printed, inside the 80% title-safe box;
         f330  join sheet with code 3NY25X typed, ENTRAR under the cursor;
         f380  pledge rules table legible at 1080;
         f470  option row A hatched with ELEGIDA, prompt readable;
         f540  pull-back: rail shows answered rows, sheet square on the desk;
         f590  phone sheet and desktop sheet both fully visible, titles clear;
         f700  confirm dialog over the review table, tally readable;
         f740  result `8 / 9` centred in its sheet;
         f820  report chart drawn, `Mean 89 %` on screen;
         f900  claim lines inside title-safe;
         f980  ASSESS lockup + command, holds to f1020.
