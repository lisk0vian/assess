---
version: 1
slug: "internal-tui"
primary_target: "internal/tui"
related_targets: []
---

## Scope

Teacher terminal panels (vault hub, lobby, live session, incidents, confirmations, report, exam editor, settings). Mode: Operate. Light and dark themes. 120×36 reference, 80×24 must work. Build path: code-led. All administration happens here, on a laptop.

## Direction contract

THESIS: The same light table as the student app, seen from the teacher's side. The class is a contact sheet: each student is a frame that darkens as they answer, and what needs attention inverts.

OWN-WORLD: Inherited from Mesa de luz in web/DESIGN.md: zone neutrals, one magenta for what belongs to the teacher, serious states inverted to ink, ruled tables, square sheets. Translated to cells in internal/tui/DESIGN.md.

STORY: At any moment the teacher sees who is where, what waits for a decision and what remains. The report answers first what to look at, then shows the proof.

FIRST VIEWPORT: Strip of labelled cells with the clock, view tabs, a request band when something waits, the contact sheet of students, and a key bar. Signature move: the class as contact tiles, darkening with progress.

FORM: Hoja de contactos, chosen by the user over Partitura and Riel + hoja.

FINISH: Every proposal is drawn on a validated cell grid in internal/tui/design/canvas.html; a change that a terminal cannot draw does not ship.

## Constraints

Keep every existing shortcut. No new chart dependency: ntcharts plus a small grid helper. No red or amber. Nothing depends on the exact rendering of a glyph. Offline, CGO-free single binary. Spanish uses tú.
