# Assess — Registro

## The idea

The name sits on a record rule. The magenta block closes the row: it is the student's mark, the
only thing that belongs to them in an exam. The identity does not illustrate the exam with paper,
books or clocks; it behaves like a record row.

It is deliberately quiet. On the exam screen the question, the clock and the institution's logo
lead. Assess appears discreetly in the header or footer, and steps back when it is in the way.

## Files

```
logo/
  assess-wordmark.svg          primary wordmark, light background
  assess-wordmark-plain.svg    no rule: app header and surfaces with their own rules
  assess-wordmark-dark.svg     primary wordmark, dark background
  assess-mark.svg              compact mark: A + rule + block
  assess-mark-dark.svg         compact mark, dark background
  assess-tile.svg              app icon / favicon (ink tile, sheet A, magenta block)
  assess-tile-dark.svg         icon for dark backgrounds
  assess-stacked.svg           stacked lockup: mark above the wordmark
  assess-stacked-dark.svg      stacked lockup, dark background

variants/                      one-ink (black / white) and square avatar
icons/                         favicon.ico, favicon.svg, PNGs, manifest and <head> snippet
png/                           transparent PNGs of the masters
presentation/                  final deck in HTML + slide PNGs
presentation/postprocess.py    finalises the deck's CLI and teacher labels
concepts.png / concepts.svg    archived exploration round
tools/build_logo.py            regenerates the SVGs (font + fontTools)
```

## Which file to use

| Use | File |
|---|---|
| Cover, README, documentation, presentation | `logo/assess-wordmark.svg` |
| App header (already has rules) | `logo/assess-wordmark-plain.svg` at 30 px body size |
| Dark background / terminal / dark README | `logo/assess-wordmark-dark.svg` |
| Avatar, stamp, medium size, one ink | `logo/assess-mark.svg` |
| Favicon, app icon, browser tab | `logo/assess-tile.svg` (or `icons/`) |
| Badge, vertical poster, screen | `logo/assess-stacked.svg` |
| One ink | `variants/*-black.svg` (light) / `variants/*-white.svg` (dark) |
| Square avatar | `variants/assess-mark-square.svg` |

The **tile** is an app icon, not the primary mark: it is for 16–64 px and app surfaces. For
one-ink printing use `assess-mark` or `assess-wordmark`, not the tile.

## Colour

| Use | HEX | RGB | CMYK approx. |
|---|---|---|---|
| Ink | `#141414` | 20 20 20 | 0 0 0 92 |
| Sheet | `#f8f7f4` | 248 247 244 | 0 0 2 3 |
| Magenta (block) | `#ec2a8a` | 236 42 138 | 0 82 42 7 |
| Dark ink | `#ecebe6` | 236 235 230 | 0 0 3 7 |
| Dark magenta | `#ff4fa3` | 255 79 163 | 0 69 36 0 |

Magenta is semantic: it marks what belongs to the student. It is not decoration, it is not used
as a category colour, and it is never swapped for another accent. For CMYK, request a proof; if
a Pantone match is needed, match it against physical samples, not a table.

## Typography

- **Wordmark:** Sofia Sans Extra Condensed, weight 750, tracking +0.12 em, outlined.
  Never set it with the live font: always use the SVG.
- **App labels and numerals:** Sofia Sans Extra Condensed 700 (labels) and 800 (numerals),
  uppercase, +0.14 em in labels.
- **Text:** Sofia Sans. **Code:** JetBrains Mono, without ligatures.

Sofia Sans is SIL OFL 1.1. The wordmark outlines may be used as a mark; the typeface remains
free and non-exclusive. What is ownable in this identity is the rule and the block, not the type.

## Minimum sizes

| Piece | Screen | Print |
|---|---|---|
| Primary wordmark | 120 px wide | 30 mm |
| Wordmark without rule | 90 px wide | 22 mm |
| Compact mark | 32 px | 8 mm |
| Tile / favicon | 16 px | — |
| Stacked lockup | 160 px wide | 40 mm |

**Clear space:** one block (the magenta square) on every side. For co-branding, two blocks
between the Assess logo and the institution's logo. The institution's logo comes first.

## Backgrounds and co-branding

- Preferred: sheet `#f8f7f4`, white, ink `#141414`.
- On a dark screen, use the `-dark` files (light ink + `#ff4fa3`); do not recolour by hand. The
  `-white` files are for one-ink printing, not for preserving the magenta.
- Never on magenta: the block disappears. Avoid busy photos; use a sheet or an ink block behind
  the logo if the background is noisy.
- In the exam, the institution is the protagonist. Assess may disappear from the screen when
  there is already too much on it; it does not have to be present everywhere.

## Don'ts

- Stretch, condense, rotate or slant the wordmark.
- Move the block away from the end of the rule or resize it on its own.
- Change the magenta or colour the letters.
- Add shadows, gradients, outlines, bevels or animation.
- Set “ASSESS” in another typeface or with different tracking.
- Use the tile as the primary mark at large sizes or in one ink.
- Put the Assess logo inside the institution's logo, or the other way round.
- Use the wordmark without the rule on a cover: use the primary version for that.

## Accessibility

- Ink on sheet: contrast above 15:1. Sheet on ink: above 14:1.
- The magenta block is decorative; the name and the form of the A do not depend on colour.
- The SVGs carry a `<title>`; in `<img>`, use `alt="Assess"`.
- Do not use the logo as the only indicator of state or action.

## App integration

The student app already uses the plain wordmark in its header (`Wordmark` in
`web/src/components/ui.tsx`) and ships the tile as its favicon from `web/public/`. If you port
the identity to another build:

1. Copy the **contents** of `icons/` into the web root (in this repo, `web/public/`); the files
   belong at the root, not in a subfolder.
2. Paste `icons/head-snippet.html` inside `<head>`.
3. For headers that already have rules, use `logo/assess-wordmark-plain.svg` at 30 px body size;
   do not add the rule twice.
4. The favicon and app icon are already generated; do not redraw them.

## License and trademark

- The project is Apache 2.0; the code licence does not grant trademark rights.
- The identity is already applied in the app (header and favicon); the files live in `branding/`.
- It is still a direction proposal, not a registration: “Assess” is a common word, and a
  clearance search is recommended before registering it.
- The credential mockup name (“Alex Morgan”, “Teacher”) is a placeholder.

## Handover

The chosen direction is **Registro**. The kit covers the wordmark, compact mark, app tile,
horizontal and stacked lockups, dark versions, one ink, avatar, favicon and the web icon set. The
deck is `presentation/presentation.html`, in English, with its slides in `presentation/slides/`.
