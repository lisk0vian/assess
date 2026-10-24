---
version: 1
slug: "web"
primary_target: "web"
related_targets: []
---

## Scope

Student exam web app (all student screens: join, notice and pledge, lobby, question, review, result, offline and anti-cheat states). Mode: Operate. Light and dark themes. Desktop and mobile with equal weight. General-purpose content (code, images, math). Build path: code-led.

## Direction contract

THESIS: The exam is a stack of exposure-record sheets on a light table. Neutral zones carry all structure; one magenta marks only what is yours (your answer, where you are). It refuses the category default: a white card, a blue progress bar and a list of rounded radio buttons.

OWN-WORLD: Zone-ramp neutrals. Light is the light table (paper #e8e6e1, sheet #f8f7f4, ink #141414); dark is the darkroom (#111110, sheet #1c1c1a, ink #ecebe6). A single dichroic magenta (#ec2a8a, dark #ff4fa3). Tracked condensed caps (Sofia Sans Extra Condensed) for labels, numerals and actions; Sofia Sans for prose; JetBrains Mono for code with grayscale syntax. Square sheets, 1px ink rules, ink hatch means flagged, magenta hatch means your answer.

STORY: At every moment the student sees where they are, what is theirs and what remains. Nothing shouts; warnings invert to ink instead of turning red. Answering darkens the record.

FIRST VIEWPORT: Desktop has a header strip of labelled cells (exam, student, 46px condensed time, warnings), a left rail of question rows with tone swatches, and the top sheet in the centre (record row, prompt, code panel, ruled option table, actions) with the next sheet askew beneath. A legend and an isolated black submit block sit on the right. Mobile has header cells, a tone strip, one sheet and a bottom action bar. Signature interaction: "Siguiente" slides the top sheet aside and the sheet beneath straightens into place. Motion has mass (about 280ms, slight overshoot); reduced motion swaps instantly.

FORM: Exposure-record zone sheets, the lead of bolder re-roll 2, chosen by the user over three alternates. seed:4b332b8ab7ed44be

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Constraints

Offline: fonts self-hosted in production. Budget: JS ≤150 KB gz, CSS ≤30 KB. WCAG 2.2 AA. Spanish uses tú. Never red alarm states.
