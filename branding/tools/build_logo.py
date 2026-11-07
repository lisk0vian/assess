#!/usr/bin/env python3
"""Assess identity — final artwork. Run from anywhere; writes ../logo/."""
import os
from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont
from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "..", "logo")
os.makedirs(OUT, exist_ok=True)

FONT = os.path.join(
    HERE, "..", "..", "web", "node_modules", ".pnpm",
    "@fontsource-variable+sofia-sans-extra-condensed@5.3.0", "node_modules",
    "@fontsource-variable", "sofia-sans-extra-condensed", "files",
    "sofia-sans-extra-condensed-latin-wght-normal.woff2",
)
INK = "#141414"
SHEET = "#f8f7f4"
ACCENT = "#ec2a8a"
DARK_INK = "#ecebe6"
DARK_ACCENT = "#ff4fa3"
WEIGHT = 750


def load_font(wght=WEIGHT):
    f = TTFont(FONT)
    f = instantiateVariableFont(f, {"wght": wght}, inplace=False, updateFontNames=False)
    return f, f.getBestCmap(), f.getGlyphSet()


def text_paths(font, cmap, gs, text, cap_height, tracking=0.12):
    upem = font["head"].unitsPerEm
    cap = font["OS/2"].sCapHeight
    size = cap_height * upem / cap
    s = size / upem
    x = 0.0
    parts = []
    for ch in text:
        gname = cmap[ord(ch)]
        pen = SVGPathPen(gs, ntos=lambda v: f"{v:.2f}")
        tpen = TransformPen(pen, (s, 0, 0, -s, x, 0))
        gs[gname].draw(tpen)
        d = pen.getCommands()
        if d:
            parts.append(d)
        x += font["hmtx"][gname][0] * s + tracking * size
    return " ".join(parts), x - tracking * size


def wordmark(x, baseline, cap=150, tracking=0.12, fill=INK):
    font, cmap, gs = load_font()
    d, w = text_paths(font, cmap, gs, "ASSESS", cap, tracking)
    return (f'<g transform="translate({x:g} {baseline:g})" fill="{fill}">'
            f'<path d="{d}"/></g>'), w


def write(w, h, body, name, title):
    doc = (f'<svg xmlns="http://www.w3.org/2000/svg" width="{w:g}" height="{h:g}" '
           f'viewBox="0 0 {w:g} {h:g}" role="img">\n'
           f'<title>{title}</title>\n{body}\n</svg>\n')
    path = os.path.join(OUT, name)
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(doc)
    print("wrote", path)


def mark_body(fill=INK, accent=ACCENT):
    """A + record rule + block; 256 field, optically centred."""
    A, _ = text_paths(*load_font(), "A", 160, 0)
    return (f'<g transform="translate(16 202)" fill="{fill}"><path d="{A}"/></g>'
            f'<rect x="16" y="202" width="224" height="12" fill="{fill}"/>'
            f'<rect x="208" y="170" width="32" height="32" fill="{accent}"/>')


def wordmark_body(ink, accent, rule=True):
    wm, w = wordmark(16, 190 if rule else 196, fill=ink)
    if rule:
        gap = 12
        square_x = 16 + w + gap
        end = square_x + 32
        body = (wm
                + f'<rect x="16" y="202" width="{end-16:.2f}" height="12" fill="{ink}"/>'
                + f'<rect x="{square_x:.2f}" y="170" width="32" height="32" fill="{accent}"/>')
        return body, round(end + 16), 256
    return wm, round(16 + w + 16), 256


def tile_body(bg=INK, letter=SHEET, accent=ACCENT):
    """Full-bleed app tile; A + block, sized for 16 px."""
    A, _ = text_paths(*load_font(), "A", 170, 0)
    return (f'<rect width="256" height="256" fill="{bg}"/>'
            f'<g transform="translate(32 200)" fill="{letter}"><path d="{A}"/></g>'
            f'<rect x="176" y="160" width="40" height="40" fill="{accent}"/>')


def stacked_body(ink, accent):
    mono = mark_body(ink, accent)
    font, cmap, gs = load_font()
    _, wm_w = text_paths(font, cmap, gs, "ASSESS", 90, 0.12)
    wm, _ = wordmark(round((520 - wm_w) / 2, 2), 365, cap=90, fill=ink)
    return f'<g transform="translate(155 15) scale(0.82)">{mono}</g>{wm}' 


if __name__ == "__main__":
    b, w, h = wordmark_body(INK, ACCENT)
    write(w, h, b, "assess-wordmark.svg", "Assess — wordmark")
    b, w, h = wordmark_body(INK, ACCENT, rule=False)
    write(w, h, b, "assess-wordmark-plain.svg", "Assess — wordmark without rule")
    b, w, h = wordmark_body(DARK_INK, DARK_ACCENT)
    write(w, h, b, "assess-wordmark-dark.svg", "Assess — wordmark for dark backgrounds")

    write(256, 256, mark_body(), "assess-mark.svg", "Assess — mark")
    write(256, 256, mark_body(DARK_INK, DARK_ACCENT), "assess-mark-dark.svg", "Assess — mark for dark backgrounds")

    write(256, 256, tile_body(), "assess-tile.svg", "Assess — app icon")
    write(256, 256, tile_body(accent=DARK_ACCENT), "assess-tile-dark.svg", "Assess — app icon for dark backgrounds")

    write(520, 420, stacked_body(INK, ACCENT), "assess-stacked.svg", "Assess — stacked lockup")
    write(520, 420, stacked_body(DARK_INK, DARK_ACCENT),
          "assess-stacked-dark.svg", "Assess — stacked lockup for dark backgrounds")
