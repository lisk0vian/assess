---
name: Assess · Mesa de luz
description: Student exam app as a stack of exposure-record sheets — neutral zones carry structure, one magenta marks what is yours.
colors:
  table: "#e8e6e1"
  sheet: "#f8f7f4"
  sheet-under: "#f0efeb"
  code-ground: "#efeee9"
  ink: "#141414"
  ink-secondary: "#5b5a56"
  ink-decorative: "#8f8d88"
  rule: "#cdcbc5"
  accent: "#ec2a8a"
  accent-text: "#a30e58"
  dark-room: "#111110"
  dark-sheet: "#1c1c1a"
  dark-sheet-under: "#232321"
  dark-code-ground: "#161615"
  dark-ink: "#ecebe6"
  dark-ink-secondary: "#a7a59f"
  dark-rule: "#353431"
  dark-rule-strong: "#bcbab4"
  dark-accent: "#ff4fa3"
  dark-accent-text: "#ff7ab9"
typography:
  label:
    fontFamily: "Sofia Sans Extra Condensed, Arial Narrow, sans-serif"
    fontSize: "13px"
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: "0.14em"
  clock:
    fontFamily: "Sofia Sans Extra Condensed, Arial Narrow, sans-serif"
    fontSize: "46px"
    fontWeight: 800
    lineHeight: 0.85
    letterSpacing: "0.02em"
  numeral:
    fontFamily: "Sofia Sans Extra Condensed, Arial Narrow, sans-serif"
    fontSize: "30px"
    fontWeight: 800
    lineHeight: 0.9
  prompt:
    fontFamily: "Sofia Sans, system-ui, sans-serif"
    fontSize: "34px"
    fontWeight: 700
    lineHeight: 1.2
    letterSpacing: "-0.015em"
  prompt-mobile:
    fontFamily: "Sofia Sans, system-ui, sans-serif"
    fontSize: "20px"
    fontWeight: 700
    lineHeight: 1.35
  body:
    fontFamily: "Sofia Sans, system-ui, sans-serif"
    fontSize: "18px"
    fontWeight: 400
    lineHeight: 1.55
  small:
    fontFamily: "Sofia Sans, system-ui, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.45
  code:
    fontFamily: "JetBrains Mono, ui-monospace, monospace"
    fontSize: "16px"
    fontWeight: 400
    lineHeight: 1.75
rounded:
  none: "0px"
spacing:
  s1: "4px"
  s2: "8px"
  s3: "12px"
  s4: "16px"
  s5: "20px"
  s6: "24px"
  s8: "32px"
  s10: "40px"
  s12: "48px"
  sheet-pad: "46px"
  sheet-pad-mobile: "18px"
components:
  button-primary:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.sheet}"
    typography: "{typography.label}"
    rounded: "{rounded.none}"
    padding: "0 20px"
    height: "50px"
  button-secondary:
    backgroundColor: "{colors.sheet}"
    textColor: "{colors.ink}"
    typography: "{typography.label}"
    rounded: "{rounded.none}"
    padding: "0 20px"
    height: "50px"
  button-secondary-hover:
    backgroundColor: "{colors.sheet-under}"
  input:
    backgroundColor: "{colors.sheet}"
    textColor: "{colors.ink}"
    rounded: "{rounded.none}"
    padding: "0 14px"
    height: "52px"
  option-row:
    backgroundColor: "{colors.sheet}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    height: "56px"
  option-key-selected:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.sheet}"
    size: "36px"
  submit-block:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.sheet}"
    padding: "16px"
---

# Design System: Assess · Mesa de luz

Source of truth: `web/src/styles/tokens.css` (tokens, both themes) and `web/src/styles/components.css` (components), used by the React app. The static specimen of every component and state is `web/prototype/components.html` (a snapshot of the same CSS); screens are `question-desktop.html` and `question-mobile.html` (append `?theme=dark`). The direction contract lives in `.impeccable/surfaces/web.md`. The teacher's terminal inherits this world; its translation to cells is `internal/tui/DESIGN.md`.

## Overview

The exam is a stack of exposure-record sheets on a light table. Neutral "zones" (paper, sheet, ink) carry every bit of structure: header cells, the question rail, the ruled answer table. A single dichroic magenta appears only on what belongs to the student: their answer and their position. Everything else stays quiet so a timed exam reads calmly for 45 minutes.

Light theme is the light table, used in a lit classroom. Dark theme is the darkroom: the same sheets with the light off, the magenta brightened to keep its contrast. Serious states (time running out, penalties, annulment) never turn red. They invert to ink, like a reversed plate.

Key characteristics: square sheets with one soft shadow; 1px ink rules; tracked condensed capitals for every label, numeral and action; the next sheet visibly askew beneath the current one; hatching as the only texture.

## Colors

### Primary
- **Dichroic magenta** (`accent` #ec2a8a / dark `dark-accent` #ff4fa3): fills the selected option's key, hatches the selected row and the current rail row, rings the current swatch, focus ring, the answer field's border, the "time granted to you" badge. As text it uses `accent-text` (#a30e58 / #ff7ab9), over 7:1 on the sheet.

### Neutral
- **Table** (#e8e6e1 / #111110): page ground.
- **Sheet** (#f8f7f4 / #1c1c1a): the reading surface. **Sheet under** (#f0efeb / #232321): sheets beneath, hover.
- **Ink** (#141414 / #ecebe6): text, strong rules, answered swatches, primary buttons. **Ink secondary** (#5b5a56 / #a7a59f): labels and secondary text (≥5.5:1). **Ink decorative** (#8f8d88): large numerals' "/12" only, never body text.
- **Rule** (#cdcbc5 / #353431): hairlines between rows.

### Named Rules
**The Yours-Only Rule.** Magenta marks what belongs to the student: their answer, their position, their focus, time granted to them. Never decoration, never a category colour, never a syntax colour.

**The Reversed-Plate Rule.** Escalation inverts to ink (≤5 min timer, penalties, annulment, offline). No red, no amber, no alarm.

## Typography

**Display/labels:** Sofia Sans Extra Condensed, in caps, tracked at +0.14em.
**Body:** Sofia Sans. **Code:** JetBrains Mono with `font-variant-ligatures: none`, so `->` and `>=` read literally.

### Hierarchy
- **Clock** (800, 46px, 0.85): time remaining.
- **Numeral** (800, 30px): question number in the record row ("04 / 12").
- **Prompt** (700, 34px desktop / 20px mobile, -0.015em, balanced): the question.
- **Body** (400, 18px, 1.55): content, options; measure ≤68ch.
- **Label** (700, 13px, +0.14em, caps): every field name, state, action.
- **Small** (400, 14px): hints, save status.
- **Code** (400, 16px, 1.75): code panels; grayscale highlighting (keywords bold, strings/comments italic secondary, numbers secondary).

### Named Rules
**The No-Eyebrow Rule.** A small label never sits above a heading or a big numeral; meta goes into record cells or under the heading.

## Layout

Desktop (1440): 80px header strip of labelled cells. A 280px left rail holds the question list. The centred top sheet is 790px wide with 46px side padding, and the next sheet sits askew beneath it. A 194px right column holds the legend, with the isolated submit block at the bottom. Mobile (390): 60px header cells, a tone strip, one full-width sheet (18px padding) and a 76px bottom bar (index · previous · next). The spacing grid is 4px. Option rows are full-bleed to the sheet edge and at least 56px tall.

## Elevation & Depth

Flat everywhere except sheets. The top sheet carries one shadow (`0 1px 2px rgba(20,20,20,.08), 0 18px 40px -8px rgba(20,20,20,.2)`). Sheets beneath are rotated (−1.4°, +2.1°) and offset, never shadowed twice. Floating notices and dialogs use a deeper float shadow. In the dark theme, shadows become a 1px light edge plus a deep shadow.

### Named Rules
**The Straight-Top Rule.** Only sheets beneath may tilt; the sheet you read is always square.

## Shapes

Radius 0 everywhere: sheets, buttons, inputs, cells, dialogs. Circles appear only in figure content and the status dot. Textures are hatches only: ink hatch (flagged), magenta hatch (yours), and a 1px struck rule (closed/disabled).

## Components

### Buttons
Rectangles, 50px tall (40px small), label typography. Primary is ink with sheet text; secondary is sheet with a 1px ink border; quiet is borderless. Hover is sheet-under (primary mixes 14% sheet in). Focus is a 2px sheet gap plus a 2px magenta ring. Disabled uses decorative ink on a rule border. Loading spins the loader icon. "Marcar para revisar" is a toggle (`aria-pressed`) whose chip fills with ink hatch.

### Submit block
An isolated ink block (≥98px tall) with "Revisar y entregar" in 23px condensed caps and "Podrás revisar →" below. It is never adjacent to navigation.

### Inputs / Fields
52px, 1px ink border, mono for numbers and body face for text, magenta caret. Focus shows a magenta border and 1px ring. The answer state is a 2px magenta border. Errors use a 2px ink border with an inverted ink message block (never magenta, which would read as focus). The session code is six ruled cells (68px, 38px condensed), with the caret shown as a magenta underline.

### Record row
Labelled cells across the top of a sheet (Pregunta · Valor · Tipo · Estado). Columns size to their content and the row ends in an ink rule.

### Options (signature)
A ruled table, not cards. Each row has a 36px key cell (letter), text and a trailing mark. Selected rows are hatched magenta with the key filled magenta and the mark "Tu respuesta". Multiple choice uses a double-framed key (inset sheet ring) and "✓ Elegida". Locked rows use decorative ink and a lock icon. Veiled rows (hover reveal) blur the text at 7px. True/false is two 104px halves in condensed caps.

### Navigation
The desktop rail rows show number · state word · 20px tone swatch (ink = answered, ink hatch = flagged, sheet = pending). The current row is hatched magenta with a ringed swatch. The mobile strip shows the same tones in N cells with the current one inset-ringed. The mobile index is a bottom sheet listing rail rows.

### Status
Header cells hold the clock (soon: label in ink; ≤5 min: inverted; last minute: inverted with a magenta base bar; extra time: a magenta-hatched "+5 min para ti" badge until seen). Strikes are three 16px squares that fill with ink. Save status reads saved / saving (spinner) / offline (inverted chip with pending count) / reconnected.

### Notices, dialogs, overlays
Notices sit in a corner with a 28px mark square (magenta hatch for "yours", inverted for penalties). The submit dialog shows a 3-cell tally. Overlays blur the sheet as a privacy curtain and put a square panel on top. Penalties, pause and annulment use inverted panels; the away countdown uses a 104px condensed numeral.

## Do's and Don'ts

### Do:
- **Do** keep magenta for the student's own answer, position, focus and granted time.
- **Do** put meta in record cells or under the heading.
- **Do** show figures on a light print matte in both themes; never invert or crop them.
- **Do** keep counters in fixed cells with tabular numerals so values change and the layout never moves.
- **Do** explain every penalty in plain words, including that the teacher can revert it.

### Don't:
- **Don't** use red or amber for warnings; invert to ink.
- **Don't** round corners or put options in cards.
- **Don't** colour code syntax; grayscale weight and italics only.
- **Don't** place a label above a heading (no eyebrows).
- **Don't** let code ligatures render (`->`, `>=`, `!=` must stay literal).
- **Don't** resemble Moodle, Google Forms, Classroom or Kahoot.
